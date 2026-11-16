package performance

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeState writes content to a fresh state file and returns its path.
func writeState(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-performance.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// unitByID returns the unit with the given ID and whether it is in the snapshot.
func unitByID(snap Snapshot, id string) (UnitStats, bool) {
	for _, u := range snap.Units {
		if u.ID == id {
			return u, true
		}
	}
	return UnitStats{}, false
}

func TestFileProviderMissingFileGivesGood(t *testing.T) {
	p := NewFileProvider(filepath.Join(t.TempDir(), "missing.json"))
	snap := p.Snapshot(time.Now())
	want := Presets()["good"]

	if snap.CPUP95 != want.CPUPct {
		t.Errorf("CPUP95 = %v, want %v", snap.CPUP95, want.CPUPct)
	}
	if snap.ClientsMax != 3 || !snap.ClientsSeen {
		t.Errorf("ClientsMax = %d ClientsSeen = %v, want 3 true", snap.ClientsMax, snap.ClientsSeen)
	}
	if len(snap.Units) != len(want.Units) {
		t.Errorf("len(Units) = %d, want %d", len(snap.Units), len(want.Units))
	}
	if snap.MeasuredFor != 7*time.Hour {
		t.Errorf("MeasuredFor = %v, want 7h", snap.MeasuredFor)
	}
}

func TestFileProviderPresetsYieldExpectedFacts(t *testing.T) {
	tests := []struct {
		preset string
		check  func(t *testing.T, snap Snapshot)
	}{
		{"measuring", func(t *testing.T, s Snapshot) {
			if s.MeasuredFor != 30*time.Minute {
				t.Errorf("MeasuredFor = %v, want 30m", s.MeasuredFor)
			}
		}},
		{"good", func(t *testing.T, s Snapshot) {
			if s.CPUP95 != 32 || s.Throttled || !s.RAMFreeKnown || s.RAMFreePct != 61 {
				t.Errorf("good facts off: %+v", s)
			}
		}},
		{"tight", func(t *testing.T, s Snapshot) {
			if s.CPUP95 != 64 {
				t.Errorf("CPUP95 = %v, want 64", s.CPUP95)
			}
		}},
		{"over", func(t *testing.T, s Snapshot) {
			if s.CPUP95 != 78 {
				t.Errorf("CPUP95 = %v, want 78", s.CPUP95)
			}
			shelly, ok := unitByID(s, "shelly")
			if !ok || shelly.CPUP95 != 14 {
				t.Errorf("shelly = %+v, want CPUP95 14", shelly)
			}
			trucki, ok := unitByID(s, "trucki")
			if !ok || trucki.CPUP95 != 9 {
				t.Errorf("trucki = %+v, want CPUP95 9", trucki)
			}
		}},
		{"throttled", func(t *testing.T, s Snapshot) {
			if !s.Throttled || s.TempC == nil || *s.TempC != 82 || s.CPUP95 != 40 {
				t.Errorf("throttled facts off: throttled=%v temp=%v cpu=%v", s.Throttled, s.TempC, s.CPUP95)
			}
		}},
		{"memory", func(t *testing.T, s Snapshot) {
			if s.RAMFreePct != 9 || s.SwapPagesPerS != 40 {
				t.Errorf("memory facts off: ram=%v swap=%v", s.RAMFreePct, s.SwapPagesPerS)
			}
		}},
		{"lonely", func(t *testing.T, s Snapshot) {
			if s.External != nil {
				t.Errorf("External = %+v, want nil", s.External)
			}
			if s.ClientsSeen || s.ClientsMax != 1 {
				t.Errorf("ClientsMax = %d ClientsSeen = %v, want 1 false", s.ClientsMax, s.ClientsSeen)
			}
			if len(s.Units) != 3 {
				t.Errorf("len(Units) = %d, want 3", len(s.Units))
			}
			if _, ok := unitByID(s, "shelly"); ok {
				t.Error("shelly present in lonely")
			}
		}},
		{"many-browsers", func(t *testing.T, s Snapshot) {
			if s.ClientsMax != 7 || !s.ClientsSeen {
				t.Errorf("ClientsMax = %d ClientsSeen = %v, want 7 true", s.ClientsMax, s.ClientsSeen)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			state := Presets()[tt.preset]
			if state.Preset != tt.preset {
				t.Fatalf("Preset field = %q, want %q", state.Preset, tt.preset)
			}
			buf := marshalState(t, state)
			snap := NewFileProvider(writeState(t, buf)).Snapshot(time.Now())
			tt.check(t, snap)
		})
	}
}

func TestFileProviderServicesAndFixedUnits(t *testing.T) {
	snap := NewFileProvider(writeState(t, marshalState(t, Presets()["good"]))).Snapshot(time.Now())

	shelly, ok := unitByID(snap, "shelly")
	if !ok {
		t.Fatal("shelly missing")
	}
	if shelly.Fixed || shelly.RoundS == nil || *shelly.RoundS != 0.4 || shelly.MsgPerMin != 126 {
		t.Errorf("shelly = %+v, want service with round 0.4 and 126 msg/min", shelly)
	}
	if !shelly.HasRAM || shelly.RAMBytes != 22<<20 {
		t.Errorf("shelly RAM = %d, want 22 MB", shelly.RAMBytes)
	}

	dash, ok := unitByID(snap, DashboardUnit)
	if !ok {
		t.Fatal("dashboard unit missing")
	}
	if !dash.Fixed || dash.RoundS != nil || dash.MsgPerMin != 0 {
		t.Errorf("dashboard = %+v, want Fixed without round or messages", dash)
	}

	for i := 1; i < len(snap.Units); i++ {
		if snap.Units[i-1].ID > snap.Units[i].ID {
			t.Errorf("units not sorted at %d: %s before %s", i, snap.Units[i-1].ID, snap.Units[i].ID)
		}
	}
	if snap.External == nil || snap.External.Name != "Wärmepumpe" || snap.External.EveryS != 15 {
		t.Errorf("External = %+v, want Wärmepumpe every 15", snap.External)
	}
}

func TestFileProviderPresentFalseLeavesUnitOut(t *testing.T) {
	content := `{"measured_s": 600, "cores": 4, "cpu_pct": 10, "ram_free_pct": 50, "clients_max": 1,
		"units": {
			"shelly": {"present": false, "cpu_pct": 5, "ram_mb": 22, "round_s": 0.4, "msg_per_min": 1},
			"trucki": {"present": true, "cpu_pct": 3, "ram_mb": 18, "round_s": 1.2, "msg_per_min": 4}
		}}`
	snap := NewFileProvider(writeState(t, content)).Snapshot(time.Now())
	if _, ok := unitByID(snap, "shelly"); ok {
		t.Error("shelly with present false is in the snapshot")
	}
	if _, ok := unitByID(snap, "trucki"); !ok {
		t.Error("trucki with present true is missing")
	}
	if len(snap.Units) != 1 {
		t.Errorf("len(Units) = %d, want 1", len(snap.Units))
	}
}

func TestFileProviderTempNullIsUnknown(t *testing.T) {
	content := `{"measured_s": 600, "cores": 4, "cpu_pct": 10, "temp_c": null, "clients_max": 2}`
	snap := NewFileProvider(writeState(t, content)).Snapshot(time.Now())
	if snap.TempC != nil {
		t.Errorf("TempC = %v, want nil", *snap.TempC)
	}
	if !snap.ClientsSeen {
		t.Error("ClientsSeen = false with clients_max 2")
	}
}

func TestFileProviderMalformedJSONFallsBackToGood(t *testing.T) {
	snap := NewFileProvider(writeState(t, `{"cpu_pct": `)).Snapshot(time.Now())
	if snap.CPUP95 != Presets()["good"].CPUPct {
		t.Errorf("CPUP95 = %v, want the good preset", snap.CPUP95)
	}
}

func TestFileProviderEveryCallRereads(t *testing.T) {
	path := writeState(t, marshalState(t, Presets()["good"]))
	p := NewFileProvider(path)
	if got := p.Snapshot(time.Now()).CPUP95; got != 32 {
		t.Fatalf("first CPUP95 = %v, want 32", got)
	}
	if err := os.WriteFile(path, []byte(marshalState(t, Presets()["over"])), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := p.Snapshot(time.Now()).CPUP95; math.Abs(got-78) > 1e-9 {
		t.Errorf("second CPUP95 = %v, want 78 after the file changed", got)
	}
}

func TestPresetsAreFreshCopies(t *testing.T) {
	a := Presets()
	a["good"].Units["shelly"] = FileUnit{}
	delete(a["good"].Units, "trucki")
	b := Presets()
	if !b["good"].Units["shelly"].Present || !b["good"].Units["trucki"].Present {
		t.Error("changing one Presets() call leaked into the next")
	}
}

// marshalState encodes a scenario the way the console writes it.
func marshalState(t *testing.T, s FileState) string {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFileStateCarriesDashboardParts(t *testing.T) {
	content := `{"measured_s": 600, "cores": 4, "cpu_pct": 10, "clients_max": 1,
		"dashboard_parts": {"push": 0.45, "http": 0.15, "mqtt": 0.25, "node": 0.05, "background": 0.10},
		"gc_share": 0.12}`
	snap := NewFileProvider(writeState(t, content)).Snapshot(time.Now())
	if snap.Dashboard == nil {
		t.Fatal("Dashboard = nil, want the parts of the state file")
	}
	if got := snap.Dashboard.Shares[PartPush]; math.Abs(got-0.45) > 1e-9 {
		t.Errorf("Shares[push] = %v, want 0.45", got)
	}
	if got := snap.Dashboard.Shares[PartMQTT]; math.Abs(got-0.25) > 1e-9 {
		t.Errorf("Shares[mqtt] = %v, want 0.25", got)
	}
	if got := snap.Dashboard.GCShare; math.Abs(got-0.12) > 1e-9 {
		t.Errorf("GCShare = %v, want 0.12", got)
	}

	without := NewFileProvider(writeState(t, `{"measured_s": 600, "cores": 4, "cpu_pct": 10}`)).Snapshot(time.Now())
	if without.Dashboard != nil {
		t.Errorf("Dashboard = %+v without dashboard_parts, want nil", without.Dashboard)
	}
}
