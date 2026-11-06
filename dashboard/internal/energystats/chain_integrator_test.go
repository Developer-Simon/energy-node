package energystats

import (
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

func chainPtr(v float64) *float64 { return &v }

func TestChainSampleTakesAvailableSegmentsInFlowOrder(t *testing.T) {
	cases := []struct {
		name   string
		plugW  float64
		acW    float64
		wantSW float64
		wantTW float64
	}{
		{"positive", 290, 300, 300, 290},
		{"negative", -290, -300, 300, 290},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			chain := energy.ChainState{
				ID:   "heat",
				Flow: "up",
				Points: []energy.ChainPointState{
					{EntityID: "plug", Value: c.plugW, HasValue: true, Freshness: "fresh"},
					{EntityID: "ac", Value: c.acW, HasValue: true, Freshness: "fresh"},
				},
				Segments: []energy.ChainSegmentState{
					{From: "ac", To: "plug", Available: true, LossW: 10, Efficiency: chainPtr(290.0 / 300.0)},
				},
			}
			s := ChainSampleOf(time.Unix(0, 0), []energy.ChainState{chain})
			if len(s.Pairs) != 1 {
				t.Fatalf("Pairs = %v, erwartet genau ein Paar", s.Pairs)
			}
			got, ok := s.Pairs[ChainPair{Source: "ac", Target: "plug"}]
			if !ok {
				t.Fatalf("Paar ac->plug fehlt: %v", s.Pairs)
			}
			near(t, "SourceW", got.SourceW, c.wantSW)
			near(t, "TargetW", got.TargetW, c.wantTW)
		})
	}
}

func TestChainSampleSkipsUnavailableAndSmallSegments(t *testing.T) {
	unavailable := energy.ChainState{
		ID:   "unavailable",
		Flow: "down",
		Points: []energy.ChainPointState{
			{EntityID: "plug", Value: 300, HasValue: true, Freshness: "fresh"},
			{EntityID: "ac", Value: 290, HasValue: true, Freshness: "stale"},
		},
		Segments: []energy.ChainSegmentState{
			{From: "plug", To: "ac", Available: false},
		},
	}
	small := energy.ChainState{
		ID:   "small",
		Flow: "down",
		Points: []energy.ChainPointState{
			{EntityID: "src", Value: 8, HasValue: true, Freshness: "fresh"},
			{EntityID: "dst", Value: 7.5, HasValue: true, Freshness: "fresh"},
		},
		Segments: []energy.ChainSegmentState{
			{From: "src", To: "dst", Available: true, LossW: 0.5},
		},
	}
	s := ChainSampleOf(time.Unix(0, 0), []energy.ChainState{unavailable, small})
	if len(s.Pairs) != 0 {
		t.Fatalf("Pairs = %v, erwartet leer", s.Pairs)
	}
}

func TestChainSampleSkipsIdleFlow(t *testing.T) {
	chain := energy.ChainState{
		ID:   "idle",
		Flow: "idle",
		Points: []energy.ChainPointState{
			{EntityID: "plug", Value: 300, HasValue: true, Freshness: "fresh"},
			{EntityID: "ac", Value: 290, HasValue: true, Freshness: "fresh"},
		},
		Segments: []energy.ChainSegmentState{
			{From: "plug", To: "ac", Available: true, LossW: 10, Efficiency: chainPtr(290.0 / 300.0)},
		},
	}
	s := ChainSampleOf(time.Unix(0, 0), []energy.ChainState{chain})
	if len(s.Pairs) != 0 {
		t.Fatalf("Pairs = %v, erwartet leer bei Flow idle", s.Pairs)
	}
}

func TestChainSampleStandby(t *testing.T) {
	type point struct {
		id    string
		value float64
		fresh string
	}
	cases := []struct {
		name        string
		points      []point
		segments    []energy.ChainSegmentState
		wantStandby bool
		standbyW    float64
	}{
		{
			name:        "small consumer and no downstream load",
			points:      []point{{"plug", 4, "fresh"}, {"ac", 0.5, "fresh"}, {"dc", 0, "fresh"}},
			segments:    []energy.ChainSegmentState{{From: "plug", To: "ac", Available: true, Efficiency: chainPtr(0.125)}},
			wantStandby: true,
			standbyW:    4,
		},
		{
			name:        "standby with an efficiency on the first segment",
			points:      []point{{"plug", 12, "fresh"}, {"ac", 3, "fresh"}, {"dc", 0, "fresh"}},
			segments:    []energy.ChainSegmentState{{From: "plug", To: "ac", Available: true, Efficiency: chainPtr(0.25)}},
			wantStandby: true,
			standbyW:    12,
		},
		{
			name:     "downstream point stale",
			points:   []point{{"plug", 4, "fresh"}, {"ac", 0.5, "stale"}},
			segments: []energy.ChainSegmentState{},
		},
		{
			name:     "source below idle threshold",
			points:   []point{{"plug", 0.5, "fresh"}, {"ac", 0, "fresh"}},
			segments: []energy.ChainSegmentState{},
		},
		{
			name:     "downstream load above min input",
			points:   []point{{"plug", 4, "fresh"}, {"ac", 300, "fresh"}},
			segments: []energy.ChainSegmentState{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var points []energy.ChainPointState
			for _, p := range c.points {
				points = append(points, energy.ChainPointState{
					EntityID: p.id, Value: p.value, HasValue: true, Freshness: p.fresh,
				})
			}
			chain := energy.ChainState{ID: "c", Flow: "down", Points: points, Segments: c.segments}
			s := ChainSampleOf(time.Unix(0, 0), []energy.ChainState{chain})
			if c.wantStandby {
				got, ok := s.Standby["plug"]
				if !ok {
					t.Fatalf("Standby = %v, erwartet plug", s.Standby)
				}
				near(t, "Standby[plug]", got, c.standbyW)
				if len(s.Pairs) != 0 {
					t.Fatalf("Pairs = %v, erwartet leer im Standby", s.Pairs)
				}
				return
			}
			if len(s.Standby) != 0 {
				t.Fatalf("Standby = %v, erwartet leer", s.Standby)
			}
		})
	}
}

func chainSample(at time.Time, pair ChainPair, sw, tw float64) ChainSample {
	return ChainSample{
		At:      at,
		Pairs:   map[ChainPair]PairPower{pair: {SourceW: sw, TargetW: tw}},
		Standby: map[string]float64{},
	}
}

func TestChainIntegratorCountsOnlySharedSeconds(t *testing.T) {
	var g ChainIntegrator
	pair := ChainPair{Source: "a", Target: "b"}
	start := hourAt("2026-11-06T10:00:00Z")
	var records []ChainHour
	// 30 Minuten mit Paar, dann 30 Minuten ohne Paar.
	for i := 0; i < 60; i++ {
		at := start.Add(time.Duration(i) * time.Minute)
		s := ChainSample{At: at, Pairs: map[ChainPair]PairPower{}, Standby: map[string]float64{}}
		if i < 30 {
			s = chainSample(at, pair, 300, 290)
		}
		records = append(records, g.Add(s)...)
	}
	records = append(records, g.Add(ChainSample{At: start.Add(time.Hour)})...)
	records = append(records, g.Flush()...)
	if len(records) != 1 {
		t.Fatalf("%d Stunden, erwartet 1", len(records))
	}
	got, ok := records[0].Pairs[pair]
	if !ok {
		t.Fatalf("Paar fehlt: %v", records[0].Pairs)
	}
	near(t, "SourceKWh", got.SourceKWh, 0.15)
	near(t, "TargetKWh", got.TargetKWh, 0.145)
	if got.Seconds != 1800 {
		t.Fatalf("Seconds = %d, erwartet 1800", got.Seconds)
	}
}

func TestChainIntegratorSplitsHours(t *testing.T) {
	var g ChainIntegrator
	pair := ChainPair{Source: "a", Target: "b"}
	first := hourAt("2026-11-06T10:59:30Z")
	if out := g.Add(chainSample(first, pair, 300, 290)); len(out) != 0 {
		t.Fatalf("erste Probe lieferte %d Stunden", len(out))
	}
	done := g.Add(chainSample(first.Add(60*time.Second), pair, 300, 290))
	if len(done) != 1 {
		t.Fatalf("%d Stunden, erwartet 1", len(done))
	}
	if !done[0].Hour.Equal(hourAt("2026-11-06T10:00:00Z")) {
		t.Fatalf("Stunde = %v, erwartet 10:00", done[0].Hour)
	}
	if got := done[0].Pairs[pair].Seconds; got != 30 {
		t.Fatalf("10:00 Seconds = %d, erwartet 30", got)
	}
	rest := g.Flush()
	if len(rest) != 1 {
		t.Fatalf("Flush %d Stunden, erwartet 1", len(rest))
	}
	if !rest[0].Hour.Equal(hourAt("2026-11-06T11:00:00Z")) {
		t.Fatalf("Stunde = %v, erwartet 11:00", rest[0].Hour)
	}
	if got := rest[0].Pairs[pair].Seconds; got != 30 {
		t.Fatalf("11:00 Seconds = %d, erwartet 30", got)
	}
}

func TestChainIntegratorHoldsAtMostMaxHold(t *testing.T) {
	var g ChainIntegrator
	pair := ChainPair{Source: "a", Target: "b"}
	start := hourAt("2026-11-06T10:00:00Z")
	g.Add(chainSample(start, pair, 300, 290))
	g.Add(chainSample(start.Add(20*time.Minute), pair, 300, 290))
	records := g.Flush()
	if len(records) != 1 {
		t.Fatalf("%d Stunden, erwartet 1", len(records))
	}
	got := records[0].Pairs[pair]
	if got.Seconds != 300 {
		t.Fatalf("Seconds = %d, erwartet 300 (MaxHold)", got.Seconds)
	}
	near(t, "SourceKWh", got.SourceKWh, 300*300/3.6e6)
}

func TestChainIntegratorEmptyHourYieldsNoRecord(t *testing.T) {
	var g ChainIntegrator
	start := hourAt("2026-11-06T10:00:00Z")
	empty := func(at time.Time) ChainSample {
		return ChainSample{At: at, Pairs: map[ChainPair]PairPower{}, Standby: map[string]float64{}}
	}
	if out := g.Add(empty(start)); len(out) != 0 {
		t.Fatalf("Add lieferte %d Stunden", len(out))
	}
	if out := g.Add(empty(start.Add(30 * time.Minute))); len(out) != 0 {
		t.Fatalf("Add lieferte %d Stunden", len(out))
	}
	if out := g.Flush(); len(out) != 0 {
		t.Fatalf("Flush lieferte %d Stunden, erwartet keine", len(out))
	}
}

func TestChainIntegratorIgnoresOutOfOrderSample(t *testing.T) {
	var g ChainIntegrator
	pair := ChainPair{Source: "a", Target: "b"}
	start := hourAt("2026-11-06T10:00:00Z")
	g.Add(chainSample(start, pair, 300, 290))
	g.Add(chainSample(start.Add(time.Minute), pair, 300, 290))
	if out := g.Add(chainSample(start.Add(30*time.Second), pair, 1000, 900)); out != nil {
		t.Fatalf("veraltete Probe lieferte %d Stunden", len(out))
	}
	records := g.Flush()
	if len(records) != 1 {
		t.Fatalf("%d Stunden, erwartet 1", len(records))
	}
	got := records[0].Pairs[pair]
	if got.Seconds != 60 {
		t.Fatalf("Seconds = %d, erwartet 60", got.Seconds)
	}
	near(t, "SourceKWh", got.SourceKWh, 300*60/3.6e6)
}

func TestChainIntegratorResetDropsHourFlushKeepsIt(t *testing.T) {
	pair := ChainPair{Source: "a", Target: "b"}
	start := hourAt("2026-11-06T10:00:00Z")

	var reset ChainIntegrator
	reset.Add(chainSample(start, pair, 300, 290))
	reset.Add(chainSample(start.Add(time.Minute), pair, 300, 290))
	reset.Reset()
	if out := reset.Flush(); len(out) != 0 {
		t.Fatalf("nach Reset lieferte Flush %d Stunden, erwartet keine", len(out))
	}

	var kept ChainIntegrator
	kept.Add(chainSample(start, pair, 300, 290))
	kept.Add(chainSample(start.Add(time.Minute), pair, 300, 290))
	records := kept.Flush()
	if len(records) != 1 {
		t.Fatalf("Flush lieferte %d Stunden, erwartet 1", len(records))
	}
	if got := records[0].Pairs[pair].Seconds; got != 60 {
		t.Fatalf("Seconds = %d, erwartet 60", got)
	}
}

func TestChainIntegratorAccumulatesStandby(t *testing.T) {
	var g ChainIntegrator
	start := hourAt("2026-11-06T10:00:00Z")
	standby := func(at time.Time) ChainSample {
		return ChainSample{At: at, Pairs: map[ChainPair]PairPower{}, Standby: map[string]float64{"plug": 4}}
	}
	// Proben alle 5 Minuten (MaxHold), danach eine Probe ohne Standby.
	for i := 0; i < 6; i++ {
		g.Add(standby(start.Add(time.Duration(i) * 5 * time.Minute)))
	}
	g.Add(ChainSample{At: start.Add(30 * time.Minute), Pairs: map[ChainPair]PairPower{}, Standby: map[string]float64{}})
	records := g.Flush()
	if len(records) != 1 {
		t.Fatalf("%d Stunden, erwartet 1", len(records))
	}
	got := records[0].Standby["plug"]
	if got.Seconds != 1800 {
		t.Fatalf("Standby Seconds = %d, erwartet 1800", got.Seconds)
	}
	near(t, "Standby KWh", got.KWh, 4*1800/3.6e6)
}
