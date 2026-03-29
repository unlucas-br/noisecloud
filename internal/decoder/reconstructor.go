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

	cfgOnce   sync.Once
	cfgScaled encoder.FrameConfig
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
	}

	return &FrameReconstructor{
		FrameCfg:  cfg,
		cfgScaled: cfg,
		ECCCfg:    encoder.NewECCConfig(),
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
	var allData []byte
	var crcWarnings int32

	threads := runtime.NumCPU() - 2
	if threads < 1 {
		threads = 1
	}
	fmt.Printf("Usando %d threads para reconstrução\n", threads)

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
			fmt.Printf("Deduplicação: %d → %d frames únicos\n",
				len(framePaths), len(unique))
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
	var processed int
	var errCount int

	for res := range resultChan {
		if res.err != nil {
			errCount++
			if errCount <= 3 {
				fmt.Fprintf(os.Stderr, "Frame %d error: %v\n", res.index, res.err)
			}
			continue
		}
		if !res.crcOK {
			atomic.AddInt32(&crcWarnings, 1)
		}

		fi := res.frameHeader.FrameIndex
		if existing, ok := frameByIndex[fi]; ok {
			if !existing.crcOK && res.crcOK {
				frameByIndex[fi] = res
			}
		} else {
			frameByIndex[fi] = res
		}

		processed++
		if progress != nil {
			progress <- float64(processed) / float64(len(framePaths))
		}
	}

	if len(frameByIndex) == 0 {
		return fmt.Errorf("nenhum quadro válido decodificado (total de erros: %d)", errCount)
	}

	if errCount > 0 {
		fmt.Printf("%d frames ignorados\n", errCount)
	}
	fmt.Printf("Frames decodificados: %d (de %d extraídos)\n",
		len(frameByIndex), len(framePaths))

	fmt.Println("Montando arquivo final...")

	var maxIdx uint32
	for fi := range frameByIndex {
		if fi > maxIdx {
			maxIdx = fi
		}
	}

	var missingFrames int
	for i := uint32(0); i <= maxIdx; i++ {
		res, ok := frameByIndex[i]
		if !ok {
			missingFrames++
			if missingFrames <= 5 {
				fmt.Fprintf(os.Stderr, "Missing frame %d\n", i)
			}
			continue
		}
		allData = append(allData, res.data...)
	}

	if missingFrames > 0 {
		fmt.Fprintf(os.Stderr, "\n❌ AVISO: %d frames faltando!\n", missingFrames)
	}

	if crcWarnings > 0 {
		fmt.Fprintf(os.Stderr, "\nTotal CRC warnings: %d/%d frames\n", crcWarnings, len(framePaths))
	}

	fmt.Println("✅ Arquivo reconstruído com sucesso")
	return os.WriteFile(outputPath, allData, 0644)
}

func verifySHA256(data []byte, expected []byte) bool {
	hash := sha256.Sum256(data)
	return bytes.Equal(hash[:], expected)
}

// processFrame converte o PNG de volta em dados binários brutos (Luma/Differential)
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

	localCfg := fr.getScaledConfig(img.Bounds())

	threshold, _ := calibrateFrame(img, localCfg)
	if threshold == 0 {
		threshold = 128
	}
	levels, _ := calibrateLevels(img, localCfg)

	extractionMethods := []bool{false, true}
	var lastErr error

	for _, isDiff := range extractionMethods {
		allBytes, err := readBytesFromImage(img, localCfg, threshold, levels, 0, 0, isDiff)
		if err != nil {
			lastErr = err
			continue
		}

		cols, rows := localCfg.GridSize()
		totalMacros := cols * rows
		bytesInFrame := totalMacros / (8 / localCfg.GrayLevels)

		if bytesInFrame > len(allBytes) {
			bytesInFrame = len(allBytes)
		}

		eccCfg := fr.ECCCfg
		ecc, err := encoder.NewECCEncoder(eccCfg)
		if err != nil {
			lastErr = err
			continue
		}

		totalShards := eccCfg.DataShards + eccCfg.ParityShards
		maxShardSize := bytesInFrame / totalShards
		eccBytes := maxShardSize * totalShards

		if eccBytes > len(allBytes) {
			lastErr = fmt.Errorf("dados insuficientes para fragmentos (shards)")
			continue
		}

		shardsData := allBytes[:eccBytes]
		shards := make([][]byte, totalShards)
		for i := range shards {
			shards[i] = shardsData[i*maxShardSize : (i+1)*maxShardSize]
		}

		var crcOK bool
		crcOK = true
		ok, _ := ecc.Verify(shards)
		if !ok {
			crcOK = false
			if err := ecc.Reconstruct(shards); err != nil {
				lastErr = fmt.Errorf("ECC irrecuperável: %w", err)
				continue
			}
		}

		fullPayload, err := ecc.Join(shards, maxShardSize*eccCfg.DataShards)
		if err != nil {
			lastErr = fmt.Errorf("falha ao unir dados do ECC: %w", err)
			continue
		}

		plaintext := fullPayload

		if len(plaintext) < encoder.FramePlainHeaderSize {
			lastErr = fmt.Errorf("dados do frame muito pequenos para o cabeçalho")
			continue
		}

		var header ParsedHeader
		copy(header.Magic[:], plaintext[0:4])
		if header.Magic != [4]byte{'N', 'C', 'C', '3'} {
			lastErr = fmt.Errorf("magic inválido no quadro decodificado: %v", header.Magic)
			continue
		}

		header.FrameIndex = binary.BigEndian.Uint32(plaintext[4:8])
		header.TotalFrames = binary.BigEndian.Uint32(plaintext[8:12])
		header.DataSize = binary.BigEndian.Uint32(plaintext[12:16])

		payloadStart := encoder.FramePlainHeaderSize
		payloadEnd := payloadStart + int(header.DataSize)

		if payloadEnd > len(plaintext) {
			lastErr = fmt.Errorf("tamanho de dados corrompido")
			continue
		}

		actualData := make([]byte, header.DataSize)
		copy(actualData, plaintext[payloadStart:payloadEnd])

		return actualData, header, crcOK, nil
	}

	return nil, emptyHeader, false, lastErr
}

func (fr *FrameReconstructor) getScaledConfig(bounds image.Rectangle) encoder.FrameConfig {
	w, h := bounds.Dx(), bounds.Dy()

	if w == fr.FrameCfg.Width && h == fr.FrameCfg.Height {
		return fr.FrameCfg
	}

	fr.cfgOnce.Do(func() {
		scaled := fr.FrameCfg
		origW := scaled.Width
		if origW > 0 {
			scaled.MacroSize = (scaled.MacroSize*w + origW/2) / origW
			scaled.CalibrationHeight = (scaled.CalibrationHeight*w + origW/2) / origW
			if scaled.MacroSize < 4 {
				scaled.MacroSize = 4
			}
			if scaled.CalibrationHeight < 4 {
				scaled.CalibrationHeight = 4
			}
		}
		scaled.Width = w
		scaled.Height = h
		fr.cfgScaled = scaled
	})

	return fr.cfgScaled
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

func readBytesFromImage(img image.Image, cfg encoder.FrameConfig, threshold byte, thresholds [3]uint8, offX, offY int, differential bool) ([]byte, error) {
	cols, rows := cfg.GridSize()
	macroSize := cfg.MacroSize

	var bits []byte
	type coord struct{ x, y int }
	coords := make([]coord, 0, cols*rows)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			coords = append(coords, coord{x, y})
		}
	}

	for _, c := range coords {
		targetX := c.x*macroSize + offX
		targetY := c.y*macroSize + offY

		var val byte
		if differential {
			val = extractDifferentialMacroPixel(img, cfg, targetX, targetY)
		} else {
			avgY, _, _ := extractMacroPixel(img, cfg, targetX, targetY)

			if cfg.GrayLevels == 2 {
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
	if cfg.GrayLevels == 2 {
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
