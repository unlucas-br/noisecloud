package weave

import "math"

// Encode turns a payload into systematic data frames plus rescue frames.
// The returned order is transport-friendly: each block emits its data frames
// followed by its rescue frames.
func Encode(data []byte, cfg Config) ([]Frame, error) {
	return EncodeTo(nil, data, cfg)
}

// EncodeTo appends encoded frames to dst and returns the extended slice.
// It lets callers reuse frame slice capacity across repeated encodes.
func EncodeTo(dst []Frame, data []byte, cfg Config) ([]Frame, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return encodeToValidated(dst, data, cfg, newCoefficientTable(cfg), make([][]byte, cfg.DataFramesPerBlock))
}

func encodeToValidated(dst []Frame, data []byte, cfg Config, coeffs coefficientTable, blockPayloads [][]byte) ([]Frame, error) {
	totalDataFrames := int(math.Ceil(float64(len(data)) / float64(cfg.PayloadSize)))
	if totalDataFrames == 0 {
		totalDataFrames = 1
	}
	blocks := (totalDataFrames + cfg.DataFramesPerBlock - 1) / cfg.DataFramesPerBlock
	frames := dst
	if cap(frames)-len(frames) < totalDataFrames+blocks*cfg.RescueFramesPerBlock {
		next := make([]Frame, len(frames), len(frames)+totalDataFrames+blocks*cfg.RescueFramesPerBlock)
		copy(next, frames)
		frames = next
	}

	for block := 0; block < blocks; block++ {
		blockStart := block * cfg.DataFramesPerBlock
		blockEnd := blockStart + cfg.DataFramesPerBlock
		if blockEnd > totalDataFrames {
			blockEnd = totalDataFrames
		}

		clear(blockPayloads)
		for slot := 0; slot < blockEnd-blockStart; slot++ {
			frameIndex := blockStart + slot
			start := frameIndex * cfg.PayloadSize
			end := start + cfg.PayloadSize
			if end > len(data) {
				end = len(data)
			}
			payload := []byte(nil)
			if start < len(data) {
				payload = data[start:end:end]
			}
			blockPayloads[slot] = payload

			frames = append(frames, Frame{
				Header: Header{
					FrameType:      FrameTypeData,
					BlockSize:      uint16(cfg.DataFramesPerBlock),
					FrameIndex:     uint32(frameIndex),
					TotalFrames:    uint32(totalDataFrames),
					TotalDataBytes: uint64(len(data)),
				},
				Payload: payload,
			})
		}

		rescues := buildRescuePayloads(blockPayloads, cfg, coeffs)
		for i, payload := range rescues {
			frames = append(frames, Frame{
				Header: Header{
					FrameType:      FrameTypeRescue,
					BlockSize:      uint16(cfg.DataFramesPerBlock),
					FrameIndex:     uint32(blockStart),
					TotalFrames:    uint32(totalDataFrames),
					RescueIndex:    uint16(i),
					TotalDataBytes: uint64(len(data)),
				},
				Payload: payload,
			})
		}
	}

	return frames, nil
}

// BuildRescuePayloads builds the default rescue frames for a block.
// For custom rescue counts, use Encode with Config.RescueFramesPerBlock.
func BuildRescuePayloads(blockPayloads [][]byte, payloadSize int) [][]byte {
	cfg := Config{
		PayloadSize:          payloadSize,
		DataFramesPerBlock:   len(blockPayloads),
		RescueFramesPerBlock: DefaultRescueCount,
	}
	return buildRescuePayloads(blockPayloads, cfg, newCoefficientTable(cfg))
}

func buildRescuePayloads(blockPayloads [][]byte, cfg Config, coeffs coefficientTable) [][]byte {
	if is16x2Profile(cfg) {
		return buildRescuePayloads16x2(blockPayloads, cfg.PayloadSize, coeffs)
	}

	rescues := make([][]byte, cfg.RescueFramesPerBlock)
	rescueBytes := make([]byte, cfg.RescueFramesPerBlock*cfg.PayloadSize)
	for i := range rescues {
		start := i * cfg.PayloadSize
		end := start + cfg.PayloadSize
		rescues[i] = rescueBytes[start:end:end]
	}

	for rescueIndex := range rescues {
		rescue := rescues[rescueIndex]
		for slot, payload := range blockPayloads {
			if len(payload) == 0 {
				continue
			}
			coeff := coeffs.at(rescueIndex, slot)
			mul := gfMulTable[coeff][:]
			for i, b := range payload {
				rescue[i] ^= mul[b]
			}
		}
	}

	return rescues
}

func buildRescuePayloads16x2(blockPayloads [][]byte, payloadSize int, coeffs coefficientTable) [][]byte {
	rescues := make([][]byte, 2)
	rescueBytes := make([]byte, 2*payloadSize)
	rescues[0] = rescueBytes[:payloadSize:payloadSize]
	rescues[1] = rescueBytes[payloadSize : 2*payloadSize : 2*payloadSize]
	fillRescuePayloads16x2(rescues[0], rescues[1], blockPayloads, coeffs)
	return rescues
}

func fillRescuePayloads16x2(rescue0, rescue1 []byte, blockPayloads [][]byte, coeffs coefficientTable) {
	limit := len(blockPayloads)
	if limit > 16 {
		limit = 16
	}
	for slot := 0; slot < limit; slot++ {
		payload := blockPayloads[slot]
		if len(payload) == 0 {
			continue
		}
		mul0 := gfMulTable[coeffs.at(0, slot)][:]
		mul1 := gfMulTable[coeffs.at(1, slot)][:]
		for i, b := range payload {
			rescue0[i] ^= mul0[b]
			rescue1[i] ^= mul1[b]
		}
	}
}

func is16x2Profile(cfg Config) bool {
	return cfg.DataFramesPerBlock == 16 && cfg.RescueFramesPerBlock == 2
}
