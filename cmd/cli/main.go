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
	"syscall"
	"time"
	"unsafe"

	"ncc/internal/decoder"
	"ncc/internal/encoder"
)

func init() {
	// Suporte a cores no terminal do Windows
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

// runEncode gerencia a compressão e codificação do arquivo em vídeo
func runEncode(inputPath, outputPath string, threads int, preset, gpu string, targetDuration int) error {
	info, err := os.Stat(inputPath)
	if err != nil {
		return fmt.Errorf("arquivo não encontrado: %s", inputPath)
	}

	fmt.Printf("Lendo arquivo (%.2f MB)...\n", float64(info.Size())/1024/1024)

	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("falha ao ler arquivo: %w", err)
	}

	fmt.Println("Comprimindo dados (Gzip)...")
	data, err = compressData(data)
	if err != nil {
		return fmt.Errorf("erro compressão: %w", err)
	}
	fmt.Printf("Tamanho comprimido: %d bytes\n", len(data))

	emptyKey := make([]byte, 32)

	var payloads []encoder.Payload
	payloads = append(payloads, encoder.Payload{
		Data:       data,
		FrameKey:   emptyKey,
		VideoSalt:  emptyKey,
		ShuffleKey: emptyKey,
	})

	fmt.Println("Testando hardware...")
	if gpu == "auto" {
		bestGPU := "none"
		candidates := []string{"nvidia", "amd", "intel"}
		for _, g := range candidates {
			if err := encoder.VerifyGPU(g); err == nil {
				bestGPU = g
				break
			}
		}
		gpu = bestGPU
		fmt.Printf("Aceleração definida: %s\n", gpu)
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

		for _, p := range payloads {
			f := 1
			rem := len(p.Data)
			if rem > capacityFrame0 {
				rem -= capacityFrame0
				f += (rem + capacityOthers - 1) / capacityOthers
			}
			currentFrames += f
		}

		if targetFrames > currentFrames {
			paddingFrames := targetFrames - currentFrames
			noiseKey := make([]byte, 32)
			rand.Read(noiseKey)

			noiseDataSize := capacityFrame0 + (paddingFrames-1)*capacityOthers
			if noiseDataSize < 0 {
				noiseDataSize = 0
			}

			noiseData := make([]byte, noiseDataSize)
			rand.Read(noiseData)

			payloads = append(payloads, encoder.Payload{
				Data:     noiseData,
				FrameKey: noiseKey,
			})
		}
	}

	progressCh := make(chan float64, 100)
	done := make(chan error, 1)

	go func() {
		done <- enc.EncodePayloads(payloads, outputPath, progressCh)
		close(progressCh)
	}()

	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h")

	for p := range progressCh {
		percent := int(p * 100)
		fmt.Printf("\rProgresso: %3d%% ", percent)
	}
	fmt.Println()

	err = <-done
	if err != nil {
		return fmt.Errorf("falha no encode: %w", err)
	}

	fmt.Printf("Vídeo salvo: %s\n", outputPath)
	return nil
}

// runDecode extrai os frames do vídeo e reconstrói o arquivo original
func runDecode(inputPath, outputPath, preset string) error {
	fmt.Printf("Lendo e extraindo: %q\n", inputPath)

	extractor, err := decoder.NewFrameExtractor(preset)
	if err != nil {
		return fmt.Errorf("falha ao criar extrator: %w", err)
	}
	defer extractor.Cleanup()

	frames, err := extractor.ExtractFrames(inputPath, nil)
	if err != nil {
		return fmt.Errorf("extrair frames: %w", err)
	}

	fmt.Printf("Extraídos %d frames. Reconstruindo arquivo...\n", len(frames))

	recon := decoder.NewFrameReconstructor(preset)
	err = recon.ReconstructFile(frames, outputPath, nil)
	if err != nil {
		return fmt.Errorf("falha ao reconstruir: %w", err)
	}

	fmt.Println("Descomprimindo payload bruto...")
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return fmt.Errorf("falha ao ler saída para descompressão: %w", err)
	}

	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("falha ao iniciar descompressão: %w", err)
	}
	defer gz.Close()

	decompressed, err := io.ReadAll(gz)
	if err != nil {
		return fmt.Errorf("falha na leitura da descompressão: %w", err)
	}

	err = os.WriteFile(outputPath, decompressed, 0644)
	if err != nil {
		return fmt.Errorf("salvar arquivo final: %w", err)
	}

	fmt.Printf("✅ Arquivo recuperado: %s\n", outputPath)
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

// Menuzinho ascii, vibe 2010~ tools hacking ksksksksksk
func runInteractiveMenu() {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("\033[?25l")
		margin := "                    "
		logo := []string{
			margin + `    _   __      _              ________                _`,
			margin + `   / | / /___  (_)____ ___    / ____/ /___  __  ______// `,
			margin + `  /  |/ / __ \/ / ___/ _ \  / /   / / __ \/ / / / __  /   `,
			margin + ` / /|  / /_/ / (__  )  __/ / /___/ / /_/ / /_/ / /_/ /    `,
			margin + `/_/ |_/\____/_/____/\___/  \____/_/\____/\__,_/\__,_/     `,
		}

		fmt.Print("\033[H\033[2J")
		for i := 0; i < len(logo); i++ {
			fmt.Printf("\033[38;5;51m%s\033[0m\n", logo[i])
		}

		fmt.Println()
		titleMargin := margin + "        "
		fmt.Printf("%s\033[38;5;43m--- Lite Version by Lucas Ferraz ---\033[0m\n", titleMargin)
		fmt.Print("\033[?25h")
		fmt.Println()

		menuMargin := "  "
		fmt.Printf("%s\033[1;36m[1]\033[0m ENCODE (Esconder Arquivo)\n", menuMargin)
		fmt.Printf("%s\033[1;36m[2]\033[0m DECODE (Recuperar Arquivo)\n", menuMargin)
		fmt.Printf("%s\033[1;36m[3]\033[0m \033[31mSAIR\033[0m\n", menuMargin)
		fmt.Println()
		fmt.Printf("%s\033[1;32m>\033[0m Selecione uma opção: ", menuMargin)

		option, _ := reader.ReadString('\n')
		option = strings.TrimSpace(option)

		if option == "3" {
			fmt.Print("\033[H\033[2J")
			return
		}

		switch option {
		case "1":
			input := promptRequired(reader, "Arquivo de Entrada: ")
			if !fileExists(input) {
				fmt.Printf("\n❌ Erro: O arquivo '%s' não foi encontrado.\n", input)
				waitEnter(reader)
				continue
			}

			output := prompt(reader, "Arquivo de Saída (ENTER para padrão): ")
			if output == "" {
				output = strings.TrimSuffix(input, filepath.Ext(input)) + "_ncc.mp4"
			}

			err := runEncode(input, output, 0, "hq", "auto", 0)
			if err != nil {
				fmt.Printf("❌ Erro: %v\n", err)
			} else {
				fmt.Println("\n✅ Encode finalizado com sucesso!")
			}
			waitEnter(reader)

		case "2":
			input := promptRequired(reader, "Arquivo NCC (.mp4): ")
			if !fileExists(input) {
				fmt.Printf("\n❌ Erro: O arquivo '%s' não foi encontrado.\n", input)
				waitEnter(reader)
				continue
			}

			output := prompt(reader, "Arquivo de Saída: ")
			if output == "" {
				output = strings.TrimSuffix(input, filepath.Ext(input)) + "_recovered.bin"
			}

			err := runDecode(input, output, "hq")
			if err != nil {
				fmt.Printf("❌ Erro: %v\n", err)
			} else {
				fmt.Println("\n✅ Decode finalizado com sucesso!")
			}
			waitEnter(reader)

		default:
			fmt.Printf("\n❌ Opção '%s' é inválida. Tente novamente.\n", option)
			time.Sleep(1 * time.Second)
		}
	}
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
		fmt.Println("(!) Este campo é obrigatório.")
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func waitEnter(reader *bufio.Reader) {
	fmt.Println("\n> Pressione ENTER para continuar...")
	reader.ReadString('\n')
}
