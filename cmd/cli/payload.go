package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
	"ncc/internal/weave"
)

const maxDecodedPayloadBytes = 1 << 30

// New videos use a standard Zstandard stream inside unchanged WEV1 frames.
func compressData(data []byte) ([]byte, error) {
	if len(data) > maxDecodedPayloadBytes {
		return nil, fmt.Errorf("arquivo excede o limite de 1 GiB")
	}
	enc, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.SpeedDefault),
		zstd.WithEncoderConcurrency(1),
		zstd.WithWindowSize(1<<20),
		zstd.WithEncoderCRC(true),
		zstd.WithZeroFrames(true),
	)
	if err != nil {
		return nil, err
	}
	defer enc.Close()
	encoded := enc.EncodeAll(data, nil)
	if len(encoded) > weave.MaxPayloadBytes {
		return nil, fmt.Errorf("payload comprimido excede o limite de 256 MiB")
	}
	return encoded, nil
}

func writeDecompressedPayload(data []byte, outputPath string) error {
	return writeDecompressedPayloadLimit(data, outputPath, maxDecodedPayloadBytes)
}

func writeDecompressedPayloadLimit(data []byte, outputPath string, limit int64) error {
	if len(data) > weave.MaxPayloadBytes || limit < 0 || limit > maxDecodedPayloadBytes {
		return fmt.Errorf("payload excede os limites de descompressao")
	}
	var reader io.Reader
	switch {
	case bytes.HasPrefix(data, []byte{0x28, 0xb5, 0x2f, 0xfd}):
		dec, err := zstd.NewReader(bytes.NewReader(data),
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderMaxMemory(8<<20),
			zstd.WithDecoderMaxWindow(8<<20),
		)
		if err != nil {
			return fmt.Errorf("falha ao iniciar Zstandard: %w", err)
		}
		defer dec.Close()
		reader = dec
	case bytes.HasPrefix(data, []byte{0x1f, 0x8b}):
		// Gzip remains readable for videos and compact trailers from older versions.
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("falha ao iniciar Gzip: %w", err)
		}
		defer gz.Close()
		reader = gz
	default:
		return fmt.Errorf("formato de compressao desconhecido")
	}

	// Validate the complete stream before replacing the destination, including
	// checksums at EOF. Keep the temporary file on the destination filesystem.
	tmp, err := os.CreateTemp(filepath.Dir(outputPath), ".ncc-decode-*")
	if err != nil {
		return fmt.Errorf("criar arquivo temporario: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	n, err := io.Copy(tmp, io.LimitReader(reader, limit+1))
	if err != nil {
		return fmt.Errorf("falha na descompressao: %w", err)
	}
	if n > limit {
		return fmt.Errorf("arquivo descomprimido excede o limite de %d bytes", limit)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("fechar arquivo final: %w", err)
	}
	if err := os.Rename(tmp.Name(), outputPath); err != nil {
		return fmt.Errorf("salvar arquivo final: %w", err)
	}
	return nil
}
