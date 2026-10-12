package energystats

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func hourAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func TestNewMonthHasOneSlotPerHour(t *testing.T) {
	if got := NewMonth(hourAt("2026-02-10T05:00:00Z")).Hours(); got != 28*24 {
		t.Fatalf("Februar 2026: %d Stunden, erwartet %d", got, 28*24)
	}
	if got := NewMonth(hourAt("2026-10-31T23:00:00Z")).Hours(); got != 31*24 {
		t.Fatalf("Oktober 2026: %d Stunden, erwartet %d", got, 31*24)
	}
}

func TestIndexRejectsHoursOutsideTheMonth(t *testing.T) {
	m := NewMonth(hourAt("2026-10-01T00:00:00Z"))
	if i, ok := m.Index(hourAt("2026-10-01T03:00:00Z")); !ok || i != 3 {
		t.Fatalf("Index = %d, %v", i, ok)
	}
	if _, ok := m.Index(hourAt("2026-11-01T00:00:00Z")); ok {
		t.Fatal("Stunde des Folgemonats darf nicht passen")
	}
	if _, ok := m.Index(hourAt("2026-09-30T23:00:00Z")); ok {
		t.Fatal("Stunde des Vormonats darf nicht passen")
	}
}

func TestAddOwnSumsWithinTheSameHour(t *testing.T) {
	m := NewMonth(hourAt("2026-10-01T00:00:00Z"))
	h := hourAt("2026-10-05T10:00:00Z")
	m.AddOwn(h, map[string]float64{"pv": 0.25}, 900)
	m.AddOwn(h, map[string]float64{"pv": 0.5}, 1800)
	i, _ := m.Index(h)
	if got := m.Roles["pv"].KWh[i]; math.Abs(got-0.75) > 1e-9 {
		t.Fatalf("pv = %v, erwartet 0.75", got)
	}
	if got := m.Roles["pv"].Source[i]; got != SourceOwn {
		t.Fatalf("Quelle = %q", got)
	}
	if got := m.Covered[i]; got != 2700 {
		t.Fatalf("Covered = %d, erwartet 2700", got)
	}
}

func TestAddOwnCapsCoveredAtOneHour(t *testing.T) {
	m := NewMonth(hourAt("2026-10-01T00:00:00Z"))
	h := hourAt("2026-10-05T10:00:00Z")
	m.AddOwn(h, map[string]float64{"pv": 1}, 3000)
	m.AddOwn(h, map[string]float64{"pv": 1}, 3000)
	i, _ := m.Index(h)
	if got := m.Covered[i]; got != 3600 {
		t.Fatalf("Covered = %d, erwartet 3600", got)
	}
}

func TestHAReplacesOwnAndOwnNeverReplacesHA(t *testing.T) {
	m := NewMonth(hourAt("2026-10-01T00:00:00Z"))
	h := hourAt("2026-10-05T10:00:00Z")
	m.AddOwn(h, map[string]float64{"pv": 0.4}, 3600)
	if !m.SetHA(h, "pv", 0.5) {
		t.Fatal("SetHA meldet keine Aenderung")
	}
	if m.AddOwn(h, map[string]float64{"pv": 9}, 3600) {
		t.Fatal("AddOwn darf eine HA-Stunde nicht aendern")
	}
	i, _ := m.Index(h)
	if got := m.Roles["pv"].KWh[i]; got != 0.5 {
		t.Fatalf("pv = %v, erwartet 0.5", got)
	}
	if got := m.Roles["pv"].Source[i]; got != SourceHA {
		t.Fatalf("Quelle = %q", got)
	}
}

func TestMonthJSONRoundTrip(t *testing.T) {
	m := NewMonth(hourAt("2026-10-01T00:00:00Z"))
	h := hourAt("2026-10-05T10:00:00Z")
	m.AddOwn(h, map[string]float64{"pv": 0.1234567891, "load_total": 0.3}, 3600)
	m.SetHA(hourAt("2026-10-05T11:00:00Z"), "pv", 0.2)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	back, err := decodeMonth(data)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Start.Equal(m.Start) || back.Hours() != m.Hours() {
		t.Fatalf("Start/Stunden: %v %d", back.Start, back.Hours())
	}
	i, _ := back.Index(h)
	if got := back.Roles["pv"].KWh[i]; math.Abs(got-0.123457) > 1e-9 {
		t.Fatalf("pv = %v, erwartet auf 6 Stellen gerundet 0.123457", got)
	}
	if got := back.Roles["pv"].Source[i+1]; got != SourceHA {
		t.Fatalf("Quelle Stunde 11 = %q", got)
	}
	if got := back.Roles["pv"].KWh[0]; !math.IsNaN(got) {
		t.Fatalf("leere Stunde = %v, erwartet NaN", got)
	}
	if back.Covered[i] != 3600 {
		t.Fatalf("Covered = %d", back.Covered[i])
	}
}

func TestDecodeMonthRejectsBrokenFiles(t *testing.T) {
	cases := map[string]string{
		"kein JSON":          `{`,
		"falsche Version":    `{"version":2,"month":"2026-10","covered":[],"roles":{}}`,
		"falsche Laenge":     `{"version":1,"month":"2026-10","covered":[0],"roles":{}}`,
		"unbekannte Quelle":  `{"version":1,"month":"2026-02","covered":` + zeros(672) + `,"roles":{"pv":{"kwh":` + nulls(672) + `,"src":"` + repeat("x", 672) + `"}}}`,
		"Monat nicht lesbar": `{"version":1,"month":"Oktober","covered":[],"roles":{}}`,
	}
	for name, body := range cases {
		if _, err := decodeMonth([]byte(body)); err == nil {
			t.Errorf("%s: kein Fehler", name)
		}
	}
}

func zeros(n int) string { return jsonList(n, "0") }
func nulls(n int) string { return jsonList(n, "null") }
func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
func jsonList(n int, item string) string {
	out := "["
	for i := 0; i < n; i++ {
		if i > 0 {
			out += ","
		}
		out += item
	}
	return out + "]"
}
