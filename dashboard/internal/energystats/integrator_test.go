package energystats

import (
	"math"
	"testing"
	"time"
)

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("%s = %v, erwartet %v", name, got, want)
	}
}

func feed(g *Integrator, from, to time.Time, step time.Duration, power map[string]float64) []HourRecord {
	var out []HourRecord
	for at := from; !at.After(to); at = at.Add(step) {
		out = append(out, g.Add(Sample{At: at, Power: power})...)
	}
	return out
}

func TestConstantPowerForOneHourIsOneKWhPerKW(t *testing.T) {
	var g Integrator
	start := hourAt("2026-10-05T10:00:00Z")
	records := feed(&g, start, start.Add(time.Hour), 10*time.Second, map[string]float64{"pv": 1000, "load_total": 250})
	if len(records) != 1 {
		t.Fatalf("%d Saetze, erwartet 1", len(records))
	}
	r := records[0]
	if !r.Hour.Equal(start) {
		t.Fatalf("Stunde = %v", r.Hour)
	}
	near(t, "pv", r.KWh["pv"], 1.0)
	near(t, "load_total", r.KWh["load_total"], 0.25)
	if r.Covered != 3600 {
		t.Fatalf("Covered = %d", r.Covered)
	}
}

func TestGapLongerThanMaxHoldAttributesNoEnergyToTheGap(t *testing.T) {
	var g Integrator
	start := hourAt("2026-10-05T10:00:00Z")
	var records []HourRecord
	records = append(records, g.Add(Sample{At: start, Power: map[string]float64{"pv": 1200}})...)
	records = append(records, g.Add(Sample{At: start.Add(3 * time.Hour), Power: map[string]float64{"pv": 1200}})...)
	records = append(records, g.Flush()...)
	if len(records) != 1 {
		t.Fatalf("%d Saetze, erwartet nur Stunde 10", len(records))
	}
	near(t, "pv", records[0].KWh["pv"], 1.2*5.0/60.0)
	if records[0].Covered != 300 {
		t.Fatalf("Covered = %d, erwartet 300", records[0].Covered)
	}
}

func TestIntervalAcrossTheHourIsSplit(t *testing.T) {
	var g Integrator
	g.Add(Sample{At: hourAt("2026-10-05T10:59:50Z"), Power: map[string]float64{"pv": 3600}})
	records := g.Add(Sample{At: hourAt("2026-10-05T11:00:10Z"), Power: map[string]float64{"pv": 3600}})
	if len(records) != 1 || !records[0].Hour.Equal(hourAt("2026-10-05T10:00:00Z")) {
		t.Fatalf("Saetze = %+v", records)
	}
	near(t, "pv Stunde 10", records[0].KWh["pv"], 0.01)
	rest := g.Flush()
	if len(rest) != 1 || !rest[0].Hour.Equal(hourAt("2026-10-05T11:00:00Z")) {
		t.Fatalf("Rest = %+v", rest)
	}
	near(t, "pv Stunde 11", rest[0].KWh["pv"], 0.01)
}

func TestNilPowerIsNotIntegrated(t *testing.T) {
	var g Integrator
	start := hourAt("2026-10-05T10:00:00Z")
	g.Add(Sample{At: start, Power: nil})
	g.Add(Sample{At: start.Add(time.Minute), Power: map[string]float64{"pv": 600}})
	records := append(g.Add(Sample{At: start.Add(2 * time.Minute), Power: nil}), g.Flush()...)
	if len(records) != 1 {
		t.Fatalf("%d Saetze", len(records))
	}
	near(t, "pv", records[0].KWh["pv"], 0.01)
	if records[0].Covered != 60 {
		t.Fatalf("Covered = %d, erwartet 60", records[0].Covered)
	}
}

func TestSampleNotAfterTheLastIsIgnored(t *testing.T) {
	var g Integrator
	start := hourAt("2026-10-05T10:00:00Z")
	g.Add(Sample{At: start.Add(time.Minute), Power: map[string]float64{"pv": 600}})
	if out := g.Add(Sample{At: start, Power: map[string]float64{"pv": 600}}); out != nil {
		t.Fatalf("Ruecksprung erzeugt Saetze: %+v", out)
	}
	g.Add(Sample{At: start.Add(2 * time.Minute), Power: map[string]float64{"pv": 600}})
	records := g.Flush()
	near(t, "pv", records[0].KWh["pv"], 0.01)
}

func TestFlushAndResetEmptyTheRunningHour(t *testing.T) {
	var g Integrator
	start := hourAt("2026-10-05T10:00:00Z")
	feed(&g, start, start.Add(10*time.Minute), 10*time.Second, map[string]float64{"pv": 600})
	if got := g.Flush(); len(got) != 1 || got[0].Covered != 600 {
		t.Fatalf("Flush = %+v", got)
	}
	if got := g.Flush(); got != nil {
		t.Fatalf("zweites Flush = %+v", got)
	}
	feed(&g, start.Add(20*time.Minute), start.Add(30*time.Minute), 10*time.Second, map[string]float64{"pv": 600})
	g.Reset()
	if got := g.Flush(); got != nil {
		t.Fatalf("Flush nach Reset = %+v", got)
	}
}
