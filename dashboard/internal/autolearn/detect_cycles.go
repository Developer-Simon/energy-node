package autolearn

import (
	"context"
	"math"
)

// LearnCyclesDetector learns the operating power and the cycle energy of one
// device from its finished cycles. Cycles of different programs, such as Eco and
// short, are split on the logarithm of their energy, and the largest program
// gives the values. It is per device and runs once a day.
var LearnCyclesDetector = Detector{
	Name:      "learn_cycles",
	Group:     "energy",
	Light:     false,
	PerDevice: true,
	Run:       learnCyclesRun,
}

const (
	// cyclesMinCount is the number of cycles from which values are learned.
	cyclesMinCount = 8
	// cyclesOtsuBins is the number of histogram bins for the program split.
	// The spec gives no number, the value is an assumption.
	cyclesOtsuBins = 16
	// cyclesMinRatio is the factor the energy medians of two programs must lie
	// apart. Otsu splits any spread, so noise within one program is rejected.
	// The value is an assumption, Eco against short cycles gives about 1.8.
	cyclesMinRatio = 1.5
	// cyclesRoundStep rounds power in W and energy in Wh to 10.
	cyclesRoundStep = 10.0
)

// learnCyclesRun returns the operating power and the cycle energy of the largest
// program of the device, and the cycle energy of the other programs.
func learnCyclesRun(ctx context.Context, in *Input, device string) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	var cs []CycleRec
	for _, f := range in.Days {
		for _, c := range f.Cycles {
			if c.Device == device && c.Wh > 0 {
				cs = append(cs, c)
			}
		}
	}
	if len(cs) < cyclesMinCount {
		return nil
	}
	programs := cyclesPrograms(cs)
	big := 0
	for i := range programs {
		if len(programs[i]) > len(programs[big]) {
			big = i
		}
	}

	var others []float64
	for i, p := range programs {
		if i != big {
			others = append(others, cyclesRound(Median(cyclesWh(p))))
		}
	}
	main := programs[big]
	n := float64(len(main))
	return []Candidate{
		{
			Detector: "learn_cycles",
			NEff:     n,
			MinNEff:  cyclesMinCount,
			Value: &Value{Key: "device/" + device + "/op_power_w", Device: device,
				Kind: ValueNumber, Unit: "W", Group: "energy",
				Num: cyclesRound(Median(cyclesOpW(main))), N: len(main)},
		},
		{
			Detector: "learn_cycles",
			NEff:     n,
			MinNEff:  cyclesMinCount,
			Value: &Value{Key: "device/" + device + "/cycle_wh", Device: device,
				Kind: ValueNumber, Unit: "Wh", Group: "energy",
				Num: cyclesRound(Median(cyclesWh(main))), N: len(main), Programs: others},
		},
	}
}

// cyclesPrograms splits the cycles into programs with Otsu on the logarithm of
// the energy. Without a split, or with medians less than cyclesMinRatio apart,
// there is one program with all cycles.
func cyclesPrograms(cs []CycleRec) [][]CycleRec {
	logs := make([]float64, len(cs))
	for i, c := range cs {
		logs[i] = math.Log(float64(c.Wh))
	}
	cut, ok := OtsuSplit(logs, cyclesOtsuBins)
	if !ok {
		return [][]CycleRec{cs}
	}
	var lo, hi []CycleRec
	for i, c := range cs {
		if logs[i] < cut {
			lo = append(lo, c)
		} else {
			hi = append(hi, c)
		}
	}
	if Median(cyclesWh(hi)) < cyclesMinRatio*Median(cyclesWh(lo)) {
		return [][]CycleRec{cs}
	}
	return [][]CycleRec{lo, hi}
}

// cyclesWh returns the energy of the cycles in Wh.
func cyclesWh(cs []CycleRec) []float64 {
	out := make([]float64, len(cs))
	for i, c := range cs {
		out[i] = float64(c.Wh)
	}
	return out
}

// cyclesOpW returns the operating power of the cycles in W.
func cyclesOpW(cs []CycleRec) []float64 {
	out := make([]float64, len(cs))
	for i, c := range cs {
		out[i] = float64(c.OpW)
	}
	return out
}

// cyclesRound rounds a power or an energy to 10 units.
func cyclesRound(v float64) float64 {
	return math.Round(v/cyclesRoundStep) * cyclesRoundStep
}
