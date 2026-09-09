package ffmpeg

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/sys/windows"
)

// Windows denies replacement while another process reads a cache file. A named
// mutex serializes verification and replacement and is released even on a crash.
func lockCache(dir string) (func(), error) {
	name, err := windows.UTF16PtrFromString(fmt.Sprintf("Local\\NoiseCloudFFmpeg-%x", sha256.Sum256([]byte(strings.ToLower(dir)))))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil && err != windows.ERROR_ALREADY_EXISTS {
		return nil, err
	}
	// Win32 mutex ownership belongs to an OS thread, not a Go goroutine.
	runtime.LockOSThread()
	state, err := windows.WaitForSingleObject(handle, 120000)
	if err != nil || (state != windows.WAIT_OBJECT_0 && state != windows.WAIT_ABANDONED) {
		windows.CloseHandle(handle)
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("aguardar preparo do FFmpeg: estado %d, erro %v", state, err)
	}
	return func() { windows.ReleaseMutex(handle); windows.CloseHandle(handle); runtime.UnlockOSThread() }, nil
}
