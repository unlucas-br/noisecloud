//go:build bundled_ffmpeg

package ffmpeg

import (
	_ "embed"
	"encoding/json"
)

//go:embed assets/ffmpeg.exe.gz
var compressedFFmpeg []byte

//go:embed assets/LICENSE.txt
var ffmpegLicense []byte

//go:embed assets/README.txt
var ffmpegReadme []byte

//go:embed bundle.json
var bundleJSON []byte

func bundled() (*bundle, error) {
	var meta struct {
		Digest string `json:"binary_sha256"`
		Size   int64  `json:"binary_size"`
	}
	if err := json.Unmarshal(bundleJSON, &meta); err != nil {
		return nil, err
	}
	return &bundle{compressed: compressedFFmpeg, digest: meta.Digest, size: meta.Size, license: ffmpegLicense, readme: ffmpegReadme}, nil
}
