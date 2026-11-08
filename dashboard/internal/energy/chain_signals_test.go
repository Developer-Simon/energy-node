package energy

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// signalInsight is a ChainInsight whose Curve answers fixed ranges and
// records the points it was asked for.
type signalInsight struct {
	curve  ChainCurve
	points []string
}

func (s *signalInsight) Segment(source, target string, countingW float64) ChainSegmentInsight {
	return ChainSegmentInsight{}
}
func (s *signalInsight) PairLearned(a, b string) bool           { return true }
func (s *signalInsight) Findings(chainID string) []ChainFinding { return nil }
func (s *signalInsight) Curve(points []string) ChainCurve {
	s.points = append([]string{}, points...)
	return s.curve
}

func signalPoint(id string, value float64) ChainPointState {
	return ChainPointState{EntityID: id, Label: id, Value: value, HasValue: true, Freshness: "fresh"}
}

func signalChain(flow string, segments ...ChainSegmentState) ChainState {
	return ChainState{
		ID:       "kette",
		Label:    "Kette",
		Flow:     flow,
		Points:   []ChainPointState{signalPoint("a", 100), signalPoint("b", 91), signalPoint("c", 85)},
		Segments: segments,
	}
}

func f64(v float64) *float64 { return &v }

func TestBuildChainSignalsSumsLossAndEfficiency(t *testing.T) {
	chain := signalChain("down",
		ChainSegmentState{From: "a", To: "b", Available: true, LossW: 9, Efficiency: f64(0.97)},
		ChainSegmentState{From: "b", To: "c", Available: true, LossW: 6, Efficiency: f64(0.98)},
	)
	got := BuildChainSignals([]ChainState{chain}, nil, time.Unix(0, 0)).Chains["kette"]
	if got.LossW == nil || *got.LossW != 15 {
		t.Fatalf("LossW = %v, want 15", got.LossW)
	}
	if got.EfficiencyPct == nil || *got.EfficiencyPct != 95.1 {
		t.Fatalf("EfficiencyPct = %v, want 95.1", got.EfficiencyPct)
	}

	unavailable := signalChain("down",
		ChainSegmentState{From: "a", To: "b", Available: true, LossW: 9, Efficiency: f64(0.97)},
		ChainSegmentState{From: "b", To: "c", Available: false},
	)
	got = BuildChainSignals([]ChainState{unavailable}, nil, time.Unix(0, 0)).Chains["kette"]
	if got.LossW != nil || got.EfficiencyPct != nil {
		t.Errorf("unavailable segment: LossW = %v, EfficiencyPct = %v, want nil, nil", got.LossW, got.EfficiencyPct)
	}

	idle := signalChain("idle",
		ChainSegmentState{From: "a", To: "b", Available: true, LossW: 9, Efficiency: f64(0.97)},
	)
	got = BuildChainSignals([]ChainState{idle}, nil, time.Unix(0, 0)).Chains["kette"]
	if got.LossW != nil || got.EfficiencyPct != nil {
		t.Errorf("idle flow: LossW = %v, EfficiencyPct = %v, want nil, nil", got.LossW, got.EfficiencyPct)
	}

	noEfficiency := signalChain("down",
		ChainSegmentState{From: "a", To: "b", Available: true, LossW: 9, Efficiency: f64(0.97)},
		ChainSegmentState{From: "b", To: "c", Available: true, LossW: 6},
	)
	got = BuildChainSignals([]ChainState{noEfficiency}, nil, time.Unix(0, 0)).Chains["kette"]
	if got.LossW == nil || *got.LossW != 15 {
		t.Errorf("nil efficiency: LossW = %v, want 15", got.LossW)
	}
	if got.EfficiencyPct != nil {
		t.Errorf("nil efficiency: EfficiencyPct = %v, want nil", *got.EfficiencyPct)
	}
}

func TestBuildChainSignalsStandby(t *testing.T) {
	standby := ChainState{
		ID:   "kette",
		Flow: "idle",
		Points: []ChainPointState{
			signalPoint("a", 12),
			signalPoint("b", 3),
			signalPoint("c", 2),
		},
	}
	got := BuildChainSignals([]ChainState{standby}, nil, time.Unix(0, 0)).Chains["kette"]
	if got.StandbyW == nil || *got.StandbyW != 12 {
		t.Fatalf("StandbyW = %v, want 12", got.StandbyW)
	}

	active := standby
	active.Points = []ChainPointState{signalPoint("a", 12), signalPoint("b", 40), signalPoint("c", 2)}
	got = BuildChainSignals([]ChainState{active}, nil, time.Unix(0, 0)).Chains["kette"]
	if got.StandbyW != nil {
		t.Errorf("StandbyW = %v, want nil", *got.StandbyW)
	}
}

func TestBuildChainSignalsFindingsAndFlags(t *testing.T) {
	since := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	chain := signalChain("down")
	chain.Label = "Wechselrichter"
	chain.SubstituteFrom = "zaehler"
	chain.NoCurrentValues = true
	chain.Findings = []ChainFinding{{Kind: "frozen", ChainID: "kette", From: "a", To: "b", Since: since}}

	got := BuildChainSignals([]ChainState{chain}, nil, time.Unix(0, 0)).Chains["kette"]
	if got.Label != "Wechselrichter" || got.Flow != "down" {
		t.Errorf("Label, Flow = %q, %q", got.Label, got.Flow)
	}
	if !got.Substitute || !got.NoCurrentValues {
		t.Errorf("Substitute = %v, NoCurrentValues = %v, want true, true", got.Substitute, got.NoCurrentValues)
	}
	if len(got.Findings) != 1 || got.Findings[0].Since != 1794045600 || got.Findings[0].From != "a" || got.Findings[0].To != "b" || got.Findings[0].Kind != "frozen" {
		t.Errorf("Findings = %+v", got.Findings)
	}

	empty := BuildChainSignals([]ChainState{signalChain("down")}, nil, time.Unix(0, 0))
	raw, err := json.Marshal(empty)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"findings":[]`) {
		t.Errorf("findings without entries must marshal as [], got %s", raw)
	}
}

func TestBuildChainSignalsBestRange(t *testing.T) {
	insight := &signalInsight{curve: ChainCurve{BestDown: &ChainPowerRange{FromW: 250, ToW: 500}}}
	chain := signalChain("down")
	got := BuildChainSignals([]ChainState{chain}, insight, time.Unix(0, 0)).Chains["kette"]
	if got.BestDown == nil || got.BestDown.FromW != 250 || got.BestDown.ToW != 500 {
		t.Errorf("BestDown = %+v, want 250 to 500", got.BestDown)
	}
	if got.BestUp != nil {
		t.Errorf("BestUp = %+v, want nil", got.BestUp)
	}
	if len(insight.points) != 3 || insight.points[0] != "a" || insight.points[1] != "b" || insight.points[2] != "c" {
		t.Errorf("Curve called with %v, want [a b c]", insight.points)
	}

	without := BuildChainSignals([]ChainState{chain}, nil, time.Unix(0, 0)).Chains["kette"]
	if without.BestDown != nil || without.BestUp != nil {
		t.Errorf("without insight: BestDown = %+v, BestUp = %+v, want nil", without.BestDown, without.BestUp)
	}
}

func TestBuildChainSignalsRoundsValues(t *testing.T) {
	chain := signalChain("down",
		ChainSegmentState{From: "a", To: "b", Available: true, LossW: 9.04999, Efficiency: f64(1)},
	)
	standby := ChainState{ID: "standby", Flow: "idle", Points: []ChainPointState{signalPoint("a", 12.345), signalPoint("b", 1), signalPoint("c", 1)}}
	signals := BuildChainSignals([]ChainState{chain, standby}, nil, time.Unix(0, 0))
	if got := signals.Chains["kette"].LossW; got == nil || *got != 9.0 {
		t.Errorf("LossW = %v, want 9.0", got)
	}
	if got := signals.Chains["standby"].StandbyW; got == nil || *got != 12.3 {
		t.Errorf("StandbyW = %v, want 12.3", got)
	}
}

func TestChainLossWMatchesPublishedLoss(t *testing.T) {
	cases := map[string]ChainState{
		"sums": signalChain("down",
			ChainSegmentState{From: "a", To: "b", Available: true, LossW: 9, Efficiency: f64(0.97)},
			ChainSegmentState{From: "b", To: "c", Available: true, LossW: 6, Efficiency: f64(0.98)},
		),
		"unavailable": signalChain("down",
			ChainSegmentState{From: "a", To: "b", Available: true, LossW: 9, Efficiency: f64(0.97)},
			ChainSegmentState{From: "b", To: "c", Available: false},
		),
		"idle": signalChain("idle",
			ChainSegmentState{From: "a", To: "b", Available: true, LossW: 9, Efficiency: f64(0.97)},
		),
		"no efficiency": signalChain("down",
			ChainSegmentState{From: "a", To: "b", Available: true, LossW: 9},
		),
		"no segments": signalChain("down"),
	}
	for name, chain := range cases {
		loss, ok := ChainLossW(chain)
		published := BuildChainSignals([]ChainState{chain}, nil, time.Unix(0, 0)).Chains["kette"].LossW
		if ok != (published != nil) {
			t.Errorf("%s: ok = %v, published LossW = %v", name, ok, published)
			continue
		}
		if ok && loss != *published {
			t.Errorf("%s: loss = %v, published LossW = %v", name, loss, *published)
		}
	}
}

func TestChainSignalsLossTotals(t *testing.T) {
	signals := ChainSignals{Chains: map[string]ChainSignal{"a": {}, "b": {}}}
	signals.SetLossTotals(map[string]float64{"a": 1.23456})
	if got := signals.Chains["a"].LossKWhTotal; got != 1.235 {
		t.Errorf("a LossKWhTotal = %v, want 1.235", got)
	}
	if got := signals.Chains["b"].LossKWhTotal; got != 0 {
		t.Errorf("b LossKWhTotal = %v, want 0", got)
	}
	data, err := json.Marshal(signals.Chains["b"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"loss_kwh_total":0`) {
		t.Errorf("JSON = %s, want loss_kwh_total", data)
	}
}
