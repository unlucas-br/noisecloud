package decoder

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type FrameExtractor struct {
	TempDir string
	Preset  string
}

func NewFrameExtractor(preset string) (*FrameExtractor, error) {
	tempDir, err := os.MkdirTemp("", "ncc-dec-*")
	if err != nil {
		return nil, err
	}
	return &FrameExtractor{TempDir: tempDir, Preset: preset}, nil
}

func (fe *FrameExtractor) Cleanup() {
	os.RemoveAll(fe.TempDir)
}

// ExtractFrames utiliza FFmpeg para despejar cada quadro do vídeo em arquivos PNG temporários
func (fe *FrameExtractor) ExtractFrames(videoPath string, progress chan<- float64) ([]string, error) {
	outputPattern := filepath.Join(fe.TempDir, "frame_%05d.png")

	ffmpegPath := findFFmpeg()

	args := fe.extractArgs(videoPath, outputPattern, "-fps_mode", "passthrough")
	if err := runExtractCommand(ffmpegPath, args); err != nil {
		cleanupExtractedPNGs(fe.TempDir)
		legacyArgs := fe.extractArgs(videoPath, outputPattern, "-vsync", "0")
		if legacyErr := runExtractCommand(ffmpegPath, legacyArgs); legacyErr != nil {
			return nil, fmt.Errorf("falha na extração ffmpeg: %w", legacyErr)
		}
	}

	entries, err := os.ReadDir(fe.TempDir)
	if err != nil {
		return nil, err
	}

	var frames []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".png") {
			frames = append(frames, filepath.Join(fe.TempDir, entry.Name()))
		}
	}

	sort.Slice(frames, func(i, j int) bool {
		return frames[i] < frames[j]
	})

	if progress != nil {
		progress <- 0.3
	}

	return frames, nil
}

func (fe *FrameExtractor) extractArgs(videoPath, outputPattern, syncFlag, syncValue string) []string {
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-nostats",
		"-hwaccel", "auto",
		"-i", videoPath,
		syncFlag, syncValue,
		"-q:v", "1",
		"-pix_fmt", "rgb24",
	}

	if fe.Preset == "fast" {
		args = append(args, "-compression_level", "0", "-pred", "0")
	}

	return append(args, outputPattern)
}

func runExtractCommand(ffmpegPath string, args []string) error {
	cmd := exec.Command(ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func cleanupExtractedPNGs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".png") {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
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
