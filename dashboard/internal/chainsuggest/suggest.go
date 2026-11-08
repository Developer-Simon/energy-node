package chainsuggest

import (
	"math"
	"sort"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

// Suggestion ist ein vorgeschlagenes Messketten-Paar. A steht netzseitig vorn,
// also mit dem hoeheren Mittelwert.
type Suggestion struct {
	A       string  `json:"a"`
	B       string  `json:"b"`
	DeviceA string  `json:"device_a"`
	DeviceB string  `json:"device_b"`
	R       float64 `json:"r"`
	Days    int     `json:"days"`
	Minutes int     `json:"minutes"`
	MeanAW  float64 `json:"mean_a_w"`
	MeanBW  float64 `json:"mean_b_w"`
	Edge    bool    `json:"edge"`
	ChainID string  `json:"chain_id,omitempty"`
}

// pairTotals fasst die Tagessummen eines Paares ueber das Fenster zusammen.
type pairTotals struct {
	days int
	sums
}

func (s *sums) add(o *sums) {
	s.n += o.n
	s.sa += o.sa
	s.sb += o.sb
	s.saa += o.saa
	s.sbb += o.sbb
	s.sab += o.sab
	s.sad += o.sad
}

// Suggestions liefert die Kettenvorschlaege aus den gesammelten Summen. edge
// meldet, ob eine Energie-Verbindung die beiden Geraete verbindet. Die Funktion
// veraendert nichts.
func (c *Collector) Suggestions(model energy.Model, edge func(deviceA, deviceB string) bool) []Suggestion {
	c.mu.Lock()
	defer c.mu.Unlock()

	totals := map[pairKey]*pairTotals{}
	for key, s := range c.sums {
		pk := pairKey{a: key.a, b: key.b}
		t := totals[pk]
		if t == nil {
			t = &pairTotals{}
			totals[pk] = t
		}
		t.days++
		t.add(s)
	}

	var cands []Suggestion
	for pk, t := range totals {
		if c.dismissed[pk] {
			continue
		}
		if t.days < c.th.MinDays || t.n < c.th.MinActiveMinutes {
			continue
		}
		devA, okA := c.owner[pk.a]
		devB, okB := c.owner[pk.b]
		if !okA || !okB {
			continue
		}
		chainA, _, inA := model.ChainOf(pk.a)
		chainB, _, inB := model.ChainOf(pk.b)
		if inA && inB {
			continue
		}
		chainID := ""
		if inA {
			chainID = chainA
		}
		if inB {
			chainID = chainB
		}

		isEdge := edge != nil && edge(devA, devB)
		minR := c.th.MinR
		if isEdge {
			minR = c.th.MinREdge
		}

		n := float64(t.n)
		ma, mb := t.sa/n, t.sb/n
		varA := t.saa/n - ma*ma
		varB := t.sbb/n - mb*mb
		if math.Sqrt(math.Max(varA, 0)) < c.th.MinStdW || math.Sqrt(math.Max(varB, 0)) < c.th.MinStdW {
			continue
		}
		if t.sad/n < c.th.MinMeanAbsDiffW {
			continue
		}
		hi, lo := math.Max(ma, mb), math.Min(ma, mb)
		if hi <= 0 || lo/hi < c.th.MinRatio {
			continue
		}
		den := (n*t.saa - t.sa*t.sa) * (n*t.sbb - t.sb*t.sb)
		if den <= 0 {
			continue
		}
		r := (n*t.sab - t.sa*t.sb) / math.Sqrt(den)
		if r < minR {
			continue
		}

		s := Suggestion{
			A: pk.a, B: pk.b, DeviceA: devA, DeviceB: devB,
			R: r, Days: t.days, Minutes: t.n,
			MeanAW: ma, MeanBW: mb,
			Edge: isEdge, ChainID: chainID,
		}
		if mb > ma {
			s.A, s.B = s.B, s.A
			s.DeviceA, s.DeviceB = s.DeviceB, s.DeviceA
			s.MeanAW, s.MeanBW = s.MeanBW, s.MeanAW
		}
		cands = append(cands, s)
	}

	sort.Slice(cands, func(i, j int) bool {
		x, y := cands[i], cands[j]
		if x.Edge != y.Edge {
			return x.Edge
		}
		if x.R != y.R {
			return x.R > y.R
		}
		if x.A != y.A {
			return x.A < y.A
		}
		return x.B < y.B
	})

	// Der beste Vorschlag gewinnt, eine Entitaet erscheint hoechstens einmal.
	used := map[string]bool{}
	out := []Suggestion{}
	for _, s := range cands {
		if len(out) >= c.th.MaxSuggestions {
			break
		}
		if used[s.A] || used[s.B] {
			continue
		}
		used[s.A], used[s.B] = true, true
		s.R = round(s.R, 3)
		s.MeanAW = round(s.MeanAW, 1)
		s.MeanBW = round(s.MeanBW, 1)
		out = append(out, s)
	}
	return out
}

func round(x float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(x*p) / p
}
