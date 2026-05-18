package weave

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

const (
	trailerMagic      = "NCCWTR1!"
	trailerFooterSize = 8 + 4 + len(trailerMagic)
)

func AppendTrailer(path string, payload []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(payload); err != nil {
		return err
	}

	var footer [trailerFooterSize]byte
	binary.BigEndian.PutUint64(footer[0:8], uint64(len(payload)))
	binary.BigEndian.PutUint32(footer[8:12], crc32.ChecksumIEEE(payload))
	copy(footer[12:], trailerMagic)
	_, err = f.Write(footer[:])
	return err
}

func ReadTrailer(path string) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	footerSize := int64(trailerFooterSize)
	if info.Size() < footerSize {
		return nil, false, nil
	}

	footer := make([]byte, trailerFooterSize)
	if _, err := f.ReadAt(footer, info.Size()-footerSize); err != nil {
		return nil, false, err
	}
	if string(footer[12:]) != trailerMagic {
		return nil, false, nil
	}

	payloadLen := int64(binary.BigEndian.Uint64(footer[0:8]))
	payloadStart := info.Size() - footerSize - payloadLen
	if payloadLen < 0 || payloadStart < 0 {
		return nil, true, fmt.Errorf("trailer compacto invalido")
	}

	payload := make([]byte, int(payloadLen))
	if _, err := f.ReadAt(payload, payloadStart); err != nil && err != io.EOF {
		return nil, true, err
	}

	expectedCRC := binary.BigEndian.Uint32(footer[8:12])
	if crc32.ChecksumIEEE(payload) != expectedCRC {
		return nil, true, fmt.Errorf("checksum do trailer compacto nao confere")
	}

	return payload, true, nil
}
