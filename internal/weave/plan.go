package weave

import "fmt"

// Plan describes the frame budget for an input before Encode is called.
type Plan struct {
	InputBytes             int
	PayloadSize            int
	DataFramesPerBlock     int
	RescueFramesPerBlock   int
	DataFrames             int
	RescueFrames           int
	TotalFrames            int
	OverheadRatio          float64
	MaxRecoverablePerBlock int
}

// PlanFor returns the expected frame counts and overhead for the given input.
func PlanFor(inputBytes int, cfg Config) (Plan, error) {
	if err := cfg.validate(); err != nil {
		return Plan{}, err
	}
	if inputBytes < 0 || inputBytes > MaxPayloadBytes {
		return Plan{}, fmt.Errorf("%w: input must be between 0 and %d bytes", ErrInvalidConfig, MaxPayloadBytes)
	}
	dataFrames := 1
	if inputBytes > 0 {
		dataFrames = 1 + (inputBytes-1)/cfg.PayloadSize
	}
	if dataFrames > maxFrameCount {
		return Plan{}, fmt.Errorf("%w: too many data frames", ErrInvalidConfig)
	}
	blocks := (dataFrames + cfg.DataFramesPerBlock - 1) / cfg.DataFramesPerBlock
	rescueFrames := blocks * cfg.RescueFramesPerBlock
	if dataFrames+rescueFrames > maxFrameCount || int64(inputBytes)+int64(rescueFrames)*int64(cfg.PayloadSize) > maxProtectedBytes {
		return Plan{}, fmt.Errorf("%w: protected frame budget exceeded", ErrInvalidConfig)
	}
	overhead := float64(rescueFrames) / float64(dataFrames)

	return Plan{
		InputBytes:             inputBytes,
		PayloadSize:            cfg.PayloadSize,
		DataFramesPerBlock:     cfg.DataFramesPerBlock,
		RescueFramesPerBlock:   cfg.RescueFramesPerBlock,
		DataFrames:             dataFrames,
		RescueFrames:           rescueFrames,
		TotalFrames:            dataFrames + rescueFrames,
		OverheadRatio:          overhead,
		MaxRecoverablePerBlock: cfg.RescueFramesPerBlock,
	}, nil
}
