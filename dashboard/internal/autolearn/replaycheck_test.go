package autolearn

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

func replayCheckRule() Rule {
	return Rule{ID: "r1", Enabled: true, Conditions: []Condition{{
		Type: "balance_threshold",
		Raw: json.RawMessage(`{"type":"balance_threshold","field":"grid_export","comparison":"above",` +
			`"threshold":500,"hysteresis":0,"hold_seconds":0}`),
	}}}
}

func replayCheckTarget() ReplayTarget {
	return ReplayTarget{RuleID: "r1", Device: "plug.x", OpW: 1000}
}

// stretchedInput has fourteen days of quarter hours and no minute reader, so
// every replayed day is stretched.
func stretchedInput() *Input {
	loc := time.UTC
	in := &Input{Now: replayTestNow(), Loc: loc}
	for d := 8; d <= 21; d++ {
		in.Days = append(in.Days, quarterDay(time.Date(2026, 11, d, 0, 0, 0, 0, loc).Format(dayLayout),
			func(int) float32 { return 300 }))
	}
	return in
}

func TestEvaluateReplayNotReplayableIsError(t *testing.T) {
	rule := replayCheckRule()
	rule.Conditions = []Condition{{Type: "chain_value", Raw: json.RawMessage(
		`{"type":"chain_value","chain_id":"c","field":"x","comparison":"above","value":1}`)}}
	v, benefit, err := EvaluateReplay(context.Background(), stretchedInput(),
		ReplayCheck{Rule: rule, Target: replayCheckTarget()}, 14, time.UTC, sun.Location{})
	if !errors.Is(err, ErrNotReplayable) {
		t.Fatalf("err = %v, want ErrNotReplayable", err)
	}
	if v != nil || benefit != (Benefit{}) {
		t.Fatalf("verdict = %v, benefit = %v, want none", v, benefit)
	}
}

func TestEvaluateReplayStretchedDaysAreEstimate(t *testing.T) {
	v, _, err := EvaluateReplay(context.Background(), stretchedInput(),
		ReplayCheck{Rule: replayCheckRule(), Target: replayCheckTarget()}, 14, time.UTC, sun.Location{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if v == nil {
		t.Fatal("verdict = nil, want a verdict")
	}
	if !v.Estimate || !v.Base.Estimate || !v.Alt.Estimate {
		t.Fatalf("estimate flags = %v/%v/%v, want all true", v.Estimate, v.Base.Estimate, v.Alt.Estimate)
	}
	if len(v.Base.On) != 14*1440 || len(v.Alt.On) != len(v.Base.On) {
		t.Fatalf("len base/alt = %d/%d, want %d", len(v.Base.On), len(v.Alt.On), 14*1440)
	}
}

func TestEvaluateReplayBaseNeverFires(t *testing.T) {
	v, _, err := EvaluateReplay(context.Background(), stretchedInput(),
		ReplayCheck{Rule: replayCheckRule(), Target: replayCheckTarget()}, 14, time.UTC, sun.Location{})
	if err != nil || v == nil {
		t.Fatalf("verdict = %v, err = %v", v, err)
	}
	if len(v.Base.Fires) != 0 {
		t.Fatalf("base fires = %v, want none, the base is the real situation", v.Base.Fires)
	}
}

func TestEvaluateReplayPassesReplaysToBenefit(t *testing.T) {
	var gotAlt, gotBase int
	chk := ReplayCheck{Rule: replayCheckRule(), Target: replayCheckTarget(),
		Benefit: func(alt, base ReplayResult) Benefit {
			gotAlt, gotBase = len(alt.On), len(base.On)
			return Benefit{Unit: "kwh_week", Value: 0.3}
		}}
	_, benefit, err := EvaluateReplay(context.Background(), stretchedInput(), chk, 14, time.UTC, sun.Location{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if benefit != (Benefit{Unit: "kwh_week", Value: 0.3}) {
		t.Fatalf("benefit = %v", benefit)
	}
	if gotAlt != 14*1440 || gotBase != 14*1440 {
		t.Fatalf("benefit got %d/%d minutes, want %d", gotAlt, gotBase, 14*1440)
	}
}

func replayCand(kwh float64, base, alt ReplayResult) Candidate {
	c := gateCand("g", "x", kwh, 2, 2, true)
	c.Replay = &ReplayVerdict{Base: base, Alt: alt}
	return c
}

func TestGateDropsReplayWithMoreGridThanExport(t *testing.T) {
	c := replayCand(1, ReplayResult{GridWh: 0, ExportWh: 1000}, ReplayResult{GridWh: 1500, ExportWh: 0})
	if out, _ := Gate([]Candidate{c}, Doc{}, nil); len(out) != 0 {
		t.Fatalf("kept %v, want dropped, the suggestion draws more from the grid than it uses", gateIDs(out))
	}
}

func TestGateKeepsReplayWhenExportCoversGrid(t *testing.T) {
	c := replayCand(1, ReplayResult{GridWh: 0, ExportWh: 1000}, ReplayResult{GridWh: 500, ExportWh: 0})
	if out, _ := Gate([]Candidate{c}, Doc{}, nil); len(out) != 1 {
		t.Fatalf("kept %v, want one suggestion", gateIDs(out))
	}
}

func TestGateDropsReplayBelowBenefitMinimum(t *testing.T) {
	c := replayCand(0.3, ReplayResult{}, ReplayResult{})
	if out, _ := Gate([]Candidate{c}, Doc{}, nil); len(out) != 0 {
		t.Fatalf("kept %v, want dropped at 0,3 kWh per week", gateIDs(out))
	}
}

func TestGateCapsReplayEstimateAtEstimate(t *testing.T) {
	c := replayCand(1, ReplayResult{}, ReplayResult{})
	c.Replay.Estimate = true
	out, _ := Gate([]Candidate{c}, Doc{}, nil)
	if len(out) != 1 || out[0].Confidence != "estimate" {
		t.Fatalf("out = %+v, want one finding with confidence estimate", out)
	}
}
