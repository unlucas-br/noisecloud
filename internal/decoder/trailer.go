package decoder

import "ncc/internal/weave"

func ReadWeaveTrailer(videoPath string) ([]byte, bool, error) {
	return weave.ReadTrailer(videoPath)
}
