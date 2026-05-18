package weave

import "fmt"

// DecodeStats summarizes the repair work performed by Reconstruct.
type DecodeStats struct {
	TotalDataFrames  uint32
	TotalDataBytes   uint64
	DataFramesSeen   int
	RescueFramesSeen int
	RecoveredFrames  int
	MissingFrames    int
	InvalidFrames    int
}

// ReconstructBinary parses marshalled frames, discards invalid frames, repairs
// recoverable erasures, and returns the original payload.
func ReconstructBinary(packets [][]byte, cfg Config) ([]byte, DecodeStats, error) {
	var frames []Frame
	stats := DecodeStats{}
	for _, packet := range packets {
		frame, err := ParseFrame(packet)
		if err != nil {
			stats.InvalidFrames++
			continue
		}
		frames = append(frames, frame)
	}

	out, innerStats, err := Reconstruct(frames, cfg)
	innerStats.InvalidFrames += stats.InvalidFrames
	return out, innerStats, err
}

// Reconstruct repairs and reassembles frames produced by Encode.
func Reconstruct(frames []Frame, cfg Config) ([]byte, DecodeStats, error) {
	return ReconstructTo(nil, frames, cfg)
}

// ReconstructTo repairs and reassembles frames into dst.
// The returned slice may reuse dst's backing array.
func ReconstructTo(dst []byte, frames []Frame, cfg Config) ([]byte, DecodeStats, error) {
	if err := cfg.validate(); err != nil {
		return nil, DecodeStats{}, err
	}
	return reconstructToValidated(dst, frames, cfg, newCoefficientTable(cfg), nil)
}

func reconstructToValidated(dst []byte, frames []Frame, cfg Config, coeffs coefficientTable, workspace *reconstructWorkspace) ([]byte, DecodeStats, error) {
	totalFrameVotes, totalByteVotes, blockSizeVotes := freshVoteMaps(workspace)
	stats := DecodeStats{}
	validCandidates := 0

	for _, frame := range frames {
		h := frame.Header
		if !frameMetadataLooksValid(h, len(frame.Payload), cfg) {
			stats.InvalidFrames++
			continue
		}
		totalFrameVotes[h.TotalFrames]++
		totalByteVotes[h.TotalDataBytes]++
		blockSizeVotes[h.BlockSize]++
		validCandidates++
	}

	if validCandidates == 0 {
		return nil, stats, ErrNoValidFrames
	}

	totalFrames := voteUint32(totalFrameVotes)
	totalBytes := voteUint64(totalByteVotes)
	blockSize := voteUint16(blockSizeVotes)
	if totalFrames == 0 || blockSize == 0 {
		return nil, stats, fmt.Errorf("%w: missing metadata", ErrUnrecoverable)
	}
	stats.TotalDataFrames = totalFrames
	stats.TotalDataBytes = totalBytes

	blockCfg := cfg
	blockCfg.DataFramesPerBlock = int(blockSize)
	if err := blockCfg.validate(); err != nil {
		return nil, stats, err
	}
	if blockCfg.DataFramesPerBlock != cfg.DataFramesPerBlock {
		coeffs = newCoefficientTable(blockCfg)
	}

	blockCount := int((totalFrames + uint32(blockSize) - 1) / uint32(blockSize))
	dataFrames := freshDataFrameSlots(workspace, totalFrames)
	rescueFrames := freshRescueFrameSlots(workspace, blockCount, cfg.RescueFramesPerBlock)

	for _, frame := range frames {
		h := frame.Header
		if !frameMetadataLooksValid(h, len(frame.Payload), cfg) {
			continue
		}
		if h.TotalFrames != totalFrames || h.TotalDataBytes != totalBytes || h.BlockSize != blockSize {
			stats.InvalidFrames++
			continue
		}

		switch h.FrameType {
		case FrameTypeData:
			if h.FrameIndex >= totalFrames {
				stats.InvalidFrames++
				continue
			}
			dataFrames[h.FrameIndex] = frame.Payload
			stats.DataFramesSeen++
		case FrameTypeRescue:
			if h.FrameIndex >= totalFrames || h.FrameIndex%uint32(blockSize) != 0 {
				stats.InvalidFrames++
				continue
			}
			blockID := int(h.FrameIndex / uint32(blockSize))
			if blockID < 0 || blockID >= len(rescueFrames) {
				stats.InvalidFrames++
				continue
			}
			if rescueFrames[blockID] == nil {
				rescueFrames[blockID] = make([][]byte, cfg.RescueFramesPerBlock)
			}
			rescueFrames[blockID][h.RescueIndex] = frame.Payload
			stats.RescueFramesSeen++
		default:
			stats.InvalidFrames++
		}
	}

	if stats.DataFramesSeen == 0 && stats.RescueFramesSeen == 0 {
		return nil, stats, ErrNoValidFrames
	}

	stats.RecoveredFrames = repairBlocksWithWorkspace(dataFrames, rescueFrames, totalFrames, totalBytes, blockCfg, coeffs, workspace)

	if uint64(cap(dst)) < totalBytes {
		dst = make([]byte, 0, totalBytes)
	} else {
		dst = dst[:0]
	}

	for i := uint32(0); i < totalFrames; i++ {
		payload := dataFrames[i]
		if payload == nil {
			stats.MissingFrames++
			continue
		}
		size := dataSizeForFrame(i, totalBytes, cfg.PayloadSize)
		if len(payload) > size {
			payload = payload[:size]
		}
		dst = append(dst, payload...)
	}
	if stats.MissingFrames > 0 {
		return nil, stats, fmt.Errorf("%w: %d frame(s) missing", ErrUnrecoverable, stats.MissingFrames)
	}
	if uint64(len(dst)) > totalBytes {
		dst = dst[:totalBytes]
	}
	return dst, stats, nil
}

func freshVoteMaps(workspace *reconstructWorkspace) (map[uint32]int, map[uint64]int, map[uint16]int) {
	if workspace != nil {
		return workspace.voteMaps()
	}
	return make(map[uint32]int), make(map[uint64]int), make(map[uint16]int)
}

func freshDataFrameSlots(workspace *reconstructWorkspace, totalFrames uint32) [][]byte {
	if workspace != nil {
		return workspace.dataFrameSlots(totalFrames)
	}
	return make([][]byte, totalFrames)
}

func freshRescueFrameSlots(workspace *reconstructWorkspace, blockCount int, rescueCount int) [][][]byte {
	if workspace != nil {
		return workspace.rescueFrameSlots(blockCount, rescueCount)
	}
	return make([][][]byte, blockCount)
}

func frameMetadataLooksValid(h Header, payloadLen int, cfg Config) bool {
	if h.TotalFrames == 0 || h.BlockSize == 0 {
		return false
	}
	if h.FrameType != FrameTypeData && h.FrameType != FrameTypeRescue {
		return false
	}
	if payloadLen > cfg.PayloadSize {
		return false
	}
	if h.FrameType == FrameTypeRescue {
		if int(h.RescueIndex) >= cfg.RescueFramesPerBlock {
			return false
		}
		if payloadLen < cfg.PayloadSize {
			return false
		}
	}
	return true
}

func repairBlocks(data [][]byte, rescues [][][]byte, totalFrames uint32, totalBytes uint64, cfg Config, coeffs coefficientTable) int {
	return repairBlocksWithWorkspace(data, rescues, totalFrames, totalBytes, cfg, coeffs, nil)
}

func repairBlocksWithWorkspace(data [][]byte, rescues [][][]byte, totalFrames uint32, totalBytes uint64, cfg Config, coeffs coefficientTable, workspace *reconstructWorkspace) int {
	recovered := 0
	blockSize := uint32(cfg.DataFramesPerBlock)
	inverseCache := make(map[string][][]byte)
	scratch := repairScratch{}
	scratchRef := &scratch
	var missingBuf []uint32
	var availableBuf []uint16
	if workspace != nil {
		inverseCache = workspace.inverseCacheFor(cfg)
		scratchRef = &workspace.scratch
		missingBuf = workspace.missing
		availableBuf = workspace.available
		workspace.beginRecoveredPayloads()
	}

	for blockID, blockRescues := range rescues {
		blockStart := uint32(blockID) * blockSize
		if blockStart >= totalFrames {
			break
		}
		blockEnd := blockStart + blockSize
		if blockEnd > totalFrames {
			blockEnd = totalFrames
		}

		missing := missingBuf[:0]
		for idx := blockStart; idx < blockEnd; idx++ {
			if data[idx] == nil {
				missing = append(missing, idx)
			}
		}
		missingBuf = missing[:0]
		if len(missing) == 0 {
			continue
		}

		availableRescues := availableRescueIndexesTo(availableBuf[:0], blockRescues, cfg.PayloadSize)
		availableBuf = availableRescues[:0]
		if len(missing) > len(availableRescues) {
			continue
		}

		if len(missing) == 1 {
			residual := scratchRef.bytes(&scratchRef.residual0, cfg.PayloadSize, false)
			if is16x2Profile(cfg) {
				if recoverOneMissingFrame16x2(data, blockRescues, blockStart, blockEnd, missing[0], availableRescues[0], totalBytes, cfg, coeffs, residual, workspace) {
					recovered++
				}
				continue
			}
			if recoverOneMissingFrame(data, blockRescues, blockStart, blockEnd, missing[0], availableRescues[0], totalBytes, cfg, coeffs, residual, workspace) {
				recovered++
			}
			continue
		}

		if len(missing) == 2 {
			residual0 := scratchRef.bytes(&scratchRef.residual0, cfg.PayloadSize, false)
			residual1 := scratchRef.bytes(&scratchRef.residual1, cfg.PayloadSize, false)
			if is16x2Profile(cfg) {
				if recoverTwoMissingFrames16x2(data, blockRescues, blockStart, blockEnd, missing[0], missing[1], availableRescues[0], availableRescues[1], totalBytes, cfg, coeffs, residual0, residual1, workspace) {
					recovered += 2
				}
				continue
			}
			if recoverTwoMissingFrames(data, blockRescues, blockStart, blockEnd, missing[0], missing[1], availableRescues[0], availableRescues[1], totalBytes, cfg, coeffs, residual0, residual1, workspace) {
				recovered += 2
			}
			continue
		}

		usedRescues := availableRescues[:len(missing)]
		key := inverseCacheKey(blockStart, missing, usedRescues)
		inverse, ok := inverseCache[key]
		if !ok {
			matrix := make([][]byte, len(missing))
			for row, rescueIndex := range usedRescues {
				matrix[row] = make([]byte, len(missing))
				for col, missingIndex := range missing {
					slot := int(missingIndex - blockStart)
					matrix[row][col] = coeffs.at(int(rescueIndex), slot)
				}
			}

			inverse, ok = invertMatrix(matrix)
			if !ok {
				continue
			}
			inverseCache[key] = inverse
		}

		residuals := scratchRef.rows(&scratchRef.residualRows, len(missing), cfg.PayloadSize, false)
		for row, rescueIndex := range usedRescues {
			rescuePayload := blockRescues[rescueIndex]
			copy(residuals[row], rescuePayload[:cfg.PayloadSize])

			for idx := blockStart; idx < blockEnd; idx++ {
				payload := data[idx]
				if payload == nil {
					continue
				}
				slot := int(idx - blockStart)
				coeff := coeffs.at(int(rescueIndex), slot)
				mul := gfMulTable[coeff][:]
				for i, b := range payload {
					residuals[row][i] ^= mul[b]
				}
			}
		}

		recoveredPayloads := scratchRef.rows(&scratchRef.recoveredRows, len(missing), cfg.PayloadSize, true)
		for col := range missing {
			for row := range usedRescues {
				coeff := inverse[col][row]
				if coeff == 0 {
					continue
				}
				mul := gfMulTable[coeff][:]
				for i := 0; i < cfg.PayloadSize; i++ {
					recoveredPayloads[col][i] ^= mul[residuals[row][i]]
				}
			}
		}

		for col, idx := range missing {
			size := dataSizeForFrame(idx, totalBytes, cfg.PayloadSize)
			payload := recoveredPayload(workspace, size)
			copy(payload, recoveredPayloads[col][:size])
			data[idx] = payload
			recovered++
		}
	}
	if workspace != nil {
		workspace.missing = missingBuf
		workspace.available = availableBuf
	}
	return recovered
}

func recoveredPayload(workspace *reconstructWorkspace, size int) []byte {
	if workspace != nil {
		return workspace.recoveredPayload(size)
	}
	return make([]byte, size)
}

type repairScratch struct {
	residual0     []byte
	residual1     []byte
	residualRows  [][]byte
	recoveredRows [][]byte
}

func (s *repairScratch) bytes(buf *[]byte, size int, clearBuffer bool) []byte {
	if cap(*buf) < size {
		*buf = make([]byte, size)
	}
	out := (*buf)[:size]
	if clearBuffer {
		clear(out)
	}
	return out
}

func (s *repairScratch) rows(buf *[][]byte, count int, size int, clearRows bool) [][]byte {
	if cap(*buf) < count {
		*buf = make([][]byte, count)
	}
	out := (*buf)[:count]
	for i := range out {
		if cap(out[i]) < size {
			out[i] = make([]byte, size)
		}
		out[i] = out[i][:size]
		if clearRows {
			clear(out[i])
		}
	}
	return out
}

func recoverOneMissingFrame(data [][]byte, rescues [][]byte, blockStart, blockEnd, missingIdx uint32, rescueIndex uint16, totalBytes uint64, cfg Config, coeffs coefficientTable, residual []byte, workspace *reconstructWorkspace) bool {
	rescuePayload := rescues[rescueIndex]
	if len(rescuePayload) < cfg.PayloadSize {
		return false
	}

	copy(residual, rescuePayload[:cfg.PayloadSize])
	for idx := blockStart; idx < blockEnd; idx++ {
		payload := data[idx]
		if payload == nil {
			continue
		}
		slot := int(idx - blockStart)
		coeff := coeffs.at(int(rescueIndex), slot)
		mul := gfMulTable[coeff][:]
		for i, b := range payload {
			residual[i] ^= mul[b]
		}
	}

	missingSlot := int(missingIdx - blockStart)
	coeff := coeffs.at(int(rescueIndex), missingSlot)
	if coeff == 0 {
		return false
	}
	invCoeff := gfInv(coeff)
	mul := gfMulTable[invCoeff][:]
	size := dataSizeForFrame(missingIdx, totalBytes, cfg.PayloadSize)
	payload := recoveredPayload(workspace, size)
	for i := 0; i < size; i++ {
		payload[i] = mul[residual[i]]
	}
	data[missingIdx] = payload
	return true
}

func recoverTwoMissingFrames(data [][]byte, rescues [][]byte, blockStart, blockEnd, aIdx, bIdx uint32, r0, r1 uint16, totalBytes uint64, cfg Config, coeffs coefficientTable, residual0, residual1 []byte, workspace *reconstructWorkspace) bool {
	r0Payload := rescues[r0]
	r1Payload := rescues[r1]
	if len(r0Payload) < cfg.PayloadSize || len(r1Payload) < cfg.PayloadSize {
		return false
	}

	copy(residual0, r0Payload[:cfg.PayloadSize])
	copy(residual1, r1Payload[:cfg.PayloadSize])

	for idx := blockStart; idx < blockEnd; idx++ {
		payload := data[idx]
		if payload == nil {
			continue
		}
		slot := int(idx - blockStart)
		c0 := coeffs.at(int(r0), slot)
		c1 := coeffs.at(int(r1), slot)
		mul0 := gfMulTable[c0][:]
		mul1 := gfMulTable[c1][:]
		for i, b := range payload {
			residual0[i] ^= mul0[b]
			residual1[i] ^= mul1[b]
		}
	}

	aSlot := int(aIdx - blockStart)
	bSlot := int(bIdx - blockStart)
	a0 := coeffs.at(int(r0), aSlot)
	b0 := coeffs.at(int(r0), bSlot)
	a1 := coeffs.at(int(r1), aSlot)
	b1 := coeffs.at(int(r1), bSlot)

	det := gfMul(a0, b1) ^ gfMul(a1, b0)
	if det == 0 {
		return false
	}
	invDet := gfInv(det)

	aSize := dataSizeForFrame(aIdx, totalBytes, cfg.PayloadSize)
	bSize := dataSizeForFrame(bIdx, totalBytes, cfg.PayloadSize)
	aPayload := recoveredPayload(workspace, aSize)
	bPayload := recoveredPayload(workspace, bSize)
	aFromR0 := gfMulTable[gfMul(invDet, b1)][:]
	aFromR1 := gfMulTable[gfMul(invDet, b0)][:]
	bFromR0 := gfMulTable[gfMul(invDet, a1)][:]
	bFromR1 := gfMulTable[gfMul(invDet, a0)][:]
	maxSize := aSize
	if bSize > maxSize {
		maxSize = bSize
	}
	for i := 0; i < maxSize; i++ {
		aByte := aFromR0[residual0[i]] ^ aFromR1[residual1[i]]
		bByte := bFromR0[residual0[i]] ^ bFromR1[residual1[i]]
		if i < aSize {
			aPayload[i] = aByte
		}
		if i < bSize {
			bPayload[i] = bByte
		}
	}

	data[aIdx] = aPayload
	data[bIdx] = bPayload
	return true
}

func recoverOneMissingFrame16x2(data [][]byte, rescues [][]byte, blockStart, blockEnd, missingIdx uint32, rescueIndex uint16, totalBytes uint64, cfg Config, coeffs coefficientTable, residual []byte, workspace *reconstructWorkspace) bool {
	rescuePayload := rescues[rescueIndex]
	if len(rescuePayload) < cfg.PayloadSize {
		return false
	}

	copy(residual, rescuePayload[:cfg.PayloadSize])
	for idx := blockStart; idx < blockEnd; idx++ {
		payload := data[idx]
		if payload == nil {
			continue
		}
		slot := int(idx - blockStart)
		mul := gfMulTable[coeffs.at(int(rescueIndex), slot)][:]
		for i, b := range payload {
			residual[i] ^= mul[b]
		}
	}

	missingSlot := int(missingIdx - blockStart)
	coeff := coeffs.at(int(rescueIndex), missingSlot)
	if coeff == 0 {
		return false
	}
	mul := gfMulTable[gfInv(coeff)][:]
	size := dataSizeForFrame(missingIdx, totalBytes, cfg.PayloadSize)
	payload := recoveredPayload(workspace, size)
	for i := 0; i < size; i++ {
		payload[i] = mul[residual[i]]
	}
	data[missingIdx] = payload
	return true
}

func recoverTwoMissingFrames16x2(data [][]byte, rescues [][]byte, blockStart, blockEnd, aIdx, bIdx uint32, r0, r1 uint16, totalBytes uint64, cfg Config, coeffs coefficientTable, residual0, residual1 []byte, workspace *reconstructWorkspace) bool {
	r0Payload := rescues[r0]
	r1Payload := rescues[r1]
	if len(r0Payload) < cfg.PayloadSize || len(r1Payload) < cfg.PayloadSize {
		return false
	}

	copy(residual0, r0Payload[:cfg.PayloadSize])
	copy(residual1, r1Payload[:cfg.PayloadSize])

	for idx := blockStart; idx < blockEnd; idx++ {
		payload := data[idx]
		if payload == nil {
			continue
		}
		slot := int(idx - blockStart)
		mul0 := gfMulTable[coeffs.at(int(r0), slot)][:]
		mul1 := gfMulTable[coeffs.at(int(r1), slot)][:]
		for i, b := range payload {
			residual0[i] ^= mul0[b]
			residual1[i] ^= mul1[b]
		}
	}

	aSlot := int(aIdx - blockStart)
	bSlot := int(bIdx - blockStart)
	a0 := coeffs.at(int(r0), aSlot)
	b0 := coeffs.at(int(r0), bSlot)
	a1 := coeffs.at(int(r1), aSlot)
	b1 := coeffs.at(int(r1), bSlot)

	det := gfMul(a0, b1) ^ gfMul(a1, b0)
	if det == 0 {
		return false
	}
	invDet := gfInv(det)

	aSize := dataSizeForFrame(aIdx, totalBytes, cfg.PayloadSize)
	bSize := dataSizeForFrame(bIdx, totalBytes, cfg.PayloadSize)
	aPayload := recoveredPayload(workspace, aSize)
	bPayload := recoveredPayload(workspace, bSize)
	aFromR0 := gfMulTable[gfMul(invDet, b1)][:]
	aFromR1 := gfMulTable[gfMul(invDet, b0)][:]
	bFromR0 := gfMulTable[gfMul(invDet, a1)][:]
	bFromR1 := gfMulTable[gfMul(invDet, a0)][:]
	maxSize := aSize
	if bSize > maxSize {
		maxSize = bSize
	}
	for i := 0; i < maxSize; i++ {
		aByte := aFromR0[residual0[i]] ^ aFromR1[residual1[i]]
		bByte := bFromR0[residual0[i]] ^ bFromR1[residual1[i]]
		if i < aSize {
			aPayload[i] = aByte
		}
		if i < bSize {
			bPayload[i] = bByte
		}
	}

	data[aIdx] = aPayload
	data[bIdx] = bPayload
	return true
}

func availableRescueIndexes(rescues [][]byte, payloadSize int) []uint16 {
	return availableRescueIndexesTo(nil, rescues, payloadSize)
}

func availableRescueIndexesTo(indexes []uint16, rescues [][]byte, payloadSize int) []uint16 {
	indexes = indexes[:0]
	for rescueIndex, payload := range rescues {
		if len(payload) >= payloadSize {
			indexes = append(indexes, uint16(rescueIndex))
		}
	}
	return indexes
}

func invertMatrix(matrix [][]byte) ([][]byte, bool) {
	n := len(matrix)
	if n == 0 {
		return nil, false
	}

	aug := make([][]byte, n)
	for row := 0; row < n; row++ {
		if len(matrix[row]) != n {
			return nil, false
		}
		aug[row] = make([]byte, n*2)
		copy(aug[row], matrix[row])
		aug[row][n+row] = 1
	}

	for col := 0; col < n; col++ {
		pivot := -1
		for row := col; row < n; row++ {
			if aug[row][col] != 0 {
				pivot = row
				break
			}
		}
		if pivot == -1 {
			return nil, false
		}
		if pivot != col {
			aug[col], aug[pivot] = aug[pivot], aug[col]
		}

		invPivot := gfInv(aug[col][col])
		for c := 0; c < n*2; c++ {
			aug[col][c] = gfMul(aug[col][c], invPivot)
		}

		for row := 0; row < n; row++ {
			if row == col {
				continue
			}
			factor := aug[row][col]
			if factor == 0 {
				continue
			}
			for c := 0; c < n*2; c++ {
				aug[row][c] ^= gfMul(factor, aug[col][c])
			}
		}
	}

	inverse := make([][]byte, n)
	for row := 0; row < n; row++ {
		inverse[row] = make([]byte, n)
		copy(inverse[row], aug[row][n:])
	}
	return inverse, true
}

func inverseCacheKey(blockStart uint32, missing []uint32, rescueIndexes []uint16) string {
	key := make([]byte, 0, 1+len(missing)+len(rescueIndexes)*2)
	key = append(key, byte(len(missing)))
	for _, idx := range missing {
		key = append(key, byte(idx-blockStart))
	}
	for _, rescueIndex := range rescueIndexes {
		key = append(key, byte(rescueIndex>>8), byte(rescueIndex))
	}
	return string(key)
}

func dataSizeForFrame(index uint32, totalBytes uint64, payloadSize int) int {
	start := uint64(index) * uint64(payloadSize)
	if start >= totalBytes {
		return 0
	}
	remaining := totalBytes - start
	if remaining > uint64(payloadSize) {
		return payloadSize
	}
	return int(remaining)
}

func voteUint32(votes map[uint32]int) uint32 {
	var out uint32
	best := -1
	for value, count := range votes {
		if count > best {
			out = value
			best = count
		}
	}
	return out
}

func voteUint64(votes map[uint64]int) uint64 {
	var out uint64
	best := -1
	for value, count := range votes {
		if count > best {
			out = value
			best = count
		}
	}
	return out
}

func voteUint16(votes map[uint16]int) uint16 {
	var out uint16
	best := -1
	for value, count := range votes {
		if count > best {
			out = value
			best = count
		}
	}
	return out
}
