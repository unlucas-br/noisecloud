package ffmpeg

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type bundle struct {
	compressed []byte
	digest     string
	size       int64
	license    []byte
	readme     []byte
}

func matchesFile(path, digest string, size int64) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, size+1))
	return err == nil && n == size && hex.EncodeToString(h.Sum(nil)) == digest
}

// A unique temporary file and atomic rename allow separate NoiseCloud processes
// to initialize the same version concurrently without executing partial files.
func materialize(b bundle, dir string) (string, error) {
	unlock, err := lockCache(dir)
	if err != nil {
		return "", err
	}
	defer unlock()
	path := filepath.Join(dir, "ffmpeg.exe")
	if matchesFile(path, b.digest, b.size) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	gz, err := gzip.NewReader(bytes.NewReader(b.compressed))
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tmp, err := os.CreateTemp(dir, ".ffmpeg-*.exe")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(gz, b.size+1))
	if err != nil {
		return "", err
	}
	if n != b.size || hex.EncodeToString(h.Sum(nil)) != b.digest {
		return "", fmt.Errorf("integridade do FFmpeg integrado invalida")
	}
	if err := tmp.Chmod(0700); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := writeNotice(dir, "LICENSE.txt", b.license); err != nil {
		return "", err
	}
	if err := writeNotice(dir, "README.txt", b.readme); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		if matchesFile(path, b.digest, b.size) {
			return path, nil
		}
		return "", err
	}
	return path, nil
}

func writeNotice(dir, name string, data []byte) error {
	path := filepath.Join(dir, name)
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	tmp, err := os.CreateTemp(dir, ".notice-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		if existing, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(existing, data) {
			return nil
		}
		return err
	}
	return nil
}
