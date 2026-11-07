package energy

import (
	"math"
	"time"
)

// ChainSignalFinding is one finding of a chain as the automations see it.
type ChainSignalFinding struct {
	Kind  string `json:"kind"`
	From  string `json:"from"`
	To    string `json:"to,omitempty"`
	Since int64  `json:"since"` // Unix-Sekunden
}

// ChainSignal is the state of one measurement chain for the automations.
type ChainSignal struct {
	Label           string               `json:"label"`
	Flow            string               `json:"flow"`
	LossW           *float64             `json:"loss_w"`
	EfficiencyPct   *float64             `json:"efficiency_pct"`
	StandbyW        *float64             `json:"standby_w"`
	Substitute      bool                 `json:"substitute"`
	NoCurrentValues bool                 `json:"no_current_values"`
	Findings        []ChainSignalFinding `json:"findings"`
	BestDown        *ChainPowerRange     `json:"best_down"`
	BestUp          *ChainPowerRange     `json:"best_up"`
}

// ChainSignals is the retained payload on the chains topic, keyed by chain ID.
type ChainSignals struct {
	At     int64                  `json:"at"`
	Chains map[string]ChainSignal `json:"chains"`
}

// BuildChainSignals derives the payload of the chains topic from the chain
// states, the learned model supplies the best ranges when attached.
func BuildChainSignals(chains []ChainState, insight ChainInsight, at time.Time) ChainSignals {
	out := ChainSignals{At: at.Unix(), Chains: make(map[string]ChainSignal, len(chains))}
	for _, chain := range chains {
		signal := ChainSignal{
			Label:           chain.Label,
			Flow:            chain.Flow,
			Substitute:      chain.SubstituteFrom != "",
			NoCurrentValues: chain.NoCurrentValues,
			Findings:        make([]ChainSignalFinding, 0, len(chain.Findings)),
		}
		signal.LossW, signal.EfficiencyPct = chainLossAndEfficiency(chain)
		if len(chain.Points) > 0 {
			if w, ok := ChainStandbyW(chain); ok {
				signal.StandbyW = f64Ptr(roundOneDecimal(w))
			}
		}
		for _, f := range chain.Findings {
			signal.Findings = append(signal.Findings, ChainSignalFinding{
				Kind: f.Kind, From: f.From, To: f.To, Since: f.Since.Unix(),
			})
		}
		if insight != nil {
			ids := make([]string, 0, len(chain.Points))
			for _, p := range chain.Points {
				ids = append(ids, p.EntityID)
			}
			curve := insight.Curve(ids)
			signal.BestDown = curve.BestDown
			signal.BestUp = curve.BestUp
		}
		out.Chains[chain.ID] = signal
	}
	return out
}

// chainLossAndEfficiency sums the loss of the segments and multiplies their
// efficiencies. Both stay nil when the flow is idle, when there are no
// segments, when one segment is not available, and the efficiency also when
// one segment has none.
func chainLossAndEfficiency(chain ChainState) (*float64, *float64) {
	if chain.Flow == "idle" || len(chain.Segments) == 0 {
		return nil, nil
	}
	loss := 0.0
	eff := 1.0
	effKnown := true
	for _, seg := range chain.Segments {
		if !seg.Available {
			return nil, nil
		}
		loss += seg.LossW
		if seg.Efficiency == nil {
			effKnown = false
		} else {
			eff *= *seg.Efficiency
		}
	}
	var effPct *float64
	if effKnown {
		effPct = f64Ptr(roundOneDecimal(eff * 100))
	}
	return f64Ptr(roundOneDecimal(loss)), effPct
}

// ChainStandbyW reports the power of the first point when the chain is in
// standby: every point is fresh, the first one reads at least ChainIdleW and
// every other one stays below ChainMinInputW.
func ChainStandbyW(chain ChainState) (float64, bool) {
	source := chain.Points[0]
	if !source.HasValue || source.Freshness != "fresh" {
		return 0, false
	}
	w := math.Abs(source.Value)
	if w < ChainIdleW {
		return 0, false
	}
	for _, p := range chain.Points[1:] {
		if p.Freshness != "fresh" || math.Abs(p.Value) >= ChainMinInputW {
			return 0, false
		}
	}
	return w, true
}

func roundOneDecimal(v float64) float64 {
	return math.Round(v*10) / 10
}

func f64Ptr(v float64) *float64 { return &v }
