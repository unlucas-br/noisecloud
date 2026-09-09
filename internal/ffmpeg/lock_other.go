//go:build !windows

package ffmpeg

import "sync"

// Embedded releases currently target Windows. Other platforms exercise the
// cache through unit tests and use external FFmpeg for development builds.
var cacheMu sync.Mutex

func lockCache(string) (func(), error) {
	cacheMu.Lock()
	return cacheMu.Unlock, nil
}
