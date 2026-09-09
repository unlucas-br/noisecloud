package encoder

import (
	"bytes"
	"testing"
)

func TestWeaveRenderingClearsReusedPadding(t *testing.T) {
	for _, levels := range []int{2, 4} {
		cfg := CompactWeaveFrameConfig()
		cfg.GrayLevels = levels
		ve := &VideoEncoder{FrameCfg: cfg}
		full := bytes.Repeat([]byte{0xff}, cfg.TotalBytesCapacity())
		pixels, err := ve.renderWeavePacket(full, nil)
		if err != nil {
			t.Fatal(err)
		}
		pixels, err = ve.renderWeavePacket([]byte{0xa5}, pixels)
		if err != nil {
			t.Fatal(err)
		}
		perByte, mask := 8, byte(1)
		if levels == 4 {
			perByte, mask = 4, 3
		}
		for i, pixel := range pixels {
			want := byte(0)
			if i < perByte {
				want = (0xa5 >> uint((perByte-1-i)*(8/perByte))) & mask
			}
			if pixel.DataByte != want {
				t.Fatalf("levels=%d pixel=%d: got %d want %d", levels, i, pixel.DataByte, want)
			}
		}
		if _, err := ve.renderWeavePacket(append(full, 0), pixels); err == nil {
			t.Fatal("oversized packet accepted")
		}
	}
}

func TestWeaveRejectsAmbiguousPayloads(t *testing.T) {
	ve := &VideoEncoder{FrameCfg: CompactWeaveFrameConfig()}
	for _, payloads := range [][]Payload{nil, {{Data: []byte("one")}, {Data: []byte("two")}}} {
		if err := ve.EncodePayloadsWeave(payloads, "unused.mp4", nil, ""); err == nil {
			t.Fatal("ambiguous stream accepted")
		}
	}
}
