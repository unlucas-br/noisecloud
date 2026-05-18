package encoder

import (
	"fmt"
	"os/exec"

	"ncc/internal/weave"
)

func (ve *VideoEncoder) EncodePayloadsWeaveTrailer(payloads []Payload, outputPath string, progressCh chan<- float64) error {
	if len(payloads) != 1 {
		return ve.EncodePayloadsWeave(payloads, outputPath, progressCh, "CPU (libx264)")
	}

	payload := payloads[0].Data
	_ = payload

	ffmpegPath := findFFmpeg()
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-nostats",
		"-y",
		"-f", "lavfi",
		"-i", "color=c=black:s=320x180:r=1",
		"-frames:v", "1",
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-crf", "40",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		outputPath,
	}

	if progressCh != nil {
		progressCh <- 0.1
	}

	cmd := exec.Command(ffmpegPath, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("falha ao criar video base compacto: %w\n%s", err, string(output))
	}

	if progressCh != nil {
		progressCh <- 0.5
	}

	if err := weave.AppendTrailer(outputPath, payload); err != nil {
		return fmt.Errorf("falha ao anexar payload compacto: %w", err)
	}

	if progressCh != nil {
		progressCh <- 1.0
	}
	return nil
}
