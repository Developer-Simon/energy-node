package chainsuggest

import (
	"fmt"
	"math"
	"sort"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

// frameFor macht den Rahmen einer Minute: je Entitaet ein eigenes Geraet mit
// dem Betrag in W. kW und Vorzeichen fallen vorher in learnrec.PowerOf weg.
func frameFor(minute time.Time, values map[string]float64) learnrec.Frame {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	devices := make([]learnrec.DeviceMinute, 0, len(ids))
	for _, id := range ids {
		devices = append(devices, learnrec.DeviceMinute{
			ID:        "dev-" + id,
			Power:     map[string]float64{id: values[id]},
			Reachable: true,
		})
	}
	return learnrec.Frame{Minute: minute, Devices: devices}
}

// feed fuettert je Minute einen Rahmen. Jeder Rahmen ist eine abgeschlossene Minute.
func feed(c *Collector, start time.Time, minutes int, values func(minute int) map[string]float64) {
	for m := 0; m < minutes; m++ {
		c.ObserveMinute(frameFor(start.Add(time.Duration(m)*time.Minute), values(m)))
	}
}

// flush schliesst die Reihe mit einem leeren Rahmen ab. Er stoesst nur den
// Tageswechsel an, Teilnehmer hat er keine.
func flush(c *Collector, at time.Time) {
	c.ObserveMinute(learnrec.Frame{Minute: at})
}

func TestCollectorMinuteMeansAndActiveFilter(t *testing.T) {
	start := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	c := NewCollector(DefaultThresholds, time.UTC)
	feed(c, start, 3, func(int) map[string]float64 { return map[string]float64{"a": 100, "b": 90} })
	feed(c, start.Add(3*time.Minute), 3, func(int) map[string]float64 { return map[string]float64{"a": 5, "b": 5} })
	flush(c, start.Add(6*time.Minute))

	s := c.sums[pairDay{a: "a", b: "b", day: dayIndex(start)}]
	if s == nil {
		t.Fatal("no sums for pair a-b")
	}
	if s.n != 3 {
		t.Errorf("active minutes = %d, want 3", s.n)
	}
	if s.sa != 300 || s.sb != 270 {
		t.Errorf("sums a=%v b=%v, want 300 and 270", s.sa, s.sb)
	}

	// Nur die Entitaeten im Rahmen nehmen teil. Die Umrechnung von kW und
	// das Vorzeichen liegen bei learnrec.PowerOf, hier kommen Betraege in W an.
	c2 := NewCollector(DefaultThresholds, time.UTC)
	for m := 0; m < 3; m++ {
		c2.ObserveMinute(frameFor(start.Add(time.Duration(m)*time.Minute), map[string]float64{"a": 100, "kw": 100, "neg": 100}))
	}
	if got := sortedKeys(c2.devices); fmt.Sprint(got) != "[a kw neg]" {
		t.Errorf("participants = %v, want [a kw neg]", got)
	}
	flush(c2, start.Add(3*time.Minute))
	kw := c2.sums[pairDay{a: "a", b: "kw", day: dayIndex(start)}]
	if kw == nil || kw.n != 3 || kw.sb != 300 {
		t.Errorf("kW pair sums = %+v, want n 3 and sb 300", kw)
	}
	neg := c2.sums[pairDay{a: "a", b: "neg", day: dayIndex(start)}]
	if neg == nil || neg.n != 3 || neg.sb != 300 {
		t.Errorf("negative pair sums = %+v, want n 3 and sb 300", neg)
	}

	// Vor 2020 gestellte Uhr: keine Summen, keine Teilnehmer.
	c3 := NewCollector(DefaultThresholds, time.UTC)
	c3.ObserveMinute(frameFor(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), map[string]float64{"a": 100, "b": 90}))
	if len(c3.sums) != 0 || len(c3.devices) != 0 {
		t.Errorf("clock not ready: sums %d, devices %d, want nothing", len(c3.sums), len(c3.devices))
	}
}

func TestCollectorWindowDropsOldDays(t *testing.T) {
	day0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	c := NewCollector(DefaultThresholds, time.UTC)
	// Acht Tage, der Tageswechsel auf Tag 7 wirft Tag 0 aus dem Fenster.
	for d := 0; d < 8; d++ {
		c.ObserveMinute(frameFor(day0.Add(time.Duration(d)*24*time.Hour), map[string]float64{"a": 100, "b": 90}))
	}

	days := map[int]bool{}
	for key := range c.sums {
		if key.a == "a" && key.b == "b" {
			days[key.day] = true
		}
	}
	if len(days) != 7 {
		t.Errorf("days in window = %d, want 7", len(days))
	}
	if days[dayIndex(day0)] {
		t.Errorf("day 0 still in window after rollover")
	}
}

func TestCollectorMaxEntitiesOnFrame(t *testing.T) {
	start := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	values := map[string]float64{}
	for i := 0; i < 70; i++ {
		values[fmt.Sprintf("e%02d", i)] = 100
	}
	c := NewCollector(DefaultThresholds, time.UTC)
	c.ObserveMinute(frameFor(start, values))

	if len(c.devices) != 64 {
		t.Fatalf("participants = %d, want 64", len(c.devices))
	}
	if _, ok := c.devices["e63"]; !ok {
		t.Errorf("e63 missing, the first 64 by ID should take part")
	}
	if _, ok := c.devices["e64"]; ok {
		t.Errorf("e64 present, only the first 64 by ID should take part")
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestCollectorSkipsNonFinite(t *testing.T) {
	start := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	c := NewCollector(DefaultThresholds, time.UTC)
	f := learnrec.Frame{Minute: start, Devices: []learnrec.DeviceMinute{
		{ID: "dev-a", Power: map[string]float64{"a": 100}},
		{ID: "dev-nan", Power: map[string]float64{"nan": math.NaN()}},
		{ID: "dev-inf", Power: map[string]float64{"inf": math.Inf(1)}},
		{ID: "dev-ninf", Power: map[string]float64{"ninf": math.Inf(-1)}},
	}}
	c.ObserveMinute(f)
	flush(c, start.Add(time.Minute))

	if got := sortedKeys(c.owner); fmt.Sprint(got) != "[a]" {
		t.Errorf("participants = %v, want [a]", got)
	}
	for key := range c.sums {
		if key.a == "nan" || key.b == "nan" || key.a == "inf" || key.b == "inf" || key.a == "ninf" || key.b == "ninf" {
			t.Errorf("non-finite entity in sums: %+v", key)
		}
	}
}

func TestCollectorDisabled(t *testing.T) {
	start := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	c := NewCollector(DefaultThresholds, time.UTC)
	both := func() map[string]float64 {
		return map[string]float64{"a": 100, "b": 90}
	}
	// Minute 0 zaehlt, bevor abgeschaltet wird.
	c.ObserveMinute(frameFor(start, both()))
	c.dirty = false

	c.SetEnabled(false)
	if c.Enabled() {
		t.Fatal("Enabled after SetEnabled(false) = true")
	}
	if c.dirty {
		t.Fatal("SetEnabled(false) set dirty")
	}
	// Minute 1 kommt bei abgeschaltetem Sammler an und zaehlt nicht.
	c.ObserveMinute(frameFor(start.Add(time.Minute), both()))
	if c.dirty {
		t.Fatal("observe while disabled set dirty")
	}

	c.SetEnabled(true)
	feed(c, start.Add(2*time.Minute), 2, func(int) map[string]float64 { return both() })

	// Minute 0, 2 und 3 zaehlen, Minute 1 nicht.
	s := c.sums[pairDay{a: "a", b: "b", day: dayIndex(start)}]
	if s == nil || s.n != 3 {
		t.Fatalf("sums = %+v, want n 3 (minute 0, minutes 2 and 3)", s)
	}
}
