package weave

// Codec holds validated configuration and reusable workspace for repeated
// encode/reconstruct loops.
//
// A Codec is not safe for concurrent use. Create one Codec per worker when
// encoding or reconstructing in parallel.
type Codec struct {
	cfg           Config
	coeffs        coefficientTable
	blockPayloads [][]byte
	reconstruct   reconstructWorkspace
}

// NewCodec validates cfg and returns a reusable codec workspace.
func NewCodec(cfg Config) (*Codec, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Codec{
		cfg:           cfg,
		coeffs:        newCoefficientTable(cfg),
		blockPayloads: make([][]byte, cfg.DataFramesPerBlock),
	}, nil
}

// Config returns the validated configuration used by c.
func (c *Codec) Config() Config {
	return c.cfg
}

// Encode turns a payload into systematic data frames plus rescue frames.
func (c *Codec) Encode(data []byte) ([]Frame, error) {
	return c.EncodeTo(nil, data)
}

// EncodeTo appends encoded frames to dst and returns the extended slice.
func (c *Codec) EncodeTo(dst []Frame, data []byte) ([]Frame, error) {
	return encodeToValidated(dst, data, c.cfg, c.coeffs, c.blockPayloads)
}

// Reconstruct repairs and reassembles frames produced by Encode.
func (c *Codec) Reconstruct(frames []Frame) ([]byte, DecodeStats, error) {
	return c.ReconstructTo(nil, frames)
}

// ReconstructTo repairs and reassembles frames into dst.
func (c *Codec) ReconstructTo(dst []byte, frames []Frame) ([]byte, DecodeStats, error) {
	return reconstructToValidated(dst, frames, c.cfg, c.coeffs, &c.reconstruct)
}

type reconstructWorkspace struct {
	totalFrameVotes map[uint32]int
	totalByteVotes  map[uint64]int
	blockSizeVotes  map[uint16]int
	dataFrames      [][]byte
	rescueFrames    [][][]byte
	inverseCache    map[string][][]byte
	inverseData     int
	inverseRescue   int
	missing         []uint32
	available       []uint16
	recovered       [][]byte
	recoveredCursor int
	scratch         repairScratch
}

func (w *reconstructWorkspace) voteMaps() (map[uint32]int, map[uint64]int, map[uint16]int) {
	if w.totalFrameVotes == nil {
		w.totalFrameVotes = make(map[uint32]int)
	} else {
		clear(w.totalFrameVotes)
	}
	if w.totalByteVotes == nil {
		w.totalByteVotes = make(map[uint64]int)
	} else {
		clear(w.totalByteVotes)
	}
	if w.blockSizeVotes == nil {
		w.blockSizeVotes = make(map[uint16]int)
	} else {
		clear(w.blockSizeVotes)
	}
	return w.totalFrameVotes, w.totalByteVotes, w.blockSizeVotes
}

func (w *reconstructWorkspace) dataFrameSlots(totalFrames uint32) [][]byte {
	if cap(w.dataFrames) < int(totalFrames) {
		w.dataFrames = make([][]byte, totalFrames)
	}
	out := w.dataFrames[:totalFrames]
	clear(out)
	w.dataFrames = out
	return out
}

func (w *reconstructWorkspace) rescueFrameSlots(blockCount int, rescueCount int) [][][]byte {
	if cap(w.rescueFrames) < blockCount {
		w.rescueFrames = make([][][]byte, blockCount)
	}
	out := w.rescueFrames[:blockCount]
	for i := range out {
		if cap(out[i]) < rescueCount {
			out[i] = make([][]byte, rescueCount)
		}
		out[i] = out[i][:rescueCount]
		clear(out[i])
	}
	w.rescueFrames = out
	return out
}

func (w *reconstructWorkspace) inverseCacheFor(cfg Config) map[string][][]byte {
	if w.inverseCache == nil || w.inverseData != cfg.DataFramesPerBlock || w.inverseRescue != cfg.RescueFramesPerBlock {
		w.inverseCache = make(map[string][][]byte)
		w.inverseData = cfg.DataFramesPerBlock
		w.inverseRescue = cfg.RescueFramesPerBlock
	}
	return w.inverseCache
}

func (w *reconstructWorkspace) beginRecoveredPayloads() {
	w.recoveredCursor = 0
}

func (w *reconstructWorkspace) recoveredPayload(size int) []byte {
	if size == 0 {
		return nil
	}
	if w.recoveredCursor == len(w.recovered) {
		w.recovered = append(w.recovered, nil)
	}
	payload := w.recovered[w.recoveredCursor]
	w.recoveredCursor++
	if cap(payload) < size {
		payload = make([]byte, size)
	}
	payload = payload[:size]
	w.recovered[w.recoveredCursor-1] = payload
	return payload
}
