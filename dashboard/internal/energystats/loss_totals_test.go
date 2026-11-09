package energystats

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

var lossT0 = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

// lossChain is a chain that loses w watts, flow down. A negative w is a
// negative loss, a nil w (w = math.NaN()) makes the chain idle.
func lossChain(id string, w float64) energy.ChainState {
	if math.IsNaN(w) {
		return energy.ChainState{ID: id, Flow: "idle"}
	}
	return energy.ChainState{
		ID:       id,
		Flow:     "down",
		Segments: []energy.ChainSegmentState{{Available: true, LossW: w}},
	}
}

func lossTotal(t *testing.T, l *LossTotals, id string) float64 {
	t.Helper()
	return l.Totals()[id]
}

func closeTo(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func noLog(string, ...any) {}

func TestLossTotalsIntegratesLoss(t *testing.T) {
	l := NewLossTotals(filepath.Join(t.TempDir(), "loss_totals.json"), noLog)
	for i := 0; i <= 10; i++ {
		l.Observe(lossT0.Add(time.Duration(i)*10*time.Second), []energy.ChainState{lossChain("a", 36)})
	}
	if got := lossTotal(t, l, "a"); !closeTo(got, 0.001) {
		t.Errorf("total = %v kWh, want 0.001 (36 W over 100 s)", got)
	}

	first := NewLossTotals(filepath.Join(t.TempDir(), "x.json"), noLog)
	first.Observe(lossT0, []energy.ChainState{lossChain("a", 36)})
	if got := lossTotal(t, first, "a"); got != 0 {
		t.Errorf("first observation counted: %v", got)
	}
}

func TestLossTotalsSkipsGapsNullAndNegative(t *testing.T) {
	l := NewLossTotals(filepath.Join(t.TempDir(), "loss_totals.json"), noLog)
	at := func(s int) time.Time { return lossT0.Add(time.Duration(s) * time.Second) }

	// Gap of 61 s does not count, the next 10 s interval does.
	l.Observe(at(0), []energy.ChainState{lossChain("a", 36)})
	l.Observe(at(61), []energy.ChainState{lossChain("a", 36)})
	if got := lossTotal(t, l, "a"); got != 0 {
		t.Errorf("gap counted: %v", got)
	}
	l.Observe(at(71), []energy.ChainState{lossChain("a", 36)})
	if got := lossTotal(t, l, "a"); !closeTo(got, 0.0001) {
		t.Errorf("after gap = %v, want 0.0001", got)
	}

	// Idle at 81 s and an unavailable segment at 91 s: no interval counts.
	l.Observe(at(81), []energy.ChainState{lossChain("a", math.NaN())})
	l.Observe(at(91), []energy.ChainState{lossChain("a", 36)})
	l.Observe(at(101), []energy.ChainState{{ID: "a", Flow: "down", Segments: []energy.ChainSegmentState{{Available: false}}}})
	l.Observe(at(111), []energy.ChainState{lossChain("a", 36)})
	if got := lossTotal(t, l, "a"); !closeTo(got, 0.0001) {
		t.Errorf("idle or unavailable counted: %v, want 0.0001", got)
	}

	// A negative loss counts as 0.
	n := NewLossTotals(filepath.Join(t.TempDir(), "neg.json"), noLog)
	n.Observe(at(0), []energy.ChainState{lossChain("b", -5)})
	n.Observe(at(10), []energy.ChainState{lossChain("b", -5)})
	if got := lossTotal(t, n, "b"); got != 0 {
		t.Errorf("negative loss counted: %v", got)
	}
}

func TestLossTotalsDropsDeletedChains(t *testing.T) {
	l := NewLossTotals(filepath.Join(t.TempDir(), "loss_totals.json"), noLog)
	at := func(s int) time.Time { return lossT0.Add(time.Duration(s) * time.Second) }
	l.Observe(at(0), []energy.ChainState{lossChain("a", 36)})
	l.Observe(at(10), []energy.ChainState{lossChain("a", 36)})
	l.Observe(at(20), []energy.ChainState{})
	if _, ok := l.Totals()["a"]; ok {
		t.Fatalf("deleted chain still has a total")
	}

	l.Observe(at(30), []energy.ChainState{lossChain("a", 36)})
	l.Observe(at(40), []energy.ChainState{lossChain("a", 36)})
	if got := lossTotal(t, l, "a"); !closeTo(got, 0.0001) {
		t.Errorf("re-created chain = %v, want 0 start plus 0.0001", got)
	}
}

func TestLossTotalsPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chains", "loss_totals.json")
	l := NewLossTotals(path, noLog)
	at := func(d time.Duration) time.Time { return lossT0.Add(d) }

	l.Observe(at(0), []energy.ChainState{lossChain("a", 36)})
	l.Observe(at(10*time.Second), []energy.ChainState{lossChain("a", 36)})
	// The observation at 59 min is more than 60 s after the previous one and
	// adds nothing. The save check is due at 60 min after the first observation.
	l.Observe(at(59*time.Minute), []energy.ChainState{lossChain("a", 36)})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file written at 59 min: %v", err)
	}
	l.Observe(at(60*time.Minute), []energy.ChainState{lossChain("a", 36)})
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file missing at 60 min: %v", err)
	}

	l.Observe(at(60*time.Minute+10*time.Second), []energy.ChainState{lossChain("a", 36)})
	l.Close()
	want := l.Totals()

	reloaded := NewLossTotals(path, noLog)
	got := reloaded.Totals()
	if len(got) != len(want) || got["a"] != want["a"] {
		t.Errorf("reloaded = %v, want %v", got, want)
	}

	var logs int
	count := func(string, ...any) { logs++ }
	if err := os.WriteFile(path, []byte("{kaputt"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := NewLossTotals(path, count)
	if len(broken.Totals()) != 0 {
		t.Errorf("broken file not empty: %v", broken.Totals())
	}
	if logs != 1 {
		t.Errorf("log calls = %d, want 1", logs)
	}
}
