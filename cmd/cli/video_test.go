package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"ncc/internal/decoder"
	"ncc/internal/encoder"
	"ncc/internal/ffmpeg"
)

// Enable explicitly: NCC_TEST_FFMPEG=1 go test ./cmd/cli -run TestVideoRoundTrip.
func TestVideoRoundTrip(t *testing.T) {
	if os.Getenv("NCC_TEST_FFMPEG") != "1" {
		t.Skip("set NCC_TEST_FFMPEG=1 for real H.264 video tests")
	}
	if _, err := ffmpeg.Path(); err != nil {
		t.Fatal("FFmpeg required:", err)
	}
	want := make([]byte, 1100)
	// Deterministic high-entropy bytes exercise several frames after compression.
	state := uint32(12345)
	for i := range want {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		want[i] = byte(state)
	}
	for _, preset := range []string{"weave", "tiktok"} {
		for _, compression := range []string{"zstd", "gzip"} {
			t.Run(preset+"/"+compression, func(t *testing.T) {
				dir := t.TempDir()
				input, video, output := filepath.Join(dir, "input.bin"), filepath.Join(dir, "video.mp4"), filepath.Join(dir, "output.bin")
				if err := os.WriteFile(input, want, 0600); err != nil {
					t.Fatal(err)
				}
				if compression == "zstd" {
					if err := runEncode(input, video, 1, preset, "none", 0, nil); err != nil {
						t.Fatal(err)
					}
				} else {
					enc, err := encoder.NewVideoEncoder(1, preset, "none", nil)
					if err != nil {
						t.Fatal(err)
					}
					defer enc.Cleanup()
					if err := enc.EncodePayloads([]encoder.Payload{{Data: legacyGzip(t, want)}}, video, nil); err != nil {
						t.Fatal(err)
					}
				}
				if err := runDecode(video, output, preset, nil); err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(output)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatal("video round trip mismatch", err)
				}
				extractor, err := decoder.NewFrameExtractor(preset)
				if err != nil {
					t.Fatal(err)
				}
				defer extractor.Cleanup()
				frames, err := extractor.ExtractFrames(video, nil)
				if err != nil || len(frames) < 3 {
					t.Fatal("extract frames", err)
				}
				// Lose two real PNG frames and make one additional input unreadable.
				surviving := append([]string{filepath.Join(dir, "missing.png")}, frames[2:]...)
				progress := make(chan float64, len(surviving))
				raw := filepath.Join(dir, "raw.bin")
				if err := decoder.NewFrameReconstructor(preset).ReconstructFile(surviving, raw, progress); err != nil {
					t.Fatal(err)
				}
				close(progress)
				last := 0.0
				for p := range progress {
					if p < last {
						t.Fatal("progress regressed")
					}
					last = p
				}
				if last != 1 {
					t.Fatal("incomplete progress", last)
				}
				payload, err := os.ReadFile(raw)
				if err != nil {
					t.Fatal(err)
				}
				if err := writeDecompressedPayload(payload, output); err != nil {
					t.Fatal(err)
				}
				got, err = os.ReadFile(output)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatal("repaired video mismatch", err)
				}
			})
		}
	}
}
