package chainsuggest

import (
	"fmt"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energydiscovery"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func entity(id, value, unit string) registry.EntityView {
	return registry.EntityView{
		UniqueID:          id,
		Value:             value,
		HasValue:          true,
		Available:         true,
		UnitOfMeasurement: unit,
	}
}

// devicesFor macht je Entitaet ein eigenes Geraet mit Wert in W.
func devicesFor(values map[string]float64) []registry.DeviceView {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	devices := make([]registry.DeviceView, 0, len(ids))
	for _, id := range ids {
		devices = append(devices, registry.DeviceView{
			ID:       "dev-" + id,
			Entities: []registry.EntityView{entity(id, strconv.FormatFloat(values[id], 'f', -1, 64), "W")},
		})
	}
	return devices
}

// feed fuettert je Minute sechs Beobachtungen im 10-s-Takt. Die letzte
// Minute schliesst erst die naechste Beobachtung ab.
func feed(c *Collector, start time.Time, minutes int, values func(minute int) map[string]float64) {
	for m := 0; m < minutes; m++ {
		minuteStart := start.Add(time.Duration(m) * time.Minute)
		for s := 0; s < 6; s++ {
			c.Observe(minuteStart.Add(time.Duration(s)*10*time.Second), devicesFor(values(m)))
		}
	}
}

// flush schliesst die laufende Minute ueber eine leere Beobachtung ab.
func flush(c *Collector, at time.Time) {
	c.Observe(at, nil)
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

	// Geraete, die nicht teilnehmen duerfen: fehlender Wert, veraltet,
	// nicht verfuegbar, Einheit %, Geraet energy_node. kW und negative
	// Werte gehen mit ein.
	c2 := NewCollector(DefaultThresholds, time.UTC)
	devices := []registry.DeviceView{
		{ID: "dev-a", Entities: []registry.EntityView{entity("a", "100", "W")}},
		{ID: "dev-kw", Entities: []registry.EntityView{entity("kw", "0.1", "kW")}},
		{ID: "dev-neg", Entities: []registry.EntityView{entity("neg", "-100", "W")}},
		{ID: "dev-novalue", Entities: []registry.EntityView{{UniqueID: "noval", Value: "100", HasValue: false, Available: true, UnitOfMeasurement: "W"}}},
		{ID: "dev-stale", Entities: []registry.EntityView{{UniqueID: "stale", Value: "100", HasValue: true, Stale: true, Available: true, UnitOfMeasurement: "W"}}},
		{ID: "dev-offline", Entities: []registry.EntityView{{UniqueID: "offline", Value: "100", HasValue: true, HasAvailability: true, Available: false, UnitOfMeasurement: "W"}}},
		{ID: "dev-pct", Entities: []registry.EntityView{entity("pct", "50", "%")}},
		{ID: energydiscovery.DeviceIdentifier, Entities: []registry.EntityView{entity("meter", "100", "W")}},
	}
	for m := 0; m < 3; m++ {
		minuteStart := start.Add(time.Duration(m) * time.Minute)
		for s := 0; s < 6; s++ {
			c2.Observe(minuteStart.Add(time.Duration(s)*10*time.Second), devices)
		}
	}
	if got := sortedKeys(c2.devices); fmt.Sprint(got) != "[a kw neg]" {
		t.Errorf("participants = %v, want [a kw neg]", got)
	}
	flush(c2, start.Add(3*time.Minute))
	kw := c2.sums[pairDay{a: "a", b: "kw", day: dayIndex(start)}]
	if kw == nil || kw.n != 3 || kw.sb != 300 {
		t.Errorf("kW pair sums = %+v, want n 3 and sb 300 (0.1 kW = 100 W)", kw)
	}
	neg := c2.sums[pairDay{a: "a", b: "neg", day: dayIndex(start)}]
	if neg == nil || neg.n != 3 || neg.sb != 300 {
		t.Errorf("negative pair sums = %+v, want n 3 and sb 300 (Betrag)", neg)
	}

	// Vor 2020 gestellte Uhr: kein Puffer, keine Teilnehmer.
	c3 := NewCollector(DefaultThresholds, time.UTC)
	c3.Observe(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), devicesFor(map[string]float64{"a": 100}))
	if len(c3.buf) != 0 || !c3.minute.IsZero() || len(c3.devices) != 0 {
		t.Errorf("clock not ready: buf %d, minute %v, devices %d, want nothing", len(c3.buf), c3.minute, len(c3.devices))
	}
}

func TestCollectorWindowDropsOldDays(t *testing.T) {
	day0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	c := NewCollector(DefaultThresholds, time.UTC)
	for d := 0; d < 8; d++ {
		feed(c, day0.Add(time.Duration(d)*24*time.Hour), 1, func(int) map[string]float64 {
			return map[string]float64{"a": 100, "b": 90}
		})
	}
	// Die Minute von Tag 7 schliesst erst die Beobachtung auf Tag 8 ab.
	flush(c, day0.Add(8*24*time.Hour))

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

func TestCollectorMaxEntities(t *testing.T) {
	start := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	values := map[string]float64{}
	for i := 0; i < 70; i++ {
		values[fmt.Sprintf("e%02d", i)] = 100
	}
	c := NewCollector(DefaultThresholds, time.UTC)
	feed(c, start, 1, func(int) map[string]float64 { return values })

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
	devices := []registry.DeviceView{
		{ID: "dev-a", Entities: []registry.EntityView{entity("a", "100", "W")}},
		{ID: "dev-nan", Entities: []registry.EntityView{entity("nan", "NaN", "W")}},
		{ID: "dev-inf", Entities: []registry.EntityView{entity("inf", "Inf", "W")}},
		{ID: "dev-ninf", Entities: []registry.EntityView{entity("ninf", "-Inf", "kW")}},
	}
	for s := 0; s < 6; s++ {
		c.Observe(start.Add(time.Duration(s)*10*time.Second), devices)
	}
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
