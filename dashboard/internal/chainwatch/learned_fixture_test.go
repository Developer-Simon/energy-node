package chainwatch

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

// TestLoadDemoFixture reads the learned table that the demo seed writes
// (demo/lib/seed_chain_stats.py, shared with its Python test) and checks that
// the format loads without a warning and gives the demo curve.
func TestLoadDemoFixture(t *testing.T) {
	var warnings []string
	logf := func(format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}
	tb := Load(filepath.Join("testdata", "demo_learned.json"), logf)
	if len(warnings) > 0 {
		t.Fatalf("Load logged %v", warnings)
	}

	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	plug, inverter, battery := "speicher_plug_power", "speicher_umrichter_power", "speicher_akku_power"
	for _, pair := range []energystats.ChainPair{
		{Source: plug, Target: inverter},
		{Source: inverter, Target: plug},
		{Source: inverter, Target: battery},
		{Source: battery, Target: inverter},
	} {
		if !tb.PairLearned(pair.Source, pair.Target, now) {
			t.Errorf("%s -> %s not learned", pair.Source, pair.Target)
		}
		if seg := tb.Segment(pair, 300, now); !seg.Learned || seg.LossW <= 0 {
			t.Errorf("%s -> %s class 6: %+v", pair.Source, pair.Target, seg)
		}
	}

	curve := tb.Curve([]string{plug, inverter, battery})
	if len(curve.Down) != 12 {
		t.Fatalf("Down has %d points, want 12", len(curve.Down))
	}
	best := curve.BestDown
	if best == nil {
		t.Fatal("BestDown is nil")
	}
	if best.FromW > 300 || best.ToW < 350 {
		t.Errorf("BestDown = %v..%v W, want it to contain class 6 (300..350 W)", best.FromW, best.ToW)
	}
	if best.FromW <= 50 || best.ToW >= 600 {
		t.Errorf("BestDown = %v..%v W, want the peak in the middle of the curve", best.FromW, best.ToW)
	}
}
