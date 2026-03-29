package encoder

import (
	"bytes"
	"fmt"
	"io"

	"github.com/klauspost/reedsolomon"
)

type ECCConfig struct {
	DataShards   int
	ParityShards int
}

// NewECCConfig define o padrão fixo de 16 Data Shards + 8 Parity Shards (Lite Standard)
func NewECCConfig() ECCConfig {
	return ECCConfig{DataShards: 16, ParityShards: 8}
}

type ECCEncoder struct {
	enc    reedsolomon.Encoder
	Config ECCConfig 
}

func NewECCEncoder(cfg ECCConfig) (*ECCEncoder, error) {
	enc, err := reedsolomon.New(cfg.DataShards, cfg.ParityShards)
	if err != nil {
		return nil, fmt.Errorf("falha ao criar encoder RS: %w", err)
	}
	return &ECCEncoder{enc: enc, Config: cfg}, nil
}

func (e *ECCEncoder) Encode(data []byte) ([][]byte, error) {
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	shards, err := e.enc.Split(dataCopy)
	if err != nil {
		return nil, fmt.Errorf("falha ao particionar dados (split): %w", err)
	}
	if err := e.enc.Encode(shards); err != nil {
		return nil, fmt.Errorf("falha na codificação RS: %w", err)
	}
	return shards, nil
}

func (e *ECCEncoder) Verify(shards [][]byte) (bool, error) {
	return e.enc.Verify(shards)
}

func (e *ECCEncoder) Reconstruct(shards [][]byte) error {
	return e.enc.Reconstruct(shards)
}

func (e *ECCEncoder) Join(shards [][]byte, outSize int) ([]byte, error) {
	buf := new(bytes.Buffer)
	err := e.enc.Join(io.Writer(buf), shards, outSize)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

