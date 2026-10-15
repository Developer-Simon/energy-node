package energystats

import (
	"errors"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

func TestHistoryRowsMapsRolesToHistorySeries(t *testing.T) {
	store := NewStore(t.TempDir())
	h := hourAt("2026-10-05T10:00:00Z")
	store.AddOwn(HourRecord{Hour: h, Covered: 3600, KWh: map[string]float64{
		"pv": 1.2, "grid_import": 0.5, "grid_export": 0.2, "battery_charge": 0.3, "battery_discharge": 0.1,
		"load_total": 1.4, "base": 0.4, "wallbox": 0, "heat_pump": 0.6, "custom:sauna": 0.7, "custom:akku": 0.9,
	}})
	cats := map[string]energy.Category{
		"sauna": {Label: "Sauna", Base: energy.CategoryConsumer},
		"akku":  {Label: "Akku", Base: energy.CategoryStorage},
	}
	rows, err := HistoryRows(store, h, h.Add(time.Hour), cats)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, r := range rows {
		if r.TS != h.UnixMilli() || r.N != 1 || r.U != "W" || r.Min != r.Avg || r.Max != r.Avg {
			t.Fatalf("Satz %+v", r)
		}
		got[r.Series] = r.Avg
	}
	want := map[string]float64{
		"role:pv": 1200, "role:grid": 300, "role:battery": 200, "role:heat_pump": 600,
		"role:custom:consumer:sauna": 700, "berechnet:hausverbrauch": 1400,
	}
	if len(got) != len(want) {
		t.Fatalf("Serien %v", got)
	}
	for k, v := range want {
		if d := got[k] - v; d > 1e-9 || d < -1e-9 {
			t.Fatalf("%s = %v, erwartet %v", k, got[k], v)
		}
	}
}

func TestHistoryRowsSkipsShortOwnHoursButKeepsHA(t *testing.T) {
	store := NewStore(t.TempDir())
	h := hourAt("2026-10-05T10:00:00Z")
	store.AddOwn(HourRecord{Hour: h, Covered: 1800, KWh: map[string]float64{"pv": 0.5}})
	store.AddOwn(HourRecord{Hour: h.Add(time.Hour), Covered: 600, KWh: map[string]float64{"pv": 0.1}})
	store.SetHA([]HARecord{{Hour: h.Add(time.Hour), Role: "pv", KWh: 0.8}})
	rows, _ := HistoryRows(store, h, h.Add(2*time.Hour), nil)
	if len(rows) != 1 || rows[0].TS != h.Add(time.Hour).UnixMilli() || rows[0].Avg != 800 {
		t.Fatalf("rows %+v", rows)
	}
}

func TestHistoryRowsNeedsBothDirectionsForGrid(t *testing.T) {
	store := NewStore(t.TempDir())
	h := hourAt("2026-10-05T10:00:00Z")
	store.SetHA([]HARecord{{Hour: h, Role: "grid_import", KWh: 0.5}})
	rows, _ := HistoryRows(store, h, h.Add(time.Hour), nil)
	if len(rows) != 0 {
		t.Fatalf("rows %+v", rows)
	}
}

func TestHistoryRowsRejectsBadRanges(t *testing.T) {
	store := NewStore(t.TempDir())
	h := hourAt("2026-10-05T10:00:00Z")
	for _, c := range [][2]time.Time{{h, h}, {h, h.Add(-time.Hour)}, {h, h.Add(MaxHistorySpan + time.Hour)}} {
		if _, err := HistoryRows(store, c[0], c[1], nil); !errors.Is(err, ErrInvalidRange) {
			t.Fatalf("%v: err %v", c, err)
		}
	}
}
