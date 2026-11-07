package chainwatch

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

func learnSamples(tb *Table, pair energystats.ChainPair, countingW float64, n int, sourceW, targetW float64, at time.Time) {
	for i := 0; i < n; i++ {
		tb.Learn(pair, countingW, sourceW, targetW, at)
	}
}

func tablePairs(tb *Table) map[energystats.ChainPair]bool {
	out := map[energystats.ChainPair]bool{}
	for pair := range tb.pairs {
		out[pair] = true
	}
	return out
}

func TestClassOf(t *testing.T) {
	cases := map[float64]int{0: 0, 49: 0, 50: 1, -300: 6, 50000: MaxClass}
	for w, want := range cases {
		if got := ClassOf(w); got != want {
			t.Errorf("ClassOf(%v) = %d, want %d", w, got, want)
		}
	}
}

func TestLearnConvergesAndForgets(t *testing.T) {
	tb := NewTable()
	pair := energystats.ChainPair{Source: "a", Target: "b"}
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

	learnSamples(tb, pair, 100, 100, 300, 290, now)
	seg := tb.Segment(pair, 100, now)
	if math.Abs(seg.LossW-10) > 1e-9 {
		t.Fatalf("LossW = %v, want 10", seg.LossW)
	}
	if seg.DeviationW > 0.5 {
		t.Fatalf("DeviationW = %v, want near 0", seg.DeviationW)
	}

	learnSamples(tb, pair, 100, 3*LearnWindow, 300, 280, now)
	seg = tb.Segment(pair, 100, now)
	if math.Abs(seg.LossW-20) > 0.5 {
		t.Fatalf("LossW after change = %v, want near 20", seg.LossW)
	}
	if n := tb.pairs[pair].Classes[ClassOf(100)].N; n != LearnWindow {
		t.Fatalf("N = %d, want %d", n, LearnWindow)
	}
}

func TestSegmentLearnedRules(t *testing.T) {
	pair := energystats.ChainPair{Source: "a", Target: "b"}
	first := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	week := first.Add(LearnedAfter)
	sixDays := first.Add(LearnedAfter - time.Hour*24)

	check := func(name string, tb *Table, countingW float64, at time.Time, want bool) {
		t.Helper()
		if got := tb.Segment(pair, countingW, at).Learned; got != want {
			t.Errorf("%s: Learned = %v, want %v", name, got, want)
		}
	}

	tb := NewTable()
	learnSamples(tb, pair, 100, LearnedSamples-1, 300, 290, first)
	check("1079 fresh", tb, 100, first, false)
	learnSamples(tb, pair, 100, 1, 300, 290, first)
	check("1080 fresh", tb, 100, first, true)

	tb = NewTable()
	learnSamples(tb, pair, 100, LearnedMinSamples, 300, 290, first)
	check("60 six days", tb, 100, sixDays, false)
	check("60 seven days", tb, 100, week, true)

	tb = NewTable()
	learnSamples(tb, pair, 100, LearnedMinSamples-1, 300, 290, first)
	check("59 seven days", tb, 100, week, false)

	tb = NewTable()
	learnSamples(tb, pair, 100, LearnedMinSamples, 300, 290, first)
	check("unseen class", tb, 1000, week, false)

	tb = NewTable()
	future := first.Add(time.Hour)
	learnSamples(tb, pair, 100, LearnedMinSamples, 300, 290, future)
	check("future FirstSeen", tb, 100, first, false)
}

func TestDirectionsLearnApart(t *testing.T) {
	tb := NewTable()
	ab := energystats.ChainPair{Source: "a", Target: "b"}
	ba := energystats.ChainPair{Source: "b", Target: "a"}
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

	learnSamples(tb, ab, 100, 10, 300, 290, now)
	learnSamples(tb, ba, 100, 10, 50, 40, now)

	if got := tb.Segment(ab, 100, now).LossW; math.Abs(got-10) > 1e-9 {
		t.Errorf("a->b LossW = %v, want 10", got)
	}
	if got := tb.Segment(ba, 100, now).LossW; math.Abs(got-10) > 1e-9 {
		t.Errorf("b->a LossW = %v, want 10", got)
	}
	if n := len(tb.pairs); n != 2 {
		t.Errorf("pairs = %d, want 2", n)
	}
	if tb.pairs[ab].Classes[ClassOf(100)].SourceW == tb.pairs[ba].Classes[ClassOf(100)].SourceW {
		t.Error("directions share one bin")
	}
}

func TestPairLearnedLooksAtEitherDirection(t *testing.T) {
	tb := NewTable()
	ba := energystats.ChainPair{Source: "b", Target: "a"}
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

	if tb.PairLearned("a", "b", now) {
		t.Fatal("empty table reports learned")
	}
	learnSamples(tb, ba, 100, LearnedSamples, 300, 290, now)
	if !tb.PairLearned("a", "b", now) {
		t.Error("PairLearned(a,b) false, want true from b->a data")
	}
	if !tb.PairLearned("b", "a", now) {
		t.Error("PairLearned(b,a) false, want true")
	}
}

func TestPruneKeepsAdjacentPairsOnly(t *testing.T) {
	tb := NewTable()
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	ab := energystats.ChainPair{Source: "a", Target: "b"}
	ba := energystats.ChainPair{Source: "b", Target: "a"}
	bc := energystats.ChainPair{Source: "b", Target: "c"}
	cb := energystats.ChainPair{Source: "c", Target: "b"}
	for _, p := range []energystats.ChainPair{ab, ba, bc, cb} {
		learnSamples(tb, p, 100, 1, 300, 290, now)
	}

	tb.Prune(map[string]energy.Chain{"x": {Points: []string{"a", "b", "c"}}})
	want := map[energystats.ChainPair]bool{ab: true, ba: true, bc: true, cb: true}
	if got := tablePairs(tb); !reflect.DeepEqual(got, want) {
		t.Fatalf("after [a,b,c]: %v, want %v", got, want)
	}

	tb.Prune(map[string]energy.Chain{"x": {Points: []string{"a", "c", "b"}}})
	want = map[energystats.ChainPair]bool{bc: true, cb: true}
	if got := tablePairs(tb); !reflect.DeepEqual(got, want) {
		t.Fatalf("after [a,c,b]: %v, want %v", got, want)
	}

	tb.Prune(map[string]energy.Chain{})
	if got := tablePairs(tb); len(got) != 0 {
		t.Fatalf("after empty map: %v, want none", got)
	}
}

func TestPruneIgnoresCountingChange(t *testing.T) {
	tb := NewTable()
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	ab := energystats.ChainPair{Source: "a", Target: "b"}
	learnSamples(tb, ab, 100, 1, 300, 290, now)

	tb.Prune(map[string]energy.Chain{"x": {Points: []string{"a", "b"}, Counting: "a"}})
	tb.Prune(map[string]energy.Chain{"x": {Points: []string{"a", "b"}, Counting: "b"}})
	if _, ok := tb.pairs[ab]; !ok {
		t.Fatal("counting change alone dropped the pair")
	}
}

func TestResetDropsBothDirections(t *testing.T) {
	tb := NewTable()
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	ab := energystats.ChainPair{Source: "a", Target: "b"}
	ba := energystats.ChainPair{Source: "b", Target: "a"}
	learnSamples(tb, ab, 100, 1, 300, 290, now)
	learnSamples(tb, ba, 100, 1, 300, 290, now)

	tb.Reset(ab)
	if len(tb.pairs) != 0 {
		t.Fatalf("pairs after Reset = %v, want none", tablePairs(tb))
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "learned.json")
	first := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	tb := NewTable()
	tb.pairs[energystats.ChainPair{Source: "a", Target: "b"}] = &pairEntry{
		FirstSeen:   first,
		ScaleSource: 1,
		ScaleTarget: 1,
		Agree:       120,
		Classes: map[int]*bin{
			5: {N: 1080, SourceW: 290.123, TargetW: 280.456, DevW: 1.234},
			2: {N: 3, SourceW: 100.5, TargetW: 99.25, DevW: 0.5},
		},
	}
	tb.pairs[energystats.ChainPair{Source: "b", Target: "a"}] = &pairEntry{
		FirstSeen:    first.Add(time.Hour),
		ScaleSource:  1.5,
		ScaleTarget:  1,
		InvertSource: true,
		Opposite:     2,
		Classes:      map[int]*bin{},
	}

	if err := tb.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	first1, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded := Load(path, func(string, ...any) { t.Error("unexpected log") })
	if !reflect.DeepEqual(loaded.pairs, tb.pairs) {
		t.Fatalf("loaded pairs differ:\n got %+v\nwant %+v", loaded.pairs, tb.pairs)
	}
	if err := loaded.Save(path); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first1) != string(second) {
		t.Fatalf("second Save differs:\n%s\n%s", first1, second)
	}
}

func TestLoadMissingFileGivesEmptyTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "learned.json")
	tb := Load(path, func(string, ...any) { t.Error("unexpected log") })
	if tb == nil || len(tb.pairs) != 0 {
		t.Fatalf("Load missing = %v, want empty table", tb)
	}
}

func TestLoadCorruptFileMovesItAside(t *testing.T) {
	cases := map[string]string{
		"broken json":     "{not json",
		"unknown version": `{"version":2,"pairs":[]}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "learned.json")
			if err := os.WriteFile(path+".corrupt", []byte("old backup"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			logged := 0
			tb := Load(path, func(string, ...any) { logged++ })
			if len(tb.pairs) != 0 {
				t.Fatal("corrupt file gave pairs")
			}
			if logged == 0 {
				t.Error("no log line for corrupt file")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("corrupt file still in place: %v", err)
			}
			got, err := os.ReadFile(path + ".corrupt")
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != content {
				t.Errorf("backup = %q, want %q", got, content)
			}
		})
	}
}
