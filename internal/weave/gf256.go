package weave

var (
	gfExp      [512]byte
	gfLog      [256]byte
	gfMulTable [256][256]byte
)

func init() {
	var x byte = 1
	for i := 0; i < 255; i++ {
		gfExp[i] = x
		gfLog[x] = byte(i)
		x = gfMulNoTable(x, 2)
	}
	for i := 255; i < len(gfExp); i++ {
		gfExp[i] = gfExp[i-255]
	}
	for a := 0; a < 256; a++ {
		for b := 0; b < 256; b++ {
			gfMulTable[a][b] = gfMulNoTable(byte(a), byte(b))
		}
	}
}

func gfMul(a, b byte) byte {
	return gfMulTable[a][b]
}

func gfMulNoTable(a, b byte) byte {
	var p byte
	for b != 0 {
		if b&1 != 0 {
			p ^= a
		}
		hi := a & 0x80
		a <<= 1
		if hi != 0 {
			a ^= 0x1d
		}
		b >>= 1
	}
	return p
}

func gfPow(a byte, power int) byte {
	var out byte = 1
	for power > 0 {
		if power&1 != 0 {
			out = gfMul(out, a)
		}
		a = gfMul(a, a)
		power >>= 1
	}
	return out
}

func gfInv(a byte) byte {
	if a == 0 {
		return 0
	}
	return gfExp[255-int(gfLog[a])]
}

func gfDiv(a, b byte) byte {
	if a == 0 {
		return 0
	}
	return gfMul(a, gfInv(b))
}
