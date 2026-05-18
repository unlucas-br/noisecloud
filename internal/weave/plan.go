package weave

import "math"

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
	dataFrames := int(math.Ceil(float64(inputBytes) / float64(cfg.PayloadSize)))
	if dataFrames == 0 {
		dataFrames = 1
	}
	blocks := (dataFrames + cfg.DataFramesPerBlock - 1) / cfg.DataFramesPerBlock
	rescueFrames := blocks * cfg.RescueFramesPerBlock
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
