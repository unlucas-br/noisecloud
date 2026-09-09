package weave

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"testing"
)

func coreData(n int) []byte {
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(i*31 + i/7)
	}
	return data
}

func TestCoreRoundTripAndRecovery(t *testing.T) {
	for _, parity := range []int{0, 2, 3} {
		cfg := Config{PayloadSize: 8, DataFramesPerBlock: 16, RescueFramesPerBlock: parity}
		codec, err := NewCodec(cfg)
		if err != nil {
			t.Fatal(err)
		}
		for _, size := range []int{0, 1, 7, 8, 127, 128, 129, 255} {
			t.Run(fmt.Sprintf("parity=%d/bytes=%d", parity, size), func(t *testing.T) {
				want := coreData(size)
				frames, err := codec.Encode(want)
				if err != nil {
					t.Fatal(err)
				}
				for missing := 0; missing <= parity; missing++ {
					var surviving []Frame
					for _, frame := range frames {
						if frame.Header.FrameType == FrameTypeData && int(frame.Header.FrameIndex)%16 < missing {
							continue
						}
						surviving = append(surviving, frame)
					}
					got, _, err := codec.Reconstruct(surviving)
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("missing=%d: reusable round trip: %v", missing, err)
					}
					got, _, err = Reconstruct(surviving, cfg)
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("missing=%d: standalone round trip: %v", missing, err)
					}
				}
			})
		}
	}
}

func TestCoreFrameWireCompatibilityAndOwnership(t *testing.T) {
	frame := Frame{Header: Header{FrameType: FrameTypeData, BlockSize: 16, TotalFrames: 1, TotalDataBytes: 3}, Payload: []byte("abc")}
	want, err := hex.DecodeString("57455631" + "0100" + "0010" + "00000000" + "00000001" + "0003" + "0000" + "0000000000000003" + "352441c2" + "616263")
	if err != nil {
		t.Fatal(err)
	}
	packet, err := frame.MarshalBinary()
	if err != nil || !bytes.Equal(packet, want) {
		t.Fatalf("WEV1 bytes changed: %x, %v", packet, err)
	}
	prefix := make([]byte, 3, 3+len(packet))
	copy(prefix, "pre")
	appended, err := frame.AppendBinary(prefix)
	if err != nil || string(appended[:3]) != "pre" || !bytes.Equal(appended[3:], want) {
		t.Fatalf("append changed prefix or wire format: %v", err)
	}
	frame.Payload[0] = 'X'
	if !bytes.Equal(appended[3:], want) {
		t.Fatal("serialized payload aliases original data")
	}
	padded := append(append([]byte(nil), packet...), 0, 0)
	view, err := ParseFrameView(padded)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := ParseFrame(padded)
	if err != nil {
		t.Fatal(err)
	}
	view.Payload[0] = 'Z'
	if padded[HeaderSize] != 'Z' || string(owned.Payload) != "abc" {
		t.Fatal("view/copy payload ownership contract was violated")
	}
	if _, err := ParseFrameView(padded); err == nil {
		t.Fatal("CRC mismatch accepted")
	}
}

func TestCoreRejectsUntrustedMetadata(t *testing.T) {
	cfg := Config{PayloadSize: 8, DataFramesPerBlock: 16}
	valid, err := Encode(coreData(32), cfg)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Frame){
		"huge bytes":           func(f *Frame) { f.Header.TotalDataBytes = math.MaxUint64 },
		"huge frames":          func(f *Frame) { f.Header.TotalFrames = math.MaxUint32 },
		"above byte limit":     func(f *Frame) { f.Header.TotalDataBytes = MaxPayloadBytes + 1 },
		"frame count relation": func(f *Frame) { f.Header.TotalFrames++ },
		"inconsistent bytes":   func(f *Frame) { f.Header.TotalDataBytes++ },
		"out of range index":   func(f *Frame) { f.Header.FrameIndex = f.Header.TotalFrames },
		"zero block":           func(f *Frame) { f.Header.BlockSize = 0 },
		"different block":      func(f *Frame) { f.Header.BlockSize++ },
		"data size":            func(f *Frame) { f.Header.DataSize++ },
		"short frame":          func(f *Frame) { f.Payload = f.Payload[:7]; f.Header.DataSize = 7 },
		"data rescue index":    func(f *Frame) { f.Header.RescueIndex = 1 },
		"unknown type":         func(f *Frame) { f.Header.FrameType = 2 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			frames := append([]Frame(nil), valid...)
			change(&frames[0])
			if out, _, err := Reconstruct(frames, cfg); err == nil || out != nil {
				t.Fatalf("invalid metadata accepted or returned output: %v", err)
			}
		})
	}
	// All frames can agree and still claim a payload incompatible with their sizes.
	for i := range valid {
		valid[i].Header.TotalDataBytes = 31
	}
	if _, _, err := Reconstruct(valid, cfg); err == nil {
		t.Fatal("inexact final payload length accepted")
	}
}

func TestCoreDuplicateFrames(t *testing.T) {
	for _, parity := range []int{0, 2} {
		cfg := Config{PayloadSize: 8, DataFramesPerBlock: 16, RescueFramesPerBlock: parity}
		want := coreData(24)
		frames, err := Encode(want, cfg)
		if err != nil {
			t.Fatal(err)
		}
		indices := []int{0}
		if parity > 0 {
			indices = append(indices, 3)
		}
		for _, index := range indices {
			duplicate := append(append([]Frame(nil), frames...), frames[index])
			got, stats, err := Reconstruct(duplicate, cfg)
			if err != nil || !bytes.Equal(got, want) || stats.DataFramesSeen != 3 || stats.RescueFramesSeen != parity {
				t.Fatalf("identical duplicate: %v, stats=%+v", err, stats)
			}
			duplicate[len(duplicate)-1].Payload = append([]byte(nil), frames[index].Payload...)
			duplicate[len(duplicate)-1].Payload[0] ^= 1
			if _, _, err := Reconstruct(duplicate, cfg); err == nil {
				t.Fatal("conflicting duplicate accepted")
			}
		}
	}
}

func TestCoreInvalidConfigurationAndBudgets(t *testing.T) {
	for _, cfg := range []Config{
		{}, {PayloadSize: -1, DataFramesPerBlock: 16},
		{PayloadSize: math.MaxInt, DataFramesPerBlock: 16},
		{PayloadSize: 8, DataFramesPerBlock: math.MaxInt},
		{PayloadSize: 8, DataFramesPerBlock: 16, RescueFramesPerBlock: -1},
		{PayloadSize: 8, DataFramesPerBlock: 255, RescueFramesPerBlock: 1},
	} {
		if _, err := NewCodec(cfg); err == nil {
			t.Fatalf("invalid configuration accepted: %+v", cfg)
		}
	}
	for _, size := range []int{-1, math.MaxInt, MaxPayloadBytes + 1} {
		if _, err := PlanFor(size, DefaultConfig()); err == nil {
			t.Fatalf("invalid byte budget accepted: %d", size)
		}
	}
	if _, err := PlanFor(MaxPayloadBytes, Config{PayloadSize: 1, DataFramesPerBlock: 16}); err == nil {
		t.Fatal("excessive frame count accepted")
	}
	if _, err := PlanFor(MaxPayloadBytes, Config{PayloadSize: 65535, DataFramesPerBlock: 1, RescueFramesPerBlock: 254}); err == nil {
		t.Fatal("excessive parity bytes accepted")
	}
	for _, args := range []struct {
		payloads [][]byte
		size     int
	}{
		{nil, 8}, {[][]byte{{1}}, -1}, {[][]byte{make([]byte, 9)}, 8}, {make([][]byte, 254), 8},
	} {
		if got := BuildRescuePayloads(args.payloads, args.size); got != nil {
			t.Fatal("invalid rescue configuration must return nil")
		}
	}
}

func TestCoreMissingAndCorruptFrames(t *testing.T) {
	cfg := Config{PayloadSize: 8, DataFramesPerBlock: 16, RescueFramesPerBlock: 2}
	want := coreData(40)
	frames, err := Encode(want, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var packets [][]byte
	for _, frame := range frames {
		packet, err := frame.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		packets = append(packets, packet)
	}
	packets[0][HeaderSize] ^= 1
	got, stats, err := ReconstructBinary(packets, cfg)
	if err != nil || !bytes.Equal(got, want) || stats.RecoveredFrames != 1 || stats.InvalidFrames != 1 {
		t.Fatalf("corrupt frame recovery failed: %v, %+v", err, stats)
	}
	if _, _, err := Reconstruct(frames[3:], cfg); err == nil {
		t.Fatal("three missing data frames accepted with two parity frames")
	}
	// Out-of-order intact frames are valid and cannot depend on ordering votes.
	for left, right := 0, len(frames)-1; left < right; left, right = left+1, right-1 {
		frames[left], frames[right] = frames[right], frames[left]
	}
	got, _, err = Reconstruct(frames, cfg)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("out-of-order frames failed: %v", err)
	}
}

func TestCoreIntactStorageFastPathReusesBuffers(t *testing.T) {
	cfg := Config{PayloadSize: 32768, DataFramesPerBlock: 16}
	codec, err := NewCodec(cfg)
	if err != nil {
		t.Fatal(err)
	}
	source := coreData(1024 * 1024)
	frames, err := codec.Encode(source)
	if err != nil {
		t.Fatal(err)
	}
	dst := make([]byte, len(source))
	allocs := testing.AllocsPerRun(20, func() {
		var err error
		frames, err = codec.EncodeTo(frames[:0], source)
		if err != nil {
			panic(err)
		}
		dst, _, err = codec.ReconstructTo(dst[:0], frames)
		if err != nil {
			panic(err)
		}
	})
	if allocs != 0 || !bytes.Equal(dst, source) {
		t.Fatalf("intact reused core loop: %.1f allocations", allocs)
	}
}

func FuzzCoreFrameMetadata(f *testing.F) {
	cfg := Config{PayloadSize: 64, DataFramesPerBlock: 16, RescueFramesPerBlock: 2}
	frames, _ := Encode(coreData(64), cfg)
	packet, _ := frames[0].MarshalBinary()
	f.Add(packet)
	f.Add([]byte("WEV1"))
	f.Fuzz(func(t *testing.T, packet []byte) {
		if len(packet) > 4096 {
			return
		}
		frame, err := ParseFrameView(packet)
		if err != nil || frame.Header.TotalDataBytes > 4096 {
			return
		}
		out, _, err := Reconstruct([]Frame{frame}, cfg)
		if err == nil && uint64(len(out)) != frame.Header.TotalDataBytes {
			t.Fatal("accepted output with incorrect declared size")
		}
	})
}

func FuzzCoreRecovery(f *testing.F) {
	f.Add([]byte("payload"), uint8(0), uint8(1))
	f.Add([]byte{}, uint8(0), uint8(0))
	f.Fuzz(func(t *testing.T, data []byte, first, second uint8) {
		if len(data) > 1024 {
			return
		}
		cfg := Config{PayloadSize: 64, DataFramesPerBlock: 16, RescueFramesPerBlock: 2}
		codec, err := NewCodec(cfg)
		if err != nil {
			t.Fatal(err)
		}
		frames, err := codec.Encode(data)
		if err != nil {
			t.Fatal(err)
		}
		count := frames[0].Header.TotalFrames
		var surviving []Frame
		for _, frame := range frames {
			if frame.Header.FrameType == FrameTypeData && (frame.Header.FrameIndex == uint32(first)%count || frame.Header.FrameIndex == uint32(second)%count) {
				continue
			}
			surviving = append(surviving, frame)
		}
		out, _, err := codec.Reconstruct(surviving)
		if err != nil || !bytes.Equal(out, data) {
			t.Fatalf("recovery failed: %v", err)
		}
	})
}

func BenchmarkCoreStorageIntact(b *testing.B) {
	for _, parity := range []int{0, 2} {
		b.Run(fmt.Sprint("parity=", parity), func(b *testing.B) {
			codec, err := NewCodec(Config{PayloadSize: 32768, DataFramesPerBlock: 16, RescueFramesPerBlock: parity})
			if err != nil {
				b.Fatal(err)
			}
			source := coreData(1024 * 1024)
			frames, err := codec.Encode(source)
			if err != nil {
				b.Fatal(err)
			}
			dst := make([]byte, len(source))
			b.SetBytes(int64(len(source)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				dst, _, err = codec.ReconstructTo(dst[:0], frames)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
