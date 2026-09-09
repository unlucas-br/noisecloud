// Package ffmpeg locates the bundled runtime or an external development install.
package ffmpeg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

var resolveMu sync.Mutex
var resolvedPath string

// Path prepares the embedded FFmpeg on first use. Release builds never need
// network access, an installer, administrator privileges, or a PATH change.
func Path() (string, error) {
	resolveMu.Lock()
	defer resolveMu.Unlock()
	if resolvedPath != "" {
		if info, err := os.Stat(resolvedPath); err == nil && info.Mode().IsRegular() {
			return resolvedPath, nil
		}
	}
	bundle, err := bundled()
	if err != nil {
		return "", err
	}
	if bundle != nil {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("localizar cache do FFmpeg: %w", err)
		}
		resolvedPath, err = materialize(*bundle, filepath.Join(cache, "NoiseCloud", "ffmpeg", bundle.digest))
		if err != nil {
			return "", fmt.Errorf("preparar FFmpeg integrado: %w", err)
		}
		return resolvedPath, nil
	}
	return externalPath()
}

func externalPath() (string, error) {
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if executable, err := os.Executable(); err == nil {
		for _, p := range []string{filepath.Join(filepath.Dir(executable), name), filepath.Join(filepath.Dir(executable), "bin", name)} {
			if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
				return p, nil
			}
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	if runtime.GOOS == "windows" {
		locations := []string{`C:\ffmpeg\bin\ffmpeg.exe`, `C:\Program Files\ffmpeg\bin\ffmpeg.exe`, `C:\Program Files (x86)\ffmpeg\bin\ffmpeg.exe`}
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			locations = append(locations, filepath.Join(local, "Microsoft", "WinGet", "Links", name))
		}
		if profile := os.Getenv("USERPROFILE"); profile != "" {
			locations = append(locations, filepath.Join(profile, "scoop", "shims", name))
		}
		for _, p := range locations {
			if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("FFmpeg nao encontrado nesta compilacao. Baixe o ncc.exe completo nas releases ou compile com -tags bundled_ffmpeg")
}
