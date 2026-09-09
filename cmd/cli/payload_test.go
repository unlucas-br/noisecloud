package main

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func legacyGzip(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPayloadCompressionCompatibility(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output.bin")
	for _, size := range []int{0, 1, 127, 128, 1024, (1 << 20) + 1} {
		want := make([]byte, size)
		for i := range want {
			want[i] = byte(i*31 + i/7)
		}
		compressed, err := compressData(want)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(compressed, []byte{0x28, 0xb5, 0x2f, 0xfd}) {
			t.Fatal("new payload is not Zstandard")
		}
		for _, payload := range [][]byte{compressed, legacyGzip(t, want)} {
			if err := writeDecompressedPayload(payload, path); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("round trip size %d: %v", size, err)
			}
		}
	}
}

func TestPayloadFailurePreservesOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.bin")
	want := []byte("existing output")
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	zstd, err := compressData(bytes.Repeat([]byte("payload"), 100))
	if err != nil {
		t.Fatal(err)
	}
	for _, good := range [][]byte{zstd, legacyGzip(t, bytes.Repeat([]byte("payload"), 100))} {
		badCRC := bytes.Clone(good)
		badCRC[len(badCRC)-1] ^= 1
		for _, bad := range [][]byte{nil, []byte("unknown"), good[:len(good)-1], badCRC} {
			if err := writeDecompressedPayload(bad, path); err == nil {
				t.Fatal("bad payload accepted")
			}
		}
		if err := writeDecompressedPayloadLimit(good, path, 699); err == nil {
			t.Fatal("expansion limit ignored")
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("invalid payload replaced destination", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary file leaked", err)
	}
	if err := writeDecompressedPayloadLimit(zstd, path, 700); err != nil {
		t.Fatal("exact limit rejected", err)
	}
}
