package decoder

import (
	"fmt"
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

	args := []string{
		"-hwaccel", "auto",
		"-i", videoPath,
		"-vsync", "0",
		"-q:v", "1",
		"-pix_fmt", "rgb24",
	}

	if fe.Preset == "fast" {
		args = append(args, "-compression_level", "0", "-pred", "0")
	}

	args = append(args, outputPattern)

	cmd := exec.Command(ffmpegPath, args...)
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("falha na extração ffmpeg: %w", err)
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

