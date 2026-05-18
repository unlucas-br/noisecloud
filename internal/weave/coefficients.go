package weave

import "sync"

type coefficientKey struct {
	dataFrames   int
	rescueFrames int
}

var coefficientCache sync.Map

type coefficientTable struct {
	dataFrames int
	values     []byte
}

func newCoefficientTable(cfg Config) coefficientTable {
	key := coefficientKey{
		dataFrames:   cfg.DataFramesPerBlock,
		rescueFrames: cfg.RescueFramesPerBlock,
	}
	if cached, ok := coefficientCache.Load(key); ok {
		return cached.(coefficientTable)
	}

	table := coefficientTable{
		dataFrames: cfg.DataFramesPerBlock,
		values:     make([]byte, cfg.DataFramesPerBlock*cfg.RescueFramesPerBlock),
	}
	for rescueIndex := 0; rescueIndex < cfg.RescueFramesPerBlock; rescueIndex++ {
		for dataSlot := 0; dataSlot < cfg.DataFramesPerBlock; dataSlot++ {
			table.values[rescueIndex*cfg.DataFramesPerBlock+dataSlot] = rescueCoefficientRaw(rescueIndex, dataSlot, cfg.RescueFramesPerBlock)
		}
	}

	actual, _ := coefficientCache.LoadOrStore(key, table)
	return actual.(coefficientTable)
}

func (t coefficientTable) at(rescueIndex int, dataSlot int) byte {
	return t.values[rescueIndex*t.dataFrames+dataSlot]
}

func rescueCoefficientRaw(rescueIndex int, dataSlot int, rescueFrames int) byte {
	// Cauchy coefficient: 1 / (x_i + y_j), with disjoint x/y sets.
	// In GF(256), addition is XOR.
	x := byte(rescueIndex + 1)
	y := byte(rescueFrames + dataSlot + 1)
	return gfInv(x ^ y)
}
