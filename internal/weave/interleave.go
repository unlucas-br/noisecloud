package weave

import "sort"

// InterleaveByBlock reorders frames so consecutive transport losses are spread
// across different Weave blocks. Reconstruct does not require deinterleaving
// because every frame is self-identifying.
func InterleaveByBlock(frames []Frame) []Frame {
	type blockFrames map[int]Frame

	blocks := make(map[uint32]blockFrames)
	var blockStarts []uint32
	seenBlocks := make(map[uint32]bool)
	var passthrough []Frame
	maxSlot := 0

	for _, frame := range frames {
		h := frame.Header
		if h.BlockSize == 0 {
			passthrough = append(passthrough, frame)
			continue
		}

		var blockStart uint32
		var slot int
		blockSize := uint32(h.BlockSize)
		switch h.FrameType {
		case FrameTypeData:
			blockStart = (h.FrameIndex / blockSize) * blockSize
			slot = int(h.FrameIndex - blockStart)
		case FrameTypeRescue:
			blockStart = h.FrameIndex
			slot = int(h.BlockSize) + int(h.RescueIndex)
		default:
			passthrough = append(passthrough, frame)
			continue
		}

		if !seenBlocks[blockStart] {
			seenBlocks[blockStart] = true
			blockStarts = append(blockStarts, blockStart)
		}
		if blocks[blockStart] == nil {
			blocks[blockStart] = make(blockFrames)
		}
		blocks[blockStart][slot] = frame
		if slot > maxSlot {
			maxSlot = slot
		}
	}

	sort.Slice(blockStarts, func(i, j int) bool {
		return blockStarts[i] < blockStarts[j]
	})

	out := make([]Frame, 0, len(frames))
	for slot := 0; slot <= maxSlot; slot++ {
		for _, blockStart := range blockStarts {
			if frame, ok := blocks[blockStart][slot]; ok {
				out = append(out, frame)
			}
		}
	}
	out = append(out, passthrough...)
	return out
}
