// bundle-ffmpeg prepares verified assets for -tags bundled_ffmpeg builds.
package main

import (
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type manifest struct {
	Version     string `json:"version"`
	URL         string `json:"archive_url"`
	ArchiveHash string `json:"archive_sha256"`
	Root        string `json:"archive_root"`
	BinaryHash  string `json:"binary_sha256"`
	BinarySize  int64  `json:"binary_size"`
}

func main() {
	archive := flag.String("archive", "", "use an already downloaded archive")
	flag.Parse()
	if err := run(*archive); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(archive string) error {
	data, err := os.ReadFile("internal/ffmpeg/bundle.json")
	if err != nil {
		return err
	}
	var meta manifest
	if err = json.Unmarshal(data, &meta); err != nil {
		return err
	}
	if archive == "" {
		tmp, err := os.CreateTemp("", "noisecloud-ffmpeg-*.zip")
		if err != nil {
			return err
		}
		archive = tmp.Name()
		defer os.Remove(archive)
		defer tmp.Close()
		client := &http.Client{Timeout: 10 * time.Minute}
		response, err := client.Get(meta.URL)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("download FFmpeg: HTTP %d", response.StatusCode)
		}
		n, err := io.Copy(tmp, io.LimitReader(response.Body, 150*1024*1024+1))
		if err != nil {
			return err
		}
		if n > 150*1024*1024 {
			return fmt.Errorf("FFmpeg archive exceeds limit")
		}
		if err = tmp.Close(); err != nil {
			return err
		}
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, copyErr := io.Copy(h, f)
	f.Close()
	if copyErr != nil {
		return copyErr
	}
	if hex.EncodeToString(h.Sum(nil)) != meta.ArchiveHash {
		return fmt.Errorf("FFmpeg archive SHA-256 mismatch")
	}
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	out := "internal/ffmpeg/assets"
	if err = os.MkdirAll(out, 0755); err != nil {
		return err
	}
	// Read only the named members; archive paths never become output paths.
	for member, destination := range map[string]string{"bin/ffmpeg.exe": "ffmpeg.exe.gz", "LICENSE": "LICENSE.txt", "README.txt": "README.txt"} {
		entry, err := zr.Open(meta.Root + "/" + member)
		if err != nil {
			return err
		}
		if member == "bin/ffmpeg.exe" {
			err = packBinary(entry, filepath.Join(out, destination), meta)
		} else {
			var notice []byte
			notice, err = io.ReadAll(io.LimitReader(entry, 1024*1024+1))
			if err == nil && len(notice) > 1024*1024 {
				err = fmt.Errorf("FFmpeg notice exceeds limit")
			}
			if err == nil {
				err = os.WriteFile(filepath.Join(out, destination), notice, 0644)
			}
		}
		entry.Close()
		if err != nil {
			return err
		}
	}
	fmt.Printf("FFmpeg %s verified and prepared for embedding\n", meta.Version)
	return nil
}

func packBinary(input io.Reader, destination string, meta manifest) error {
	tmp, err := os.CreateTemp(filepath.Dir(destination), "ffmpeg-pack-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	z, err := gzip.NewWriterLevel(tmp, gzip.BestCompression)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(z, h), io.LimitReader(input, meta.BinarySize+1))
	closeErr := z.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if n != meta.BinarySize || hex.EncodeToString(h.Sum(nil)) != meta.BinaryHash {
		return fmt.Errorf("FFmpeg executable SHA-256 mismatch")
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), destination)
}
