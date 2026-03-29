package encoder

import (
	"image"
	"image/color"
)

type MacroPixel struct {
	X, Y     int
	DataByte byte
	Size     int
	IsBinary bool
}

var grayLevels = [4]uint8{32, 96, 160, 224}
var binaryLevels = [2]uint8{32, 224}

func NibbleToGray(bits byte) uint8 {
	if bits > 3 {
		bits = 3
	}
	return grayLevels[bits]
}

func BitToGray(bit byte) uint8 {
	if bit > 1 {
		bit = 1
	}
	return binaryLevels[bit]
}

func GrayToNibble(gray uint8) byte {
	if gray < 64 {
		return 0
	} else if gray < 128 {
		return 1
	} else if gray < 192 {
		return 2
	}
	return 3
}

func DynGrayToNibble(gray uint8, thresholds [3]uint8) byte {
	if gray < thresholds[0] {
		return 0
	} else if gray < thresholds[1] {
		return 1
	} else if gray < thresholds[2] {
		return 2
	}
	return 3
}

// ByteToGray converte o byte de dados do macro pixel para níveis de cinza
func (mp *MacroPixel) ByteToGray() uint8 {
	if mp.IsBinary {
		return BitToGray(mp.DataByte & 0x01)
	}
	return NibbleToGray(mp.DataByte & 0x03)
}

// GrayToByte converte níveis de cinza de volta para bytes de dados
func GrayToByte(gray uint8) byte {
	return GrayToNibble(gray)
}

// Render renderiza o macro pixel como uma imagem RGBA
func (mp *MacroPixel) Render() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, mp.Size, mp.Size))
	gray := mp.ByteToGray()
	c := color.RGBA{R: gray, G: gray, B: gray, A: 255}

	for y := 0; y < mp.Size; y++ {
		for x := 0; x < mp.Size; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func ExpandByte(b byte) [4]byte {
	return [4]byte{
		(b >> 6) & 0x03,
		(b >> 4) & 0x03,
		(b >> 2) & 0x03,
		b & 0x03,
	}
}

func CombineBits(bits [4]byte) byte {
	return (bits[0] << 6) | (bits[1] << 4) | (bits[2] << 2) | bits[3]
}

func CombineNibbles(high, low byte) byte {
	return ((high & 0x03) << 2) | (low & 0x03)
}

type ColorSpace struct {
	Y, U, V uint8
}

func (mp *MacroPixel) ByteToColor() ColorSpace {
	gray := mp.ByteToGray()
	return ColorSpace{Y: gray, U: 128, V: 128}
}

func YUVToRGB(y, u, v uint8) color.RGBA {
	return color.RGBA{R: y, G: y, B: y, A: 255}
}

func clampUint8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
