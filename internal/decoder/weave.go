package decoder

import (
	"fmt"
	"image"
	"os"
	"runtime"
	"sync"

	"ncc/internal/weave"
)

type weaveDecodeResult struct {
	index int
	frame weave.Frame
	err   error
}

func (fr *FrameReconstructor) ReconstructWeaveFile(framePaths []string, outputPath string, progress chan<- float64) error {
	threads := runtime.NumCPU() - 2
	if threads < 1 {
		threads = 1
	}

	jobChan := make(chan struct {
		i    int
		path string
	}, len(framePaths))
	resultChan := make(chan weaveDecodeResult, len(framePaths))

	var wg sync.WaitGroup
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobChan {
				frame, err := fr.processWeaveFrame(job.path)
				resultChan <- weaveDecodeResult{index: job.i, frame: frame, err: err}
			}
		}()
	}

	for i, path := range framePaths {
		jobChan <- struct {
			i    int
			path string
		}{i: i, path: path}
	}
	close(jobChan)

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var frames []weave.Frame
	var processed int
	var errCount int
	for res := range resultChan {
		if res.err != nil {
			errCount++
			continue
		}
		frames = append(frames, res.frame)
		processed++
		if progress != nil {
			progress <- float64(processed) / float64(len(framePaths))
		}
	}

	if len(frames) == 0 {
		return fmt.Errorf("nenhum quadro Weave valido decodificado (total de erros: %d)", errCount)
	}

	payloadCapacity := fr.FrameCfg.TotalBytesCapacity() - weave.HeaderSize
	if payloadCapacity <= 0 {
		return fmt.Errorf("capacidade negativa para payload")
	}
	cfg := weave.Config{
		PayloadSize:          payloadCapacity,
		DataFramesPerBlock:   weave.DefaultBlockSize,
		RescueFramesPerBlock: weave.DefaultRescueCount,
	}

	data, stats, err := weave.Reconstruct(frames, cfg)
	if err != nil {
		return fmt.Errorf("falha ao reconstruir Weave: %w", err)
	}
	_ = stats
	return os.WriteFile(outputPath, data, 0644)
}

func (fr *FrameReconstructor) processWeaveFrame(path string) (weave.Frame, error) {
	f, err := os.Open(path)
	if err != nil {
		return weave.Frame{}, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return weave.Frame{}, fmt.Errorf("falha ao decodificar png: %w", err)
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
		{true, 0, 0, 0, 0},
	}

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

			allBytes, err := readBytesFromImage(img, baseCfg, tryCfg, adjustThreshold(threshold, a.thrAdjust), levels, a.offX, a.offY, a.isDiff)
			if err != nil {
				lastErr = err
				continue
			}

			bytesInFrame := baseCfg.TotalBytesCapacity()
			if bytesInFrame > len(allBytes) {
				bytesInFrame = len(allBytes)
			}
			frame, err := weave.ParseFrame(allBytes[:bytesInFrame])
			if err != nil {
				lastErr = fmt.Errorf("CRC Weave invalido: %w", err)
				continue
			}
			return frame, nil
		}
	}

	if lastErr != nil {
		return weave.Frame{}, lastErr
	}
	return weave.Frame{}, fmt.Errorf("falha ao decodificar frame Weave")
}
