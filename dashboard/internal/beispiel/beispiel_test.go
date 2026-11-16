package beispiel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

const testSeed = `{
  "version": 1,
  "devices": {
    "apsystems_devices": [
      {"id": "beispiel_pv", "name": "Beispiel PV-Wechselrichter", "host": "192.0.2.50"}
    ],
    "shelly_devices": [
      {"id": "beispiel_netz", "name": "Beispiel Netzzähler", "host": "192.0.2.51", "generation": 1},
      {"id": "beispiel_steckdose", "name": "Beispiel Steckdose", "host": "192.0.2.52", "generation": 2}
    ]
  },
  "scenario": {"id": "beispiel_ueberschuss", "name": "Beispiel Steckdose bei Überschuss", "enabled": true, "template": "surplus_switch"},
  "publish_prefix": "outstation/beispiel_steckdose/relay/0/set",
  "assignments": {
    "beispiel_pv_total_power": {"role": "pv"},
    "beispiel_netz_power_l1": {"role": "grid"},
    "beispiel_netz_power_l2": {"role": "grid"},
    "beispiel_netz_power_l3": {"role": "grid"},
    "beispiel_steckdose_relay": {"role": "switch"}
  }
}`

func newFixture(t *testing.T) (string, *settings.Store) {
	t.Helper()
	dir := t.TempDir()
	return dir, settings.NewStore(dir)
}

func writeSeed(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, SeedFileName), []byte(testSeed), 0640); err != nil {
		t.Fatal(err)
	}
}

func TestApplySeedWithoutFileIsNoop(t *testing.T) {
	dir, st := newFixture(t)
	if err := ApplySeed(dir, st); err != nil {
		t.Fatalf("ApplySeed ohne Datei: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, NoteFileName)); !os.IsNotExist(err) {
		t.Fatalf("Notiz ohne Seed angelegt: %v", err)
	}
	note, err := LoadNote(dir)
	if err != nil || note != nil {
		t.Fatalf("LoadNote ohne Notiz = %v, %v, want nil, nil", note, err)
	}
}

func TestApplySeedSetsAssignmentsOnlyWhenUnset(t *testing.T) {
	dir, st := newFixture(t)
	if _, err := st.PatchEnergy(func(cfg *settings.EnergyConfig) error {
		cfg.Assignments["beispiel_netz_power_l1"] = energy.Assignment{Role: energy.RoleLoad}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	writeSeed(t, dir)

	if err := ApplySeed(dir, st); err != nil {
		t.Fatalf("ApplySeed: %v", err)
	}
	cfg, err := st.LoadEnergy()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Assignments["beispiel_netz_power_l1"].Role; got != energy.RoleLoad {
		t.Errorf("l1 Rolle = %q, want load (vorhandene Zuordnung bleibt)", got)
	}
	for id, role := range map[string]energy.Role{
		"beispiel_netz_power_l2":   energy.RoleGrid,
		"beispiel_netz_power_l3":   energy.RoleGrid,
		"beispiel_pv_total_power":  energy.RolePV,
		"beispiel_steckdose_relay": energy.RoleSwitch,
	} {
		if got := cfg.Assignments[id].Role; got != role {
			t.Errorf("%s Rolle = %q, want %q", id, got, role)
		}
	}
}

func TestApplySeedTurnsSimulationOn(t *testing.T) {
	dir, st := newFixture(t)
	writeSeed(t, dir)
	if err := ApplySeed(dir, st); err != nil {
		t.Fatalf("ApplySeed: %v", err)
	}
	cfg, err := st.LoadMQTT()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SimulationActive {
		t.Fatal("SimulationActive nach dem Seed = false, want true")
	}
}

func TestApplySeedDoesNotNeedMQTT(t *testing.T) {
	dir, st := newFixture(t)
	writeSeed(t, dir)
	if err := ApplySeed(dir, st); err != nil {
		t.Fatalf("ApplySeed: %v", err)
	}
	// Ein frischer Store liest den gespeicherten Stand von der Platte.
	cfg, err := settings.NewStore(dir).LoadMQTT()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SimulationActive {
		t.Fatal("SimulationActive wurde nicht gespeichert")
	}
}

func TestApplySeedAppendsTilesToSavedLayout(t *testing.T) {
	dir, st := newFixture(t)
	if err := st.SaveLayout(settings.Layout{Version: 1, Pages: []settings.Page{{
		ID: "start", Name: "Start", Order: 0,
		Groups: []settings.Group{{ID: "g1", Name: "Geräte", Items: []settings.Item{}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	writeSeed(t, dir)
	if err := ApplySeed(dir, st); err != nil {
		t.Fatalf("ApplySeed: %v", err)
	}
	layout, err := st.LoadLayout()
	if err != nil {
		t.Fatal(err)
	}
	items := layout.Pages[0].Groups[0].Items
	if len(items) != 3 {
		t.Fatalf("Kacheln = %d, want 3", len(items))
	}
	for _, item := range items {
		if item.Type != "device" || item.Span != "1" || !item.Visible || item.ID != "device:"+item.Ref {
			t.Errorf("Kachel = %+v, want device mit Span 1 und sichtbar", item)
		}
	}
}

func TestApplySeedLeavesDefaultLayoutAlone(t *testing.T) {
	dir, st := newFixture(t)
	writeSeed(t, dir)
	if err := ApplySeed(dir, st); err != nil {
		t.Fatalf("ApplySeed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "layout.json")); !os.IsNotExist(err) {
		t.Fatalf("layout.json ohne gespeichertes Layout angelegt: %v", err)
	}
}

func TestApplySeedRenamesSeedToNote(t *testing.T) {
	dir, st := newFixture(t)
	writeSeed(t, dir)
	if err := ApplySeed(dir, st); err != nil {
		t.Fatalf("ApplySeed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, SeedFileName)); !os.IsNotExist(err) {
		t.Fatalf("Seed nach dem Start noch vorhanden: %v", err)
	}
	note, err := LoadNote(dir)
	if err != nil {
		t.Fatalf("LoadNote: %v", err)
	}
	if note == nil {
		t.Fatal("LoadNote = nil, want Notiz")
	}
	if note.Version != 1 || note.PublishPrefix != "outstation/beispiel_steckdose/relay/0/set" {
		t.Errorf("Notiz = %+v", note)
	}
	if len(note.Devices) != 2 || len(note.Assignments) != 5 || len(note.Scenario) == 0 {
		t.Errorf("Notiz unvollständig: devices=%d assignments=%d", len(note.Devices), len(note.Assignments))
	}
}
