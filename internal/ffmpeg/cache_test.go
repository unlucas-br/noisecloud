package ffmpeg

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func testBundle(t *testing.T) (bundle, []byte) {
	t.Helper()
	data := bytes.Repeat([]byte("synthetic runtime bytes"), 200)
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return bundle{compressed: compressed.Bytes(), digest: fmt.Sprintf("%x", sha256.Sum256(data)), size: int64(len(data)), license: []byte("test license"), readme: []byte("test origin")}, data
}

func TestCacheExtractionReuseAndRepair(t *testing.T) {
	b, want := testBundle(t)
	dir := filepath.Join(t.TempDir(), "cache")
	path, err := materialize(b, dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := materialize(b, dir); err != nil || again != path {
		t.Fatal("reuse failed", err)
	}
	after, err := os.Stat(path)
	if err != nil || !info.ModTime().Equal(after.ModTime()) {
		t.Fatal("valid cache was rewritten", err)
	}
	bad := bytes.Clone(want)
	bad[0] ^= 1
	if err := os.WriteFile(path, bad, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := materialize(b, dir); err != nil {
		t.Fatal("repair failed", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("invalid cached binary reused", err)
	}
	for _, name := range []string{"LICENSE.txt", "README.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInvalidBundleNeverCreatesExecutable(t *testing.T) {
	b, _ := testBundle(t)
	badCRC := bytes.Clone(b.compressed)
	badCRC[len(badCRC)-5] ^= 1
	for _, bad := range []bundle{
		{compressed: []byte("broken"), digest: b.digest, size: b.size},
		{compressed: badCRC, digest: b.digest, size: b.size},
		{compressed: b.compressed, digest: "wrong", size: b.size},
		{compressed: b.compressed, digest: b.digest, size: b.size - 1},
		{compressed: b.compressed, digest: b.digest, size: b.size + 1},
	} {
		dir := t.TempDir()
		if _, err := materialize(bad, dir); err == nil {
			t.Fatal("corrupt bundle accepted")
		}
		if _, err := os.Stat(filepath.Join(dir, "ffmpeg.exe")); !os.IsNotExist(err) {
			t.Fatal("corrupt executable was installed", err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatal("partial files leaked", err)
		}
	}
}

func TestConcurrentExtraction(t *testing.T) {
	b, want := testBundle(t)
	dir := t.TempDir()
	errors := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := materialize(b, dir); errors <- err }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(filepath.Join(dir, "ffmpeg.exe"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("concurrent extraction failed", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 3 {
		t.Fatal("temporary files leaked", err)
	}
}

func TestExternalDevelopmentRuntime(t *testing.T) {
	dir := t.TempDir()
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	want := filepath.Join(dir, name)
	if err := os.WriteFile(want, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err := externalPath()
	if err != nil || got != want {
		t.Fatalf("external development runtime: %s, %v", got, err)
	}
}
