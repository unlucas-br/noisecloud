package encoder

import (
	"bytes"
	"fmt"
	"image"

	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"encoding/binary"
	mrand "math/rand"
)

type VideoEncoder struct {
	FrameCfg FrameConfig
	ECCCfg   ECCConfig
	TempDir  string
	Threads  int
	GPU      string 
	Preset   string 
	FrameKey []byte 
}

// NewVideoEncoder inicializa o encoder com as configurações de hardware e frames
func NewVideoEncoder(threads int, preset string, gpu string, frameKey []byte) (*VideoEncoder, error) {
	tempDir, err := os.MkdirTemp("", "ncc-*")
	if err != nil {
		return nil, err
	}

	if threads <= 0 {
		threads = runtime.NumCPU() - 2
		if threads < 1 {
			threads = 1
		}
		fmt.Printf("Threads: %d (reservando 2 cores)\n", threads)
	}

	frameCfg := DefaultFrameConfig()
	if preset == "hq" {
		frameCfg = HQFrameConfig()
	} else if preset == "dense" {
		frameCfg = HighDensityFrameConfig()
	} else if preset == "square" {
		frameCfg = SquareFrameConfig()
	} else if preset == "fast" {
		frameCfg = DefaultFrameConfig()
	}

	return &VideoEncoder{
		FrameCfg: frameCfg,
		ECCCfg:   NewECCConfig(),
		TempDir:  tempDir,
		Threads:  threads,
		GPU:      gpu,
		Preset:   preset,
		FrameKey: frameKey,
	}, nil
}

func (ve *VideoEncoder) Cleanup() {
	os.RemoveAll(ve.TempDir)
}

type Payload struct {
	Data       []byte
	FrameKey   []byte
	VideoSalt  []byte
	ShuffleKey []byte
}

// EncodePayloads converte os dados em uma sequência de frames e gera o vídeo final
func (ve *VideoEncoder) EncodePayloads(payloads []Payload, outputPath string, progressCh chan<- float64) error {
	encoderType := "CPU (libx264)"
	if ve.GPU != "none" {
		if ve.GPU == "auto" {
			encoderType = "AUTO (Procurando...)"
		} else {
			encoderType = fmt.Sprintf("GPU (%s)", ve.GPU)
		}
	}

	capacityFrame0 := ve.FrameCfg.CapacityPerFrame(ve.ECCCfg)
	capacityOthers := ve.FrameCfg.CapacityPerFrame(ve.ECCCfg)

	var totalFrames int
	var payloadFrames []int

	for i, p := range payloads {
		frames := 1
		remaining := len(p.Data)
		if remaining > capacityFrame0 {
			remaining -= capacityFrame0
		} else {
			remaining = 0
		}

		if remaining > 0 {
			frames += (remaining + capacityOthers - 1) / capacityOthers
		}
		if len(p.Data) == 0 {
			frames = 1
		}
		totalFrames += frames
		payloadFrames = append(payloadFrames, frames)

		fmt.Printf("\nDEBUG: Payload %d: Dados=%d bytes, capacidade0=%d, capacidadeOutros=%d, frames=%d\n", i, len(p.Data), capacityFrame0, capacityOthers, frames)

		fmt.Printf("Payload %d: %.2f MB | %d quadros | Threads: %d | Encoder: %s\n",
			i, float64(len(p.Data))/1024/1024, frames, ve.Threads, encoderType)
	}

	ffmpegCmd, ffmpegStdin, err := ve.StartFFmpegPipe(outputPath, totalFrames)
	if err != nil {
		return fmt.Errorf("falha ao iniciar ffmpeg: %w", err)
	}
	defer ffmpegStdin.Close()

	type Job struct {
		Index       int
		GlobalIndex int
		Data        []byte
		FrameKey    []byte
		VideoSalt   []byte
		ShuffleKey  []byte
		TotalFrames int
	}
	type Result struct {
		Index  int
		Pixels []MacroPixel
		Err    error
	}

	bufferSize := ve.Threads * 4
	if bufferSize > totalFrames {
		bufferSize = totalFrames
	}

	jobs := make(chan Job, bufferSize)
	results := make(chan Result, bufferSize)

	cols, rows := ve.FrameCfg.GridSize()
	totalMacros := cols * rows
	pixelPool := sync.Pool{
		New: func() interface{} {
			return make([]MacroPixel, totalMacros)
		},
	}

	var wg sync.WaitGroup
	for w := 0; w < ve.Threads; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			workerECC, err := NewECCEncoder(ve.ECCCfg)
			if err != nil {
				results <- Result{Index: -1, Err: fmt.Errorf("falha na inicialização do ecc: %w", err)}
				return
			}

			for job := range jobs {
				pixelBuf := pixelPool.Get().([]MacroPixel)

				frame, err := NewFrame(
					ve.FrameCfg,
					workerECC,
					job.Index,
					job.Data,
					job.TotalFrames,
					job.FrameKey,
					job.VideoSalt,
					job.ShuffleKey,
				)
				if err != nil {
					pixelPool.Put(pixelBuf)
					results <- Result{Index: job.Index, Err: err}
					return
				}

				pixels, err := frame.Render(pixelBuf)
				if err != nil {
					pixelPool.Put(pixelBuf)
					results <- Result{Index: job.Index, Err: err}
					return
				}
				results <- Result{Index: job.GlobalIndex, Pixels: pixels, Err: nil}
			}
		}()
	}

	go func() {
		type jobData struct {
			LogicalIndex int
			Data         []byte
			FrameKey     []byte
			VideoSalt    []byte
			ShuffleKey   []byte
			TotalFrames  int
		}
		var allJobs []jobData

		for pi, p := range payloads {
			frames := payloadFrames[pi]
			data := p.Data
			for i := 0; i < frames; i++ {
				var frameData []byte
				if i == 0 {
					end := capacityFrame0
					if end > len(data) {
						end = len(data)
					}
					frameData = data[:end]
				} else {
					start := capacityFrame0 + (i-1)*capacityOthers
					end := start + capacityOthers
					if start >= len(data) {
						frameData = []byte{}
					} else {
						if end > len(data) {
							end = len(data)
						}
						frameData = data[start:end]
					}
				}
				allJobs = append(allJobs, jobData{
					LogicalIndex: i,
					Data:         frameData,
					FrameKey:     p.FrameKey,
					VideoSalt:    p.VideoSalt,
					ShuffleKey:   p.ShuffleKey,
					TotalFrames:  frames,
				})
			}
		}

		seed := int64(time.Now().UnixNano())
		if len(payloads) > 0 && len(payloads[0].FrameKey) >= 8 {
			seed = int64(binary.BigEndian.Uint64(payloads[0].FrameKey[:8]))
		}
		rng := mrand.New(mrand.NewSource(seed))
		rng.Shuffle(len(allJobs), func(i, j int) {
			allJobs[i], allJobs[j] = allJobs[j], allJobs[i]
		})

		for physicalIdx, jd := range allJobs {
			jobs <- Job{
				Index:       jd.LogicalIndex,
				GlobalIndex: physicalIdx,
				Data:        jd.Data,
				FrameKey:    jd.FrameKey,
				VideoSalt:   jd.VideoSalt,
				ShuffleKey:  jd.ShuffleKey,
				TotalFrames: jd.TotalFrames,
			}
		}

		close(jobs)
		wg.Wait()
		close(results)
	}()

	pending := make(map[int][]MacroPixel)
	nextFrameIndex := 0 

	calibrationImg := image.NewRGBA(image.Rect(0, 0, ve.FrameCfg.Width, ve.FrameCfg.Height))
	ve.renderCalibrationBar(calibrationImg)
	calibrationBarPix := calibrationImg.Pix[:ve.FrameCfg.CalibrationHeight*calibrationImg.Stride] 

	for res := range results {
		if res.Err != nil {
			return fmt.Errorf("erro no worker quadro %d: %w", res.Index, res.Err)
		}

		pending[res.Index] = res.Pixels

		for {
			pixels, ok := pending[nextFrameIndex]
			if !ok {
				break
			}

			img := image.NewRGBA(image.Rect(0, 0, ve.FrameCfg.Width, ve.FrameCfg.Height))
			copy(img.Pix[:ve.FrameCfg.CalibrationHeight*img.Stride], calibrationBarPix)
			ve.drawFrameToBuffer(img, pixels)

			if _, err := ffmpegStdin.Write(img.Pix); err != nil {
				return fmt.Errorf("falha ao escrever o quadro %d para o ffmpeg: %w", nextFrameIndex, err)
			}

			delete(pending, nextFrameIndex)
			pixelPool.Put(pixels)

			if progressCh != nil {
				progressCh <- float64(nextFrameIndex+1) / float64(totalFrames)
			}
			nextFrameIndex++
		}
	}

	ffmpegStdin.Close()

	if err := ffmpegCmd.Wait(); err != nil {
		return fmt.Errorf("falha na finalização do ffmpeg: %w", err)
	}

	return nil
}

func (ve *VideoEncoder) renderCalibrationBar(img *image.RGBA) {
	width := img.Bounds().Dx()
	sectionWidth := width / 4

	for y := 0; y < ve.FrameCfg.CalibrationHeight; y++ {
		for x := 0; x < width; x++ {
			var val uint8 = 0
			if x >= sectionWidth && x < sectionWidth*2 {
				val = 255
			}
			if x >= sectionWidth*3 {
				val = 255
			}

			offset := img.PixOffset(x, y)
			img.Pix[offset] = val   
			img.Pix[offset+1] = val 
			img.Pix[offset+2] = val 
			img.Pix[offset+3] = 255 
		}
	}
}

func (ve *VideoEncoder) drawFrameToBuffer(img *image.RGBA, pixels []MacroPixel) {
	stride := img.Stride

	for _, mp := range pixels {
		offsetY := mp.Y + ve.FrameCfg.CalibrationHeight
		rowWidth := mp.Size * 4
		rowBuffer := make([]byte, rowWidth)
		gray := mp.ByteToGray()

		for k := 0; k < mp.Size; k++ {
			rowBuffer[k*4] = gray   
			rowBuffer[k*4+1] = gray 
			rowBuffer[k*4+2] = gray 
			rowBuffer[k*4+3] = 255  
		}

		for y := 0; y < mp.Size; y++ {
			rowStart := (offsetY + y) * stride
			pixelOffset := rowStart + mp.X*4
			copy(img.Pix[pixelOffset:pixelOffset+rowWidth], rowBuffer)
		}
	}
}

// StartFFmpegPipe configura o pipeline do FFmpeg para compressão de vídeo (GPU ou CPU)
func (ve *VideoEncoder) StartFFmpegPipe(outputPath string, totalFrames int) (*exec.Cmd, io.WriteCloser, error) {
	ffmpegPath := findFFmpeg()

	videoCodec := "libx264" 
	gpuFlags := []string{}

	if ve.GPU == "nvidia" || ve.GPU == "nvenc" {
		videoCodec = "h264_nvenc"
		if ve.Preset == "fast" {
			gpuFlags = []string{"-preset", "p1"}
		} else {
			gpuFlags = []string{"-preset", "p7", "-tune", "hq"}
		}
	} else if ve.GPU == "amd" || ve.GPU == "amf" {
		videoCodec = "h264_amf"
		if ve.Preset == "fast" {
			gpuFlags = []string{"-quality", "speed"}
		} else {
			gpuFlags = []string{"-quality", "quality"}
		}
	} else if ve.GPU == "intel" || ve.GPU == "qsv" {
		videoCodec = "h264_qsv"
		if ve.Preset == "fast" {
			gpuFlags = []string{"-preset", "veryfast"}
		} else {
			gpuFlags = []string{"-global_quality", "20"}
		}
	} else if ve.GPU == "auto" {
		gpus := []string{"nvidia", "amd", "intel"}
		for _, g := range gpus {
			if err := VerifyGPU(g); err == nil {
				fmt.Printf("GPU Detectada: %s\n", g)
				if g == "nvidia" {
					videoCodec = "h264_nvenc"
					if ve.Preset == "fast" {
						gpuFlags = []string{"-preset", "p1"} 
					} else {
						gpuFlags = []string{"-preset", "p7", "-tune", "hq"}
					}
				} else if g == "amd" {
					videoCodec = "h264_amf"
					if ve.Preset == "fast" {
						gpuFlags = []string{"-quality", "speed"}
					} else {
						gpuFlags = []string{"-quality", "quality"}
					}
				} else if g == "intel" {
					videoCodec = "h264_qsv"
					if ve.Preset == "fast" {
						gpuFlags = []string{"-preset", "veryfast"}
					} else {
						gpuFlags = []string{"-global_quality", "20"}
					}
				}
				break
			}
		}
		if videoCodec == "libx264" {
			fmt.Println("Nenhum encoder de GPU encontrado. Usando CPU.")
		}
	}

	args := []string{
		"-y",
		"-f", "rawvideo",
		"-pixel_format", "rgba",
		"-video_size", fmt.Sprintf("%dx%d", ve.FrameCfg.Width, ve.FrameCfg.Height),
		"-framerate", fmt.Sprintf("%d", ve.FrameCfg.FPS),
		"-i", "pipe:0",
		"-c:v", videoCodec,
	}

	if videoCodec == "libx264" {
		if ve.Preset == "fast" {
			args = append(args, "-preset", "ultrafast", "-crf", "18")
		} else {
			args = append(args, "-preset", "slow", "-crf", "18")
		}
	} else {
		args = append(args, gpuFlags...)
		if videoCodec == "h264_nvenc" {
			args = append(args, "-cq", "20")
		} else {
			args = append(args, "-b:v", "8M") 
		}
	}

	args = append(args,
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		outputPath,
	)

	cmd := exec.Command(ffmpegPath, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("pipe de entrada padrão falhou: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, nil, fmt.Errorf("falha ao iniciar o ffmpeg: %w", err)
	}

	return cmd, stdin, nil
}

func findFFmpeg() string {
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path
	}

	locations := []string{
		`C:\ffmpeg\bin\ffmpeg.exe`,
		`C:\Program Files\ffmpeg\bin\ffmpeg.exe`,
		`C:\Program Files (x86)\ffmpeg\bin\ffmpeg.exe`,
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links", "ffmpeg.exe"),
		filepath.Join(os.Getenv("USERPROFILE"), "scoop", "shims", "ffmpeg.exe"),
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	return "ffmpeg"
}

func VerifyGPU(gpuType string) error {
	ffmpegPath := findFFmpeg()

	codec := ""
	if gpuType == "nvidia" {
		codec = "h264_nvenc"
	} else if gpuType == "amd" {
		codec = "h264_amf"
	} else if gpuType == "intel" {
		codec = "h264_qsv"
	} else {
		return fmt.Errorf("tipo de gpu desconhecido: %s", gpuType)
	}

	cmd := exec.Command(ffmpegPath,
		"-y",
		"-hide_banner",
		"-f", "lavfi",
		"-i", "color=c=black:s=256x256",
		"-vframes", "1",
		"-c:v", codec,
		"-f", "null",
		"-",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Teste de GPU falhou para '%s' (%s):\n%s", gpuType, codec, string(output))
	}
	return nil
}

func BenchmarkSpeed(gpuType string, width, height, fps int) (float64, error) {
	ffmpegPath := findFFmpeg()
	codec := "libx264"
	args := []string{}

	if gpuType != "none" {
		if gpuType == "nvidia" {
			codec = "h264_nvenc"
			args = append(args, "-preset", "p7", "-tune", "hq")
		} else if gpuType == "amd" {
			codec = "h264_amf"
			args = append(args, "-quality", "speed")
		} else if gpuType == "intel" {
			codec = "h264_qsv"
			args = append(args, "-global_quality", "20")
		} else {
			return 0, fmt.Errorf("tipo de gpu desconhecido: %s", gpuType)
		}
	} else {
		args = append(args, "-preset", "ultrafast")
		args = append(args, "-preset", "slow", "-crf", "23")
	}

	cmd := exec.Command(ffmpegPath,
		"-y",
		"-hide_banner",
		"-f", "lavfi",
		"-i", fmt.Sprintf("testsrc=size=%dx%d:rate=%d", width, height, fps),
		"-t", "5", 
		"-c:v", codec,
	)
	cmd.Args = append(cmd.Args, args...)
	cmd.Args = append(cmd.Args, "-f", "null", "-")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	start := time.Now()
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("benchmark falhou para %s: %w", gpuType, err)
	}
	duration := time.Since(start)

	totalFrames := 5 * fps
	calculatedFPS := float64(totalFrames) / duration.Seconds()

	return calculatedFPS, nil
}

