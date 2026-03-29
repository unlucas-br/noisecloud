package encoder

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
)

const (
	FramePlainHeaderSize = 16 
	CalibrationBarHeight = 16 
)

type FrameConfig struct {
	Width             int
	Height            int
	MacroSize         int
	FPS               int
	CalibrationHeight int 
	GrayLevels        int 
}

func (fc FrameConfig) TotalBytesCapacity() int {
	cols, rows := fc.GridSize()
	totalMacros := cols * rows

	bits := 1
	if fc.GrayLevels == 4 {
		bits = 2
	} else if fc.GrayLevels == 16 {
		bits = 4
	}

	return (totalMacros * bits) / 8
}

func HighDensityFrameConfig() FrameConfig {
	return FrameConfig{
		Width:             1280,
		Height:            720,
		MacroSize:         10,
		FPS:               30,
		CalibrationHeight: 16,
		GrayLevels:        4,
	}
}

func HQFrameConfig() FrameConfig {
	return FrameConfig{
		Width:             1920,
		Height:            1080,
		MacroSize:         24,
		FPS:               15,
		CalibrationHeight: 16,
		GrayLevels:        2,
	}
}

func SquareFrameConfig() FrameConfig {
	return FrameConfig{
		Width:             1080,
		Height:            1080,
		MacroSize:         24,
		FPS:               10,
		CalibrationHeight: 24,
		GrayLevels:        2,
	}
}

func DefaultFrameConfig() FrameConfig {
	return FrameConfig{
		Width:             1280,
		Height:            720,
		MacroSize:         16,
		FPS:               30,
		CalibrationHeight: 16,
		GrayLevels:        2,
	}
}

func (fc FrameConfig) GridSize() (cols, rows int) {
	cols = fc.Width / fc.MacroSize
	availableHeight := fc.Height - fc.CalibrationHeight
	rows = availableHeight / fc.MacroSize
	return
}

func (fc FrameConfig) CapacityPerFrame(eccCfg ECCConfig) int {
	bytesInFrame := fc.TotalBytesCapacity()

	totalShards := eccCfg.DataShards + eccCfg.ParityShards
	maxShardSize := bytesInFrame / totalShards
	dataCapacity := maxShardSize * eccCfg.DataShards

	payloadCapacity := dataCapacity - FramePlainHeaderSize

	if payloadCapacity < 0 {
		return 0
	}
	return payloadCapacity
}

type Frame struct {
	Config FrameConfig
	Data   []byte 
	ECC    *ECCEncoder
}

// NewFrame constrói a estrutura de um quadro, incluindo cabeçalho NCC3 e dados brutos
func NewFrame(cfg FrameConfig, ecc *ECCEncoder, index int, data []byte, totalFrames int, frameKey []byte, videoSalt []byte, shuffleKey []byte) (*Frame, error) {
	headerBuf := new(bytes.Buffer)
	headerBuf.Write([]byte{'N', 'C', 'C', '3'})
	binary.Write(headerBuf, binary.BigEndian, uint32(index))
	binary.Write(headerBuf, binary.BigEndian, uint32(totalFrames))
	binary.Write(headerBuf, binary.BigEndian, uint32(len(data)))

	plaintext := append(headerBuf.Bytes(), data...)

	return &Frame{
		Config: cfg,
		Data:   plaintext,
		ECC:    ecc,
	}, nil
}

func (f *Frame) Render(pixels []MacroPixel) ([]MacroPixel, error) {
	cols, rows := f.Config.GridSize()
	totalMacros := cols * rows

	shards, err := f.ECC.Encode(f.Data)
	if err != nil {
		return nil, fmt.Errorf("codificação ECC falhou: %w", err)
	}

	var allBytes []byte
	for _, shard := range shards {
		allBytes = append(allBytes, shard...)
	}

	maxBytes := f.Config.TotalBytesCapacity()

	if len(allBytes) < maxBytes {
		padding := make([]byte, maxBytes-len(allBytes))
		rand.Read(padding)
		allBytes = append(allBytes, padding...)
	}

	if len(allBytes) > maxBytes {
		return nil, fmt.Errorf("dados excedem a capacidade do quadro: %d bytes > %d max", len(allBytes), maxBytes)
	}

	if cap(pixels) < totalMacros {
		pixels = make([]MacroPixel, totalMacros) 
	}
	pixels = pixels[:totalMacros] 

	pixelsPerByte := 4
	if f.Config.GrayLevels == 2 {
		pixelsPerByte = 8
	}

	type coord struct{ x, y int }
	coords := make([]coord, 0, totalMacros)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			coords = append(coords, coord{x, y})
		}
	}

	for i := 0; i < totalMacros && i < len(coords); i++ {
		byteIdx := i / pixelsPerByte
		if byteIdx >= len(allBytes) {
			break
		}

		var bits byte
		if f.Config.GrayLevels == 2 {
			shift := uint(7 - (i % 8))
			bits = (allBytes[byteIdx] >> shift) & 0x01
		} else {
			shift := uint(6 - (i%4)*2)
			bits = (allBytes[byteIdx] >> shift) & 0x03
		}

		pixels[i] = MacroPixel{
			X:        coords[i].x * f.Config.MacroSize,
			Y:        coords[i].y * f.Config.MacroSize,
			DataByte: bits,
			Size:     f.Config.MacroSize,
			IsBinary: f.Config.GrayLevels == 2,
		}
	}

	return pixels, nil
}


