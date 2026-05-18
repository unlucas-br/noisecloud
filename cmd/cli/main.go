package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"ncc/internal/decoder"
	"ncc/internal/encoder"
)

func init() {
	if runtime.GOOS == "windows" {
		enableWindowsVirtualTerminal()
	}
}

func enableWindowsVirtualTerminal() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode := kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode := kernel32.NewProc("SetConsoleMode")
	procGetStdHandle := kernel32.NewProc("GetStdHandle")

	handle, _, _ := procGetStdHandle.Call(uintptr(uint32(0xFFFFFFF5)))
	if handle == 0 {
		return
	}

	var mode uint32
	ret, _, _ := procGetConsoleMode.Call(handle, uintptr(unsafe.Pointer(&mode)))
	if ret == 0 {
		return
	}

	mode |= 0x0004
	procSetConsoleMode.Call(handle, uintptr(mode))
}

func main() {
	runInteractiveMenu()
}

type statusReporter func(stage, detail string, progress float64, determinate bool)

type loadingScreen struct {
	stage       string
	detail      string
	progress    float64
	determinate bool
	startedAt   time.Time
	stopCh      chan struct{}
	doneCh      chan struct{}
	mu          sync.RWMutex
	frame       int
	lastWidth   int
}

func newLoadingScreen() *loadingScreen {
	return &loadingScreen{
		stage:       "",
		detail:      "",
		determinate: false,
		startedAt:   time.Now(),
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
	}
}

func (ls *loadingScreen) Start() {
	fmt.Print("\033[?25l")
	go func() {
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		defer close(ls.doneCh)

		for {
			select {
			case <-ls.stopCh:
				return
			case <-ticker.C:
				ls.render()
			}
		}
	}()
}

func (ls *loadingScreen) Stop() {
	close(ls.stopCh)
	<-ls.doneCh
	ls.clearLine()
	fmt.Print("\033[?25h")
}

func (ls *loadingScreen) Update(stage, detail string, progress float64, determinate bool) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if stage != "" {
		ls.stage = stage
	}
	if detail != "" {
		ls.detail = detail
	}
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	ls.progress = progress
	ls.determinate = determinate
}

func (ls *loadingScreen) render() {
	ls.mu.Lock()
	stage := ls.stage
	detail := ls.detail
	progress := ls.progress
	determinate := ls.determinate
	frame := ls.frame
	ls.frame++
	ls.mu.Unlock()

	if stage == "" {
		return
	}

	spinnerFrames := []string{"|", "/", "-", "\\"}
	spinner := spinnerFrames[frame%len(spinnerFrames)]
	bar := buildAnimatedBar(frame, progress, determinate, 34)
	label := "buffering"
	if determinate {
		label = fmt.Sprintf("%3d%%", int(progress*100))
	}

	line := fmt.Sprintf("%s %s | %s | [%s] %s | %s",
		spinner,
		stage,
		detail,
		bar,
		label,
		formatElapsed(time.Since(ls.startedAt)),
	)

	padding := ""
	if ls.lastWidth > len(line) {
		padding = strings.Repeat(" ", ls.lastWidth-len(line))
	}
	ls.lastWidth = len(line)

	fmt.Printf("\r%s%s", line, padding)
}

func (ls *loadingScreen) clearLine() {
	ls.mu.Lock()
	width := ls.lastWidth
	ls.mu.Unlock()
	if width > 0 {
		fmt.Printf("\r%s\r", strings.Repeat(" ", width))
	}
}

func buildAnimatedBar(frame int, progress float64, determinate bool, width int) string {
	if width <= 0 {
		width = 24
	}

	bar := make([]byte, width)
	for i := range bar {
		bar[i] = '-'
	}

	if determinate {
		filled := int(progress * float64(width))
		if progress > 0 && filled == 0 {
			filled = 1
		}
		if filled > width {
			filled = width
		}
		for i := 0; i < filled; i++ {
			bar[i] = '='
		}
		return string(bar)
	}

	segment := 8
	if segment > width {
		segment = width
	}
	cycle := width*2 - 2
	if cycle <= 0 {
		cycle = 1
	}
	pos := frame % cycle
	if pos >= width {
		pos = cycle - pos
	}
	for i := 0; i < segment; i++ {
		idx := pos + i
		if idx >= width {
			break
		}
		bar[idx] = '='
	}
	return string(bar)
}

func formatElapsed(elapsed time.Duration) string {
	totalSeconds := int(elapsed.Seconds())
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func runWithLoading(operation func(report statusReporter) error) error {
	loader := newLoadingScreen()
	loader.Start()
	defer loader.Stop()
	return operation(loader.Update)
}

func runEncode(inputPath, outputPath string, threads int, preset, gpu string, targetDuration int, report statusReporter) error {
	if report == nil {
		report = func(string, string, float64, bool) {}
	}

	_, err := os.Stat(inputPath)
	if err != nil {
		return fmt.Errorf("arquivo nao encontrado: %s", inputPath)
	}

	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("falha ao ler arquivo: %w", err)
	}

	data, err = compressData(data)
	if err != nil {
		return fmt.Errorf("erro compressao: %w", err)
	}

	emptyKey := make([]byte, 32)
	payloads := []encoder.Payload{{
		Data:       data,
		FrameKey:   emptyKey,
		VideoSalt:  emptyKey,
		ShuffleKey: emptyKey,
	}}

	if preset == "weave" {
		gpu = "none"
	} else if gpu == "auto" {
		bestGPU := "none"
		for _, candidate := range []string{"nvidia", "amd", "intel"} {
			if err := encoder.VerifyGPU(candidate); err == nil {
				bestGPU = candidate
				break
			}
		}
		gpu = bestGPU
	}

	enc, err := encoder.NewVideoEncoder(threads, preset, gpu, emptyKey)
	if err != nil {
		return fmt.Errorf("falha ao criar encoder: %w", err)
	}
	defer enc.Cleanup()

	if targetDuration > 0 {
		targetFrames := targetDuration * enc.FrameCfg.FPS
		currentFrames := 0
		capacityFrame0 := enc.FrameCfg.CapacityPerFrame(enc.ECCCfg)
		capacityOthers := enc.FrameCfg.CapacityPerFrame(enc.ECCCfg)

		for _, payload := range payloads {
			frames := 1
			remaining := len(payload.Data)
			if remaining > capacityFrame0 {
				remaining -= capacityFrame0
				frames += (remaining + capacityOthers - 1) / capacityOthers
			}
			currentFrames += frames
		}

		if targetFrames > currentFrames {
			paddingFrames := targetFrames - currentFrames
			noiseKey := make([]byte, 32)
			_, _ = rand.Read(noiseKey)

			noiseDataSize := capacityFrame0 + (paddingFrames-1)*capacityOthers
			if noiseDataSize < 0 {
				noiseDataSize = 0
			}

			noiseData := make([]byte, noiseDataSize)
			_, _ = rand.Read(noiseData)

			payloads = append(payloads, encoder.Payload{
				Data:     noiseData,
				FrameKey: noiseKey,
			})
		}
	}

	progressCh := make(chan float64, 100)
	done := make(chan error, 1)

	go func() {
		err := enc.EncodePayloads(payloads, outputPath, progressCh)
		close(progressCh)
		done <- err
	}()

	for progress := range progressCh {
		report("Gerando video", filepath.Base(outputPath), progress, true)
	}

	if err := <-done; err != nil {
		return fmt.Errorf("falha no encode: %w", err)
	}
	return nil
}

func runDecode(inputPath, outputPath, preset string, report statusReporter) error {
	if report == nil {
		report = func(string, string, float64, bool) {}
	}

	if preset == "weave" {
		data, ok, err := decoder.ReadWeaveTrailer(inputPath)
		if err != nil {
			return fmt.Errorf("ler payload compacto: %w", err)
		}
		if ok {
			if err := writeDecompressedPayload(data, outputPath); err != nil {
				return err
			}
			return nil
		}
	}

	extractor, err := decoder.NewFrameExtractor(preset)
	if err != nil {
		return fmt.Errorf("falha ao criar extrator: %w", err)
	}
	defer extractor.Cleanup()

	report("Extraindo frames", filepath.Base(inputPath), 0, false)
	frames, err := extractor.ExtractFrames(inputPath, nil)
	if err != nil {
		return fmt.Errorf("extrair frames: %w", err)
	}

	outputDir := filepath.Dir(outputPath)
	if outputDir == "" {
		outputDir = "."
	}
	rawFile, err := os.CreateTemp(outputDir, "."+filepath.Base(outputPath)+".raw-*")
	if err != nil {
		return fmt.Errorf("criar arquivo temporario de reconstrucao: %w", err)
	}
	rawOutputPath := rawFile.Name()
	rawFile.Close()
	defer os.Remove(rawOutputPath)

	recon := decoder.NewFrameReconstructor(preset)
	progressCh := make(chan float64, 100)
	done := make(chan error, 1)

	go func() {
		err := recon.ReconstructFile(frames, rawOutputPath, progressCh)
		close(progressCh)
		done <- err
	}()

	for progress := range progressCh {
		report("Reconstruindo payload", fmt.Sprintf("%d quadros", len(frames)), progress, true)
	}

	if err := <-done; err != nil {
		if preset == "weave" {
			return fmt.Errorf("falha ao reconstruir: %w\nEste video nao contem payload compacto nem frames Weave validos. Se ele veio do YouTube a partir de um MP4 compacto/preto, o YouTube removeu o payload; gere e envie novamente com o EXE atualizado", err)
		}
		return fmt.Errorf("falha ao reconstruir: %w", err)
	}

	data, err := os.ReadFile(rawOutputPath)
	if err != nil {
		return fmt.Errorf("falha ao ler saida para descompressao: %w", err)
	}

	if err := writeDecompressedPayload(data, outputPath); err != nil {
		return err
	}
	return nil
}

func writeDecompressedPayload(data []byte, outputPath string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("falha ao iniciar descompressao: %w", err)
	}
	defer gz.Close()

	decompressed, err := io.ReadAll(gz)
	if err != nil {
		return fmt.Errorf("falha na leitura da descompressao: %w", err)
	}

	if err := os.WriteFile(outputPath, decompressed, 0644); err != nil {
		return fmt.Errorf("salvar arquivo final: %w", err)
	}

	return nil
}

func compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func runInteractiveMenu() {
	reader := bufio.NewReader(os.Stdin)
	renderMenuHeader()

	for {
		menuMargin := "  "
		fmt.Printf("%s\033[1;32m>\033[0m Selecione uma opcao: ", menuMargin)

		option, _ := reader.ReadString('\n')
		option = strings.TrimSpace(option)

		if option == "5" {
			fmt.Println()
			return
		}

		switch option {
		case "1":
			input := promptRequired(reader, "Arquivo de entrada: ")
			if !fileExists(input) {
				fmt.Printf("\nErro: o arquivo '%s' nao foi encontrado.\n", input)
				waitEnter(reader)
				continue
			}

			output := prompt(reader, "Arquivo de saida (ENTER para padrao): ")
			if output == "" {
				output = strings.TrimSuffix(input, filepath.Ext(input)) + "_ncc.mp4"
			}

			printOperationStart("encode")
			err := runWithLoading(func(report statusReporter) error {
				return runEncode(input, output, 0, "weave", "auto", 0, report)
			})
			if err != nil {
				fmt.Printf("Erro: %v\n", err)
			} else {
				printOperationDone("salvo", output)
			}
			waitEnter(reader)

		case "2":
			fmt.Println()
			fmt.Printf("  \033[1;36m[ TikTok Weave Mode ]\033[0m Resolucao: 1080x1920 | FPS: 30 | Max: 10 min\n")
			fmt.Printf("  Capacidade estimada: ate \033[1;33m~1.4 MB\033[0m por video de 10 minutos\n")
			fmt.Println()

			input := promptRequired(reader, "Arquivo de entrada: ")
			if !fileExists(input) {
				fmt.Printf("\nErro: o arquivo '%s' nao foi encontrado.\n", input)
				waitEnter(reader)
				continue
			}

			if info, err := os.Stat(input); err == nil {
				sizeMB := float64(info.Size()) / 1024 / 1024
				const tiktokMaxMB = 1.4
				if sizeMB > tiktokMaxMB {
					fmt.Printf("\n  Aviso: arquivo (%.1f MB) excede a capacidade de um video de 10 min (~%.0f MB).\n", sizeMB, tiktokMaxMB)
					fmt.Printf("  O video gerado sera maior que 10 min e podera ser cortado pelo TikTok.\n\n")
				} else {
					minutesNeeded := (sizeMB / tiktokMaxMB) * 10
					fmt.Printf("  Arquivo cabe em aproximadamente %.1f min de video TikTok.\n\n", minutesNeeded)
				}
			}

			output := prompt(reader, "Arquivo de saida (ENTER para padrao): ")
			if output == "" {
				output = strings.TrimSuffix(input, filepath.Ext(input)) + "_tiktok.mp4"
			}

			printOperationStart("encode")
			err := runWithLoading(func(report statusReporter) error {
				return runEncode(input, output, 0, "tiktok", "auto", 0, report)
			})
			if err != nil {
				fmt.Printf("Erro: %v\n", err)
			} else {
				printOperationDone("salvo", output)
			}
			waitEnter(reader)

		case "3":
			input := promptRequired(reader, "Arquivo NCC (.mp4): ")
			if !fileExists(input) {
				fmt.Printf("\nErro: o arquivo '%s' nao foi encontrado.\n", input)
				waitEnter(reader)
				continue
			}

			output := prompt(reader, "Arquivo de saida: ")
			if output == "" {
				output = strings.TrimSuffix(input, filepath.Ext(input)) + "_recovered.bin"
			}

			printOperationStart("decode")
			err := runWithLoading(func(report statusReporter) error {
				return runDecode(input, output, "weave", report)
			})
			if err != nil {
				fmt.Printf("Erro: %v\n", err)
			} else {
				printOperationDone("recuperado", output)
			}
			waitEnter(reader)

		case "4":
			fmt.Println()
			fmt.Printf("  [ TikTok Weave Decode ] Usando WEV1 - 16 dados + 2 resgates por bloco\n")
			fmt.Println()

			input := promptRequired(reader, "Arquivo TikTok NCC (.mp4): ")
			if !fileExists(input) {
				fmt.Printf("\nErro: o arquivo '%s' nao foi encontrado.\n", input)
				waitEnter(reader)
				continue
			}

			output := prompt(reader, "Arquivo de saida: ")
			if output == "" {
				output = strings.TrimSuffix(input, filepath.Ext(input)) + "_recovered.bin"
			}

			printOperationStart("decode")
			err := runWithLoading(func(report statusReporter) error {
				return runDecode(input, output, "tiktok", report)
			})
			if err != nil {
				fmt.Printf("Erro: %v\n", err)
			} else {
				printOperationDone("recuperado", output)
			}
			waitEnter(reader)

		default:
			fmt.Printf("\nErro: opcao '%s' invalida. Tente novamente.\n", option)
			time.Sleep(1 * time.Second)
		}
	}
}

func renderMenuHeader() {
	fmt.Print("\033[H\033[2J")
	fmt.Print("\033[?25h")

	margin := "                    "
	logo := []string{
		margin + `    _   __      _              ________                _`,
		margin + `   / | / /___  (_)____ ___    / ____/ /___  __  ______// `,
		margin + `  /  |/ / __ \/ / ___/ _ \  / /   / / __ \/ / / / __  /   `,
		margin + ` / /|  / /_/ / (__  )  __/ / /___/ / /_/ / /_/ / /_/ /    `,
		margin + `/_/ |_/\____/_/____/\___/  \____/_/\____/\__,_/\__,_/     `,
	}
	for _, line := range logo {
		fmt.Printf("\033[38;5;51m%s\033[0m\n", line)
	}
	fmt.Println()

	titleMargin := margin + "        "
	liteTitle := "--- Lite Version by Lucas Ferraz ---"
	weaveTitle := "--- Weave 1.0 ---"
	weaveMargin := titleMargin + strings.Repeat(" ", (len(liteTitle)-len(weaveTitle))/2)
	fmt.Printf("%s\033[38;5;43m%s\033[0m\n", titleMargin, liteTitle)
	fmt.Printf("%s\033[31m%s\033[0m\n", weaveMargin, weaveTitle)
	fmt.Println()

	menuMargin := "  "
	fmt.Printf("%s\033[1;36m[1]\033[0m ENCODE              (Esconder arquivo - Padrao Weave)\n", menuMargin)
	fmt.Printf("%s\033[1;36m[2]\033[0m ENCODE TIKTOK       (Experimental - 9:16 - 30fps)\n", menuMargin)
	fmt.Printf("%s\033[1;36m[3]\033[0m DECODE              (Recuperar arquivo - Padrao Weave)\n", menuMargin)
	fmt.Printf("%s\033[1;36m[4]\033[0m DECODE TIKTOK       (Recuperar arquivo TikTok)\n", menuMargin)
	fmt.Printf("%s\033[1;36m[5]\033[0m \033[31mSAIR\033[0m\n", menuMargin)
	fmt.Println()
}

func printOperationStart(kind string) {
	fmt.Println()
	fmt.Printf("Preparando %s...\n", kind)
	fmt.Println("Processando...")
}

func printOperationDone(resultKind, output string) {
	fmt.Println("\nProcesso concluido.")
	if resultKind == "salvo" {
		fmt.Printf("Arquivo salvo: %s\n", output)
		return
	}
	fmt.Printf("Arquivo recuperado: %s\n", output)
}

func prompt(reader *bufio.Reader, text string) string {
	fmt.Print(text)
	res, _ := reader.ReadString('\n')
	return strings.TrimSpace(res)
}

func promptRequired(reader *bufio.Reader, text string) string {
	for {
		res := prompt(reader, text)
		if res != "" {
			return res
		}
		fmt.Println("(!) Este campo e obrigatorio.")
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func waitEnter(reader *bufio.Reader) {
	fmt.Println("\n> Pressione ENTER para continuar...")
	reader.ReadString('\n')
	renderMenuHeader()
}
