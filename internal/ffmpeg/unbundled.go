//go:build !bundled_ffmpeg || !windows || !amd64

package ffmpeg

func bundled() (*bundle, error) { return nil, nil }
