package decoder

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"

	"ncc/internal/encoder"
)

type FrameReconstructor struct {
	FrameCfg encoder.FrameConfig
	ECCCfg   encoder.ECCConfig
	Preset   string
}

// NewFrameReconstructor inicializa o reconstructor com configurações baseadas no preset.
// Como este projeto é uma versão adaptada do original, o foco principal é o modo padrão
// de alta performance e robustez.
func NewFrameReconstructor(preset string) *FrameReconstructor {
	cfg := encoder.DefaultFrameConfig()
	if preset == "hq" {
		cfg = encoder.HQFrameConfig()
	} else if preset == "dense" {
		cfg = encoder.HighDensityFrameConfig()
	} else if preset == "square" {
		cfg = encoder.SquareFrameConfig()
	} else if preset == "tiktok" {
		cfg = encoder.TikTokFrameConfig()
	} else if preset == "weave" {
		cfg = encoder.CompactWeaveFrameConfig()
	}

	eccCfg := encoder.NewECCConfig()
	if preset == "tiktok" {
		eccCfg = encoder.TikTokECCConfig()
	}

	return &FrameReconstructor{
		FrameCfg: cfg,
		ECCCfg:   eccCfg,
		Preset:   preset,
	}
}

type ParsedHeader struct {
	Magic       [4]byte
	FrameIndex  uint32
	TotalFrames uint32
	DataSize    uint32
}

type decodeResult struct {
	index       int
	data        []byte
	frameHeader ParsedHeader
	crcOK       bool
	err         error
}

// ReconstructFile une os frames decodificados e aplica a correção de erro Reed-Solomon
func (fr *FrameReconstructor) ReconstructFile(framePaths []string, outputPath string, progress chan<- float64) error {
	if fr.Preset == "weave" || fr.Preset == "tiktok" {
		return fr.ReconstructWeaveFile(framePaths, outputPath, progress)
	}

	var allData []byte
	var eccWarnings int32

	threads := runtime.NumCPU() - 2
	if threads < 1 {
		threads = 1
	}
	sort.Slice(framePaths, func(i, j int) bool {
		return framePaths[i] < framePaths[j]
	})

	if len(framePaths) > 100 {
		seen := make(map[[32]byte]bool)
		var unique []string
		for _, path := range framePaths {
			data, err := os.ReadFile(path)
			if err != nil {
				unique = append(unique, path)
				continue
			}
			h := sha256.Sum256(data)
			if !seen[h] {
				seen[h] = true
				unique = append(unique, path)
			}
		}
		if len(unique) < len(framePaths) {
			framePaths = unique
		}
	}

	jobChan := make(chan struct {
		i    int
		path string
	}, len(framePaths))
	resultChan := make(chan decodeResult, len(framePaths))

	var wg sync.WaitGroup
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobChan {
				data, header, crcOK, err := fr.processFrame(job.path)
				resultChan <- decodeResult{
					index:       job.i,
					data:        data,
					frameHeader: header,
					crcOK:       crcOK,
					err:         err,
				}
			}
		}()
	}

	for i, path := range framePaths {
		jobChan <- struct {
			i    int
			path string
		}{i, path}
	}
	close(jobChan)

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	frameByIndex := make(map[uint32]decodeResult)
	totalFrameVotes := make(map[uint32]int)
	var processed int
	var errCount int

	for res := range resultChan {
		if res.err != nil {
			errCount++
			continue
		}
		if !res.crcOK {
			atomic.AddInt32(&eccWarnings, 1)
		}

		if res.frameHeader.TotalFrames == 0 || res.frameHeader.FrameIndex >= res.frameHeader.TotalFrames {
			errCount++
			continue
		}

		fi := res.frameHeader.FrameIndex
		if existing, ok := frameByIndex[fi]; ok {
			if !existing.crcOK && res.crcOK {
				frameByIndex[fi] = res
			}
		} else {
			frameByIndex[fi] = res
		}
		totalFrameVotes[res.frameHeader.TotalFrames]++

		processed++
		if progress != nil {
			progress <- float64(processed) / float64(len(framePaths))
		}
	}

	if len(frameByIndex) == 0 {
		return fmt.Errorf("nenhum quadro válido decodificado (total de erros: %d)", errCount)
	}

	var expectedFrames uint32
	var expectedVotes int
	for total, votes := range totalFrameVotes {
		if votes > expectedVotes {
			expectedFrames = total
			expectedVotes = votes
		}
	}

	if expectedFrames > 0 {
		for fi, res := range frameByIndex {
			if res.frameHeader.TotalFrames != expectedFrames || fi >= expectedFrames {
				delete(frameByIndex, fi)
			}
		}
	}

	var maxIdx uint32
	if expectedFrames > 0 {
		maxIdx = expectedFrames - 1
	} else {
		for fi := range frameByIndex {
			if fi > maxIdx {
				maxIdx = fi
			}
		}
	}

	var missingFrames int
	var unverifiedFrames int
	for i := uint32(0); i <= maxIdx; i++ {
		res, ok := frameByIndex[i]
		if !ok {
			missingFrames++
			continue
		}
		if !res.crcOK {
			unverifiedFrames++
		}
		allData = append(allData, res.data...)
	}

	_ = errCount
	_ = missingFrames
	_ = unverifiedFrames
	_ = eccWarnings
	return os.WriteFile(outputPath, allData, 0644)
}

func verifySHA256(data []byte, expected []byte) bool {
	hash := sha256.Sum256(data)
	return bytes.Equal(hash[:], expected)
}

type frameCandidate struct {
	data   []byte
	header ParsedHeader
	eccOK  bool
}

// processFrame converte o PNG de volta em dados binários brutos.
func (fr *FrameReconstructor) processFrame(path string) ([]byte, ParsedHeader, bool, error) {
	var emptyHeader ParsedHeader

	f, err := os.Open(path)
	if err != nil {
		return nil, emptyHeader, false, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, emptyHeader, false, fmt.Errorf("falha ao decodificar png: %w", err)
	}

	type attempt struct {
		isDiff    bool
		calAdjust int
		offX      int
		offY      int
		thrAdjust int
	}

	attempts := []attempt{
		{false, 0, 0, 0, 0},
		{false, 0, 0, -1, 0},
		{false, 0, 0, 1, 0},
		{false, 0, -1, 0, 0},
		{false, 0, 1, 0, 0},
		{false, -1, 0, 0, 0},
		{false, +1, 0, 0, 0},
		{false, 0, 0, 0, -10},
		{false, 0, 0, 0, +10},
		{false, 0, 0, 0, -20},
		{false, 0, 0, 0, +20},
		{true, 0, 0, 0, 0},
	}

	var fallback *frameCandidate
	var lastErr error

	for _, baseCfg := range fr.candidateFrameConfigs() {
		localCfg := getScaledConfigFor(baseCfg, img.Bounds())

		threshold, _ := calibrateFrame(img, localCfg)
		if threshold == 0 {
			threshold = 128
		}
		levels, _ := calibrateLevels(img, localCfg)

		for _, a := range attempts {
			tryCfg := localCfg
			if a.calAdjust != 0 {
				tryCfg.CalibrationHeight += a.calAdjust * localCfg.MacroSize
				if tryCfg.CalibrationHeight < 0 || tryCfg.CalibrationHeight >= img.Bounds().Dy() {
					continue
				}
			}

			tryThreshold := adjustThreshold(threshold, a.thrAdjust)
			allBytes, err := readBytesFromImage(img, baseCfg, tryCfg, tryThreshold, levels, a.offX, a.offY, a.isDiff)
			if err != nil {
				lastErr = err
				continue
			}

			bytesInFrame := baseCfg.TotalBytesCapacity()
			if bytesInFrame > len(allBytes) {
				bytesInFrame = len(allBytes)
			}

			for _, eccCfg := range fr.candidateECCConfigs(baseCfg) {
				candidate, err := fr.tryDecodePayload(allBytes, bytesInFrame, eccCfg)
				if err != nil {
					lastErr = err
					continue
				}

				if candidate.eccOK {
					return candidate.data, candidate.header, true, nil
				}
				if fallback == nil {
					copied := candidate
					fallback = &copied
				}
			}
		}
	}

	if fallback != nil {
		return fallback.data, fallback.header, false, nil
	}
	return nil, emptyHeader, false, lastErr
}

func (fr *FrameReconstructor) tryDecodePayload(allBytes []byte, bytesInFrame int, eccCfg encoder.ECCConfig) (frameCandidate, error) {
	totalShards := eccCfg.DataShards + eccCfg.ParityShards
	maxShardSize := bytesInFrame / totalShards
	if maxShardSize == 0 {
		return frameCandidate{}, fmt.Errorf("capacidade insuficiente para fragmentos ECC")
	}

	var fallback *frameCandidate
	var lastErr error
	minShardSize := (encoder.FramePlainHeaderSize + eccCfg.DataShards - 1) / eccCfg.DataShards
	for shardSize := maxShardSize; shardSize >= minShardSize; shardSize-- {
		candidate, err := fr.tryDecodePayloadAtShardSize(allBytes, totalShards, shardSize, eccCfg, false)
		if err != nil {
			lastErr = err
			continue
		}
		if candidate.eccOK {
			return candidate, nil
		}
		if fallback == nil {
			copied := candidate
			fallback = &copied
		}
	}

	for shardSize := maxShardSize; shardSize >= minShardSize; shardSize-- {
		candidate, err := fr.tryDecodePayloadAtShardSize(allBytes, totalShards, shardSize, eccCfg, true)
		if err != nil {
			lastErr = err
			continue
		}
		if candidate.eccOK {
			return candidate, nil
		}
		if fallback == nil {
			copied := candidate
			fallback = &copied
		}
	}

	if fallback != nil {
		return *fallback, nil
	}
	if lastErr != nil {
		return frameCandidate{}, lastErr
	}
	return frameCandidate{}, fmt.Errorf("falha ao decodificar quadro")
}

func (fr *FrameReconstructor) tryDecodePayloadAtShardSize(allBytes []byte, totalShards, shardSize int, eccCfg encoder.ECCConfig, allowRepair bool) (frameCandidate, error) {
	eccBytes := shardSize * totalShards
	if eccBytes > len(allBytes) {
		return frameCandidate{}, fmt.Errorf("dados insuficientes para fragmentos (shards)")
	}

	ecc, err := encoder.NewECCEncoder(eccCfg)
	if err != nil {
		return frameCandidate{}, err
	}

	shards := splitShards(allBytes[:eccBytes], totalShards, shardSize)
	ok, _ := ecc.Verify(shards)
	if ok {
		fullPayload, err := ecc.Join(shards, shardSize*eccCfg.DataShards)
		if err != nil {
			return frameCandidate{}, fmt.Errorf("falha ao unir dados do ECC: %w", err)
		}
		header, data, err := parsePlainFrame(fullPayload, shardSize*eccCfg.DataShards)
		if err != nil {
			return frameCandidate{}, err
		}
		return frameCandidate{data: data, header: header, eccOK: true}, nil
	}

	fullPayload, err := ecc.Join(shards, shardSize*eccCfg.DataShards)
	if err != nil {
		return frameCandidate{}, fmt.Errorf("falha ao unir dados do ECC: %w", err)
	}

	var fallback *frameCandidate
	var parseErr error
	header, data, err := parsePlainFrame(fullPayload, shardSize*eccCfg.DataShards)
	if err == nil {
		fallback = &frameCandidate{data: data, header: header, eccOK: false}
	} else {
		parseErr = err
	}

	if !allowRepair {
		if fallback != nil {
			return *fallback, nil
		}
		if parseErr != nil {
			return frameCandidate{}, parseErr
		}
		return frameCandidate{}, fmt.Errorf("falha ao decodificar quadro com ECC inválido")
	}

	maxRepairErasures := eccCfg.ParityShards
	if maxRepairErasures > 6 {
		maxRepairErasures = 4
	}
	if repairCorruptShards(ecc, shards, maxRepairErasures) {
		fullPayload, err = ecc.Join(shards, shardSize*eccCfg.DataShards)
		if err != nil {
			return frameCandidate{}, fmt.Errorf("falha ao unir dados do ECC reparado: %w", err)
		}
		repairedHeader, repairedData, err := parsePlainFrame(fullPayload, shardSize*eccCfg.DataShards)
		if err != nil {
			return frameCandidate{}, err
		}
		return frameCandidate{data: repairedData, header: repairedHeader, eccOK: true}, nil
	}

	if fallback != nil {
		return *fallback, nil
	}
	if parseErr != nil {
		return frameCandidate{}, parseErr
	}
	return frameCandidate{}, fmt.Errorf("falha ao decodificar quadro com ECC inválido")
}

func parsePlainFrame(plaintext []byte, maxDataBytes int) (ParsedHeader, []byte, error) {
	var header ParsedHeader
	if len(plaintext) < encoder.FramePlainHeaderSize {
		return header, nil, fmt.Errorf("dados do frame muito pequenos para o cabeçalho")
	}

	copy(header.Magic[:], plaintext[0:4])
	if header.Magic != [4]byte{'N', 'C', 'C', '3'} {
		return header, nil, fmt.Errorf("magic inválido no quadro decodificado: %v", header.Magic)
	}

	header.FrameIndex = binary.BigEndian.Uint32(plaintext[4:8])
	header.TotalFrames = binary.BigEndian.Uint32(plaintext[8:12])
	header.DataSize = binary.BigEndian.Uint32(plaintext[12:16])

	maxPayloadBytes := maxDataBytes - encoder.FramePlainHeaderSize
	if maxPayloadBytes < 0 {
		return header, nil, fmt.Errorf("capacidade negativa para payload")
	}
	if header.TotalFrames == 0 || header.FrameIndex >= header.TotalFrames {
		return header, nil, fmt.Errorf("cabeçalho inconsistente")
	}
	if int(header.DataSize) > maxPayloadBytes {
		return header, nil, fmt.Errorf("tamanho de dados corrompido")
	}

	payloadStart := encoder.FramePlainHeaderSize
	payloadEnd := payloadStart + int(header.DataSize)
	if payloadEnd > len(plaintext) {
		return header, nil, fmt.Errorf("tamanho de dados corrompido")
	}

	actualData := make([]byte, header.DataSize)
	copy(actualData, plaintext[payloadStart:payloadEnd])
	return header, actualData, nil
}

func splitShards(data []byte, totalShards int, shardSize int) [][]byte {
	shards := make([][]byte, totalShards)
	for i := range shards {
		shards[i] = data[i*shardSize : (i+1)*shardSize]
	}
	return shards
}

func repairCorruptShards(ecc *encoder.ECCEncoder, shards [][]byte, maxErasures int) bool {
	if ok, _ := ecc.Verify(shards); ok {
		return true
	}
	if maxErasures > len(shards) {
		maxErasures = len(shards)
	}

	for erased := 1; erased <= maxErasures; erased++ {
		combo := make([]int, 0, erased)
		if tryShardErasures(ecc, shards, combo, erased, 0) {
			return true
		}
	}
	return false
}

func tryShardErasures(ecc *encoder.ECCEncoder, original [][]byte, combo []int, target, start int) bool {
	if len(combo) == target {
		candidate := cloneShards(original)
		for _, idx := range combo {
			candidate[idx] = nil
		}
		if err := ecc.Reconstruct(candidate); err != nil {
			return false
		}
		if ok, _ := ecc.Verify(candidate); !ok {
			return false
		}
		for i := range original {
			copy(original[i], candidate[i])
		}
		return true
	}

	remaining := target - len(combo)
	for i := start; i <= len(original)-remaining; i++ {
		combo = append(combo, i)
		if tryShardErasures(ecc, original, combo, target, i+1) {
			return true
		}
		combo = combo[:len(combo)-1]
	}
	return false
}

func cloneShards(shards [][]byte) [][]byte {
	cloned := make([][]byte, len(shards))
	for i, shard := range shards {
		if shard == nil {
			continue
		}
		cloned[i] = make([]byte, len(shard))
		copy(cloned[i], shard)
	}
	return cloned
}

func adjustThreshold(base byte, delta int) byte {
	v := int(base) + delta
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

func (fr *FrameReconstructor) candidateFrameConfigs() []encoder.FrameConfig {
	candidates := []encoder.FrameConfig{fr.FrameCfg}
	if fr.Preset == "tiktok" {
		candidates = append(candidates, encoder.HQFrameConfig(), encoder.DefaultFrameConfig(), encoder.SquareFrameConfig())
	} else if fr.Preset == "weave" {
		candidates = append(candidates, encoder.HighDensityFrameConfig(), encoder.DefaultFrameConfig(), encoder.HQFrameConfig())
	} else if fr.Preset == "hq" {
		candidates = append(candidates, encoder.TikTokFrameConfig())
	}

	seen := make(map[encoder.FrameConfig]bool)
	unique := make([]encoder.FrameConfig, 0, len(candidates))
	for _, cfg := range candidates {
		if seen[cfg] {
			continue
		}
		seen[cfg] = true
		unique = append(unique, cfg)
	}
	return unique
}

func (fr *FrameReconstructor) candidateECCConfigs(baseCfg encoder.FrameConfig) []encoder.ECCConfig {
	candidates := []encoder.ECCConfig{fr.ECCCfg}
	if baseCfg == encoder.TikTokFrameConfig() {
		candidates = append(candidates, encoder.TikTokECCConfig())
	} else {
		candidates = append(candidates, encoder.NewECCConfig())
	}
	candidates = append(candidates, encoder.NewECCConfig(), encoder.TikTokECCConfig())

	seen := make(map[encoder.ECCConfig]bool)
	unique := make([]encoder.ECCConfig, 0, len(candidates))
	for _, cfg := range candidates {
		if seen[cfg] {
			continue
		}
		seen[cfg] = true
		unique = append(unique, cfg)
	}
	return unique
}

// getScaledConfig recalcula as configurações de grade para as dimensões reais do frame.
// O TikTok redimensiona vídeos (ex: 1080→576, 1920→1024), então os parâmetros precisam
// ser ajustados proporcionalmente para cada frame decodificado.
// MacroSize é escalonado pela largura; CalibrationHeight é escalonado pela ALTURA,
// pois a barra de calibração ocupa espaço vertical e os dados começam abaixo dela.
func (fr *FrameReconstructor) getScaledConfig(bounds image.Rectangle) encoder.FrameConfig {
	return getScaledConfigFor(fr.FrameCfg, bounds)
}

func getScaledConfigFor(base encoder.FrameConfig, bounds image.Rectangle) encoder.FrameConfig {
	w, h := bounds.Dx(), bounds.Dy()

	if w == base.Width && h == base.Height {
		return base
	}

	scaled := base
	origW := base.Width
	origH := base.Height

	if origW > 0 {
		// MacroSize escala pela largura (os pixels de dados são quadrados)
		scaled.MacroSize = (scaled.MacroSize*w + origW/2) / origW
		if scaled.MacroSize < 4 {
			scaled.MacroSize = 4
		}
	}

	if origH > 0 {
		// CalibrationHeight escala pela ALTURA, pois é uma faixa horizontal
		scaled.CalibrationHeight = (scaled.CalibrationHeight*h + origH/2) / origH
		if scaled.CalibrationHeight < 4 {
			scaled.CalibrationHeight = 4
		}
	}

	scaled.Width = w
	scaled.Height = h
	return scaled
}

func calibrateFrame(img image.Image, cfg encoder.FrameConfig) (byte, error) {
	bounds := img.Bounds()
	width := bounds.Dx()
	sectionWidth := width / 4
	sampleY := 0
	calHeight := cfg.CalibrationHeight
	blackAvg := measureSectionAverage(img, 0, sampleY, sectionWidth, calHeight)
	whiteAvg := measureSectionAverage(img, 3*sectionWidth, sampleY, sectionWidth, calHeight)
	threshold := uint8((int(blackAvg) + int(whiteAvg)) / 2)
	return byte(threshold), nil
}

func calibrateLevels(img image.Image, cfg encoder.FrameConfig) ([3]uint8, error) {
	bounds := img.Bounds()
	width := bounds.Dx()
	sectionWidth := width / 4
	sampleY := 0
	calHeight := cfg.CalibrationHeight
	blackAvg := float64(measureSectionAverage(img, 0, sampleY, sectionWidth, calHeight))
	whiteAvg := float64(measureSectionAverage(img, 3*sectionWidth, sampleY, sectionWidth, calHeight))
	rng := whiteAvg - blackAvg
	if rng < 10 {
		return [3]uint8{64, 128, 192}, nil
	}
	t1 := uint8(blackAvg + rng*(1.0/6.0))
	t2 := uint8(blackAvg + rng*(0.5))
	t3 := uint8(blackAvg + rng*(5.0/6.0))
	return [3]uint8{t1, t2, t3}, nil
}

func measureSectionAverage(img image.Image, startX, startY, w, h int) uint8 {
	var sum uint32
	var count uint32
	marginX := w / 4
	marginY := h / 4
	for y := startY + marginY; y < startY+h-marginY; y++ {
		for x := startX + marginX; x < startX+w-marginX; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			sum += r >> 8
			count++
		}
	}
	if count == 0 {
		return 128
	}
	return uint8(sum / count)
}

func extractMacroPixel(img image.Image, cfg encoder.FrameConfig, startX, startY int) (y, u, v uint8) {
	var sumR uint32
	realY := startY + cfg.CalibrationHeight
	bounds := img.Bounds()
	macroSize := cfg.MacroSize

	count := 0
	for dy := 0; dy < macroSize; dy++ {
		for dx := 0; dx < macroSize; dx++ {
			px := startX + dx
			py := realY + dy
			if px >= bounds.Dx() || py >= bounds.Dy() {
				continue
			}
			r, _, _, _ := img.At(bounds.Min.X+px, bounds.Min.Y+py).RGBA()
			sumR += r >> 8
			count++
		}
	}

	if count == 0 {
		return 0, 128, 128
	}
	avgR := uint8(sumR / uint32(count))
	return avgR, 128, 128
}

func extractDifferentialMacroPixel(img image.Image, cfg encoder.FrameConfig, startX, startY int) byte {
	realY := startY + cfg.CalibrationHeight
	bounds := img.Bounds()
	macroSize := cfg.MacroSize
	halfSize := macroSize / 2

	var leftSum uint32
	var rightSum uint32
	var leftCount uint32
	var rightCount uint32

	for dy := 0; dy < macroSize; dy++ {
		py := realY + dy
		if py >= bounds.Dy() {
			continue
		}

		for dx := 0; dx < macroSize; dx++ {
			px := startX + dx
			if px >= bounds.Dx() {
				continue
			}

			r, _, _, _ := img.At(bounds.Min.X+px, bounds.Min.Y+py).RGBA()
			brightness := r >> 8

			if dx < halfSize {
				leftSum += brightness
				leftCount++
			} else {
				rightSum += brightness
				rightCount++
			}
		}
	}

	if leftCount == 0 || rightCount == 0 {
		return 0
	}

	leftAvg := float64(leftSum) / float64(leftCount)
	rightAvg := float64(rightSum) / float64(rightCount)

	avgLuma := (leftAvg + rightAvg) / 2.0
	lumaFactor := 1.0 - math.Pow((avgLuma-128)/128, 2)

	if lumaFactor < 0.25 {
		lumaFactor = 0.25
	}

	expectedDelta := 20.0 * lumaFactor

	target1Left := avgLuma + expectedDelta
	target1Right := avgLuma - expectedDelta
	target0Left := avgLuma - expectedDelta
	target0Right := avgLuma + expectedDelta

	distTo1 := math.Abs(leftAvg-target1Left) + math.Abs(rightAvg-target1Right)
	distTo0 := math.Abs(leftAvg-target0Left) + math.Abs(rightAvg-target0Right)

	if distTo1 < distTo0 {
		return 1
	}
	return 0
}

func readBytesFromImage(img image.Image, baseCfg, scaledCfg encoder.FrameConfig, threshold byte, thresholds [3]uint8, offX, offY int, differential bool) ([]byte, error) {
	cols, rows := baseCfg.GridSize()

	var bits []byte
	type coord struct{ x, y int }
	coords := make([]coord, 0, cols*rows)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			coords = append(coords, coord{x, y})
		}
	}

	for _, c := range coords {
		x0, y0, x1, y1 := scaledMacroBounds(img.Bounds(), baseCfg, scaledCfg, c.x, c.y, offX, offY)

		var val byte
		if differential {
			val = extractDifferentialMacroPixelRect(img, x0, y0, x1, y1)
		} else {
			avgY := extractMacroPixelRect(img, x0, y0, x1, y1)

			if baseCfg.GrayLevels == 2 {
				if avgY >= threshold {
					val = 1
				} else {
					val = 0
				}
			} else {
				val = encoder.DynGrayToNibble(avgY, thresholds)
			}
		}
		bits = append(bits, val)
	}

	var allBytes []byte
	if baseCfg.GrayLevels == 2 {
		for i := 0; i+7 < len(bits); i += 8 {
			b := (bits[i] << 7) | (bits[i+1] << 6) | (bits[i+2] << 5) | (bits[i+3] << 4) |
				(bits[i+4] << 3) | (bits[i+5] << 2) | (bits[i+6] << 1) | bits[i+7]
			allBytes = append(allBytes, b)
		}
	} else {
		for i := 0; i+3 < len(bits); i += 4 {
			b := (bits[i] << 6) | (bits[i+1] << 4) | (bits[i+2] << 2) | bits[i+3]
			allBytes = append(allBytes, b)
		}
	}
	return allBytes, nil
}

func scaledMacroBounds(bounds image.Rectangle, baseCfg, scaledCfg encoder.FrameConfig, col, row, offX, offY int) (int, int, int, int) {
	w, h := bounds.Dx(), bounds.Dy()
	scaleX := float64(w) / float64(baseCfg.Width)

	baseDataHeight := baseCfg.Height - baseCfg.CalibrationHeight
	scaledDataHeight := h - scaledCfg.CalibrationHeight
	scaleY := float64(scaledDataHeight) / float64(baseDataHeight)
	if baseDataHeight <= 0 || scaledDataHeight <= 0 {
		scaleY = float64(h) / float64(baseCfg.Height)
	}

	x0 := int(math.Round(float64(col*baseCfg.MacroSize)*scaleX)) + offX
	x1 := int(math.Round(float64((col+1)*baseCfg.MacroSize)*scaleX)) + offX
	y0 := scaledCfg.CalibrationHeight + int(math.Round(float64(row*baseCfg.MacroSize)*scaleY)) + offY
	y1 := scaledCfg.CalibrationHeight + int(math.Round(float64((row+1)*baseCfg.MacroSize)*scaleY)) + offY

	x0 = clampInt(x0, 0, w)
	x1 = clampInt(x1, 0, w)
	y0 = clampInt(y0, 0, h)
	y1 = clampInt(y1, 0, h)

	if x1 <= x0 {
		x1 = clampInt(x0+1, 0, w)
	}
	if y1 <= y0 {
		y1 = clampInt(y0+1, 0, h)
	}
	return x0, y0, x1, y1
}

func extractMacroPixelRect(img image.Image, x0, y0, x1, y1 int) uint8 {
	x0, y0, x1, y1 = insetSampleRect(x0, y0, x1, y1)
	bounds := img.Bounds()

	var sum uint32
	var count uint32
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, _, _, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			sum += r >> 8
			count++
		}
	}
	if count == 0 {
		return 128
	}
	return uint8(sum / count)
}

func extractDifferentialMacroPixelRect(img image.Image, x0, y0, x1, y1 int) byte {
	x0, y0, x1, y1 = insetSampleRect(x0, y0, x1, y1)
	bounds := img.Bounds()
	midX := x0 + (x1-x0)/2

	var leftSum uint32
	var rightSum uint32
	var leftCount uint32
	var rightCount uint32

	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, _, _, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			brightness := r >> 8
			if x < midX {
				leftSum += brightness
				leftCount++
			} else {
				rightSum += brightness
				rightCount++
			}
		}
	}

	if leftCount == 0 || rightCount == 0 {
		return 0
	}

	leftAvg := float64(leftSum) / float64(leftCount)
	rightAvg := float64(rightSum) / float64(rightCount)
	avgLuma := (leftAvg + rightAvg) / 2.0
	lumaFactor := 1.0 - math.Pow((avgLuma-128)/128, 2)
	if lumaFactor < 0.25 {
		lumaFactor = 0.25
	}

	expectedDelta := 20.0 * lumaFactor
	target1Left := avgLuma + expectedDelta
	target1Right := avgLuma - expectedDelta
	target0Left := avgLuma - expectedDelta
	target0Right := avgLuma + expectedDelta

	distTo1 := math.Abs(leftAvg-target1Left) + math.Abs(rightAvg-target1Right)
	distTo0 := math.Abs(leftAvg-target0Left) + math.Abs(rightAvg-target0Right)
	if distTo1 < distTo0 {
		return 1
	}
	return 0
}

func insetSampleRect(x0, y0, x1, y1 int) (int, int, int, int) {
	w := x1 - x0
	h := y1 - y0
	marginX := w / 4
	marginY := h / 4
	if w-2*marginX < 2 {
		marginX = 0
	}
	if h-2*marginY < 2 {
		marginY = 0
	}
	return x0 + marginX, y0 + marginY, x1 - marginX, y1 - marginY
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
