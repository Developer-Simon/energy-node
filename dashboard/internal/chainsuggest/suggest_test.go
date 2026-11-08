package chainsuggest

import (
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

// ramp schwankt ueber den Tag, damit Standardabweichung und r stabil bleiben.
func ramp(m int) float64 { return float64(50 + (m*37)%450) }

// wobble ist ein Versatz von plus und minus 15 W im Wechsel, r faellt darunter.
func wobble(m int) float64 {
	if m%2 == 0 {
		return 15
	}
	return -15
}

// suggest fuettert den Sammler mit minutes Minuten ab 12:00 UTC und fragt die Vorschlaege ab.
func suggest(th Thresholds, minutes int, model energy.Model, edge func(a, b string) bool, values func(m int) map[string]float64) []Suggestion {
	start := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	c := NewCollector(th, time.UTC)
	feed(c, start, minutes, values)
	flush(c, start.Add(time.Duration(minutes)*time.Minute))
	return c.Suggestions(model, edge)
}

func noEdge(a, b string) bool { return false }

// pairName liefert die beiden Entitaeten eines Vorschlags unabhaengig von der Reihenfolge.
func pairName(s Suggestion) string {
	if s.A < s.B {
		return s.A + "-" + s.B
	}
	return s.B + "-" + s.A
}

func baseThresholds() Thresholds {
	th := DefaultThresholds
	th.MinDays = 1
	th.MinActiveMinutes = 30
	return th
}

func TestSuggestTwoCorrelatedPoints(t *testing.T) {
	th := baseThresholds()
	model := energy.Model{}

	out := suggest(th, 40, model, noEdge, func(m int) map[string]float64 {
		a := ramp(m)
		return map[string]float64{"a": a, "b": 0.95*a - 3}
	})
	if len(out) != 1 {
		t.Fatalf("suggestions = %d, want 1: %+v", len(out), out)
	}
	s := out[0]
	if s.A != "a" || s.B != "b" {
		t.Errorf("order = %s, %s, want a, b", s.A, s.B)
	}
	if s.R < 0.999 {
		t.Errorf("r = %v, want >= 0.999", s.R)
	}
	if s.Edge || s.ChainID != "" {
		t.Errorf("edge %v chain %q, want false and empty", s.Edge, s.ChainID)
	}
	if s.MeanAW <= s.MeanBW {
		t.Errorf("means a=%v b=%v, want a in front", s.MeanAW, s.MeanBW)
	}
	if s.DeviceA != "dev-a" || s.DeviceB != "dev-b" {
		t.Errorf("devices %s, %s, want dev-a, dev-b", s.DeviceA, s.DeviceB)
	}

	// Mittelwerte vertauscht: b steht vorn.
	swapped := suggest(th, 40, model, noEdge, func(m int) map[string]float64 {
		b := ramp(m)
		return map[string]float64{"a": 0.95*b - 3, "b": b}
	})
	if len(swapped) != 1 || swapped[0].A != "b" || swapped[0].B != "a" {
		t.Errorf("swapped means: %+v, want one suggestion with b in front", swapped)
	}
}

func TestSuggestRejects(t *testing.T) {
	cases := []struct {
		name   string
		th     func(Thresholds) Thresholds
		minute int
		values func(m int) map[string]float64
	}{
		{
			name: "r below threshold without edge",
			th: func(th Thresholds) Thresholds {
				th.MinR = 0.999
				return th
			},
			minute: 40,
			values: func(m int) map[string]float64 {
				return map[string]float64{"a": ramp(m), "b": ramp(m) + wobble(m)}
			},
		},
		{
			name:   "ratio below 0.7",
			th:     func(th Thresholds) Thresholds { return th },
			minute: 40,
			values: func(m int) map[string]float64 {
				return map[string]float64{"a": ramp(m), "b": 0.5 * ramp(m)}
			},
		},
		{
			name:   "b constant",
			th:     func(th Thresholds) Thresholds { return th },
			minute: 40,
			values: func(m int) map[string]float64 {
				return map[string]float64{"a": ramp(m), "b": 260}
			},
		},
		{
			name:   "duplicate of a",
			th:     func(th Thresholds) Thresholds { return th },
			minute: 40,
			values: func(m int) map[string]float64 {
				return map[string]float64{"a": ramp(m), "b": ramp(m)}
			},
		},
		{
			name: "too few days",
			th: func(th Thresholds) Thresholds {
				th.MinDays = 3
				return th
			},
			minute: 40,
			values: func(m int) map[string]float64 {
				return map[string]float64{"a": ramp(m), "b": 0.95*ramp(m) - 3}
			},
		},
		{
			name: "too few minutes",
			th: func(th Thresholds) Thresholds {
				th.MinActiveMinutes = 720
				return th
			},
			minute: 40,
			values: func(m int) map[string]float64 {
				return map[string]float64{"a": ramp(m), "b": 0.95*ramp(m) - 3}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := suggest(tc.th(baseThresholds()), tc.minute, energy.Model{}, noEdge, tc.values)
			if len(out) != 0 {
				t.Errorf("suggestions = %+v, want none", out)
			}
		})
	}
}

func TestSuggestEdgeLowersThreshold(t *testing.T) {
	th := baseThresholds()
	th.MinR = 0.999

	pair := func(m int) map[string]float64 {
		return map[string]float64{"a": ramp(m), "b": ramp(m) + wobble(m)}
	}
	edgeAB := func(x, y string) bool {
		return (x == "dev-a" && y == "dev-b") || (x == "dev-b" && y == "dev-a")
	}

	out := suggest(th, 40, energy.Model{}, edgeAB, pair)
	if len(out) != 1 || !out[0].Edge {
		t.Fatalf("with edge: %+v, want one suggestion with edge", out)
	}

	// Zweites Paar ohne Kante und mit r 1: der Vorschlag mit Kante steht trotzdem vorn.
	both := suggest(th, 40, energy.Model{}, edgeAB, func(m int) map[string]float64 {
		return map[string]float64{
			"a": ramp(m),
			"b": ramp(m) + wobble(m),
			"c": 0.9 * ramp(m),
			"d": 0.8 * ramp(m),
		}
	})
	if len(both) != 2 {
		t.Fatalf("suggestions = %d, want 2: %+v", len(both), both)
	}
	if !both[0].Edge || pairName(both[0]) != "a-b" {
		t.Errorf("first = %+v, want the edge pair a-b", both[0])
	}
	if both[1].Edge {
		t.Errorf("second = %+v, want no edge", both[1])
	}
}

func TestSuggestRespectsChains(t *testing.T) {
	values := func(m int) map[string]float64 {
		return map[string]float64{"a": ramp(m), "b": 0.95*ramp(m) - 3, "x": 100, "y": 100}
	}

	both := energy.Model{Chains: map[string]energy.Chain{
		"k1": {Points: []string{"a", "b"}, Counting: "b"},
	}}
	if out := suggest(baseThresholds(), 40, both, noEdge, values); len(out) != 0 {
		t.Errorf("adjacent in one chain: %+v, want none", out)
	}

	split := energy.Model{Chains: map[string]energy.Chain{
		"k1": {Points: []string{"a", "x"}, Counting: "x"},
		"k2": {Points: []string{"b", "y"}, Counting: "y"},
	}}
	if out := suggest(baseThresholds(), 40, split, noEdge, values); len(out) != 0 {
		t.Errorf("one entity in each chain: %+v, want none", out)
	}

	extend := energy.Model{Chains: map[string]energy.Chain{
		"k1": {Points: []string{"a", "x"}, Counting: "x"},
	}}
	out := suggest(baseThresholds(), 40, extend, noEdge, values)
	if len(out) != 1 || out[0].ChainID != "k1" {
		t.Errorf("extension: %+v, want one suggestion with chain k1", out)
	}
}

func TestSuggestOneEntityOnce(t *testing.T) {
	th := baseThresholds()

	// a-b ist perfekt, c korreliert mit a nur ueber den Versatz.
	out := suggest(th, 40, energy.Model{}, noEdge, func(m int) map[string]float64 {
		return map[string]float64{
			"a": ramp(m),
			"b": 0.95*ramp(m) - 3,
			"c": ramp(m) + wobble(m),
		}
	})
	if len(out) != 1 || pairName(out[0]) != "a-b" {
		t.Errorf("suggestions = %+v, want only a-b", out)
	}

	// Zwei freie Paare, Obergrenze 1: nur das bessere.
	th.MaxSuggestions = 1
	out = suggest(th, 40, energy.Model{}, noEdge, func(m int) map[string]float64 {
		return map[string]float64{
			"a": ramp(m),
			"b": 0.95*ramp(m) - 3,
			"c": ramp(m) + wobble(m),
			"d": 0.8*ramp(m) + wobble(m),
		}
	})
	if len(out) != 1 || pairName(out[0]) != "a-b" {
		t.Errorf("capped suggestions = %+v, want only a-b", out)
	}
}
