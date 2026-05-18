package weave

import (
	"errors"
	"fmt"
	"math"
)

const (
	Magic              = "WEV1"
	Version            = 1
	HeaderSize         = 32
	DefaultBlockSize   = 16
	DefaultRescueCount = 2
)

var (
	ErrInvalidConfig = errors.New("invalid weave config")
	ErrNoValidFrames = errors.New("no valid weave frames")
	ErrUnrecoverable = errors.New("unrecoverable weave block")
)

// Config defines how payload bytes are split and protected.
//
// PayloadSize is the number of application bytes each data frame can carry.
// It is intentionally transport-agnostic: a video, QR-code stream, packet
// protocol, image grid, or storage layer can choose its own capacity.
type Config struct {
	PayloadSize          int
	DataFramesPerBlock   int
	RescueFramesPerBlock int
}

// DefaultConfig returns a general-purpose profile. Projects should tune
// PayloadSize to match their transport capacity after their own headers.
func DefaultConfig() Config {
	return Config{
		PayloadSize:          1024,
		DataFramesPerBlock:   DefaultBlockSize,
		RescueFramesPerBlock: DefaultRescueCount,
	}
}

func (c Config) validate() error {
	if c.PayloadSize <= 0 {
		return fmt.Errorf("%w: payload size must be positive", ErrInvalidConfig)
	}
	if c.PayloadSize > math.MaxUint16 {
		return fmt.Errorf("%w: payload size must fit uint16", ErrInvalidConfig)
	}
	if c.DataFramesPerBlock <= 0 || c.DataFramesPerBlock > 255 {
		return fmt.Errorf("%w: data frames per block must be between 1 and 255", ErrInvalidConfig)
	}
	if c.RescueFramesPerBlock <= 0 || c.RescueFramesPerBlock > 255 {
		return fmt.Errorf("%w: rescue frames per block must be between 1 and 255", ErrInvalidConfig)
	}
	if c.DataFramesPerBlock+c.RescueFramesPerBlock > 255 {
		return fmt.Errorf("%w: data and rescue frames per block must sum to 255 or less", ErrInvalidConfig)
	}
	return nil
}
