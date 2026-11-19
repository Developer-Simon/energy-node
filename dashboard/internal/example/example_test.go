package example

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

const testSeed = `{
  "version": 1,
  "devices": {
    "apsystems_devices": [
      {"id": "example_pv", "name": "Beispiel PV-Wechselrichter", "host": "192.0.2.50"}
    ],
    "shelly_devices": [
      {"id": "example_grid", "name": "Beispiel Netzzähler", "host": "192.0.2.51", "generation": 1},
      {"id": "example_plug", "name": "Beispiel Steckdose", "host": "192.0.2.52", "generation": 2}
    ]
  },
  "scenario": {
    "id": "example_surplus", "name": "Beispiel Steckdose bei Überschuss", "enabled": true, "template": "surplus_switch",
    "when": {"conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 1500, "hysteresis": 0, "hold_seconds": 120}]},
    "on_start": {"actions": []},
    "on_end": {"actions": [{"type": "publish", "topic": "outstation/example_plug/relay/0/set", "retain": false, "payload_source": "constant", "payload": "OFF", "entity_id": "example_plug_relay"}]},
    "while": {"mode": "hold", "actions": [{"type": "publish", "topic": "outstation/example_plug/relay/0/set", "retain": false, "payload_source": "constant", "payload": "ON", "entity_id": "example_plug_relay"}], "repeat_seconds": 60},
    "until": {"conditions": [{"type": "balance_threshold", "field": "grid_import", "comparison": "above", "threshold": 100, "hysteresis": 0, "hold_seconds": 120}]}
  },
  "publish_prefix": "outstation/example_plug/relay/0/set",
  "assignments": {
    "example_pv_total_power": {"role": "pv"},
    "example_grid_power_l1": {"role": "grid"},
    "example_grid_power_l2": {"role": "grid"},
    "example_grid_power_l3": {"role": "grid"},
    "example_plug_relay": {"role": "switch"}
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
		cfg.Assignments["example_grid_power_l1"] = energy.Assignment{Role: energy.RoleLoad}
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
	if got := cfg.Assignments["example_grid_power_l1"].Role; got != energy.RoleLoad {
		t.Errorf("l1 Rolle = %q, want load (vorhandene Zuordnung bleibt)", got)
	}
	for id, role := range map[string]energy.Role{
		"example_grid_power_l2":  energy.RoleGrid,
		"example_grid_power_l3":  energy.RoleGrid,
		"example_pv_total_power": energy.RolePV,
		"example_plug_relay":     energy.RoleSwitch,
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
	if note.Version != 1 || note.PublishPrefix != "outstation/example_plug/relay/0/set" {
		t.Errorf("Notiz = %+v", note)
	}
	if len(note.Devices) != 2 || len(note.Assignments) != 5 || len(note.Scenario) == 0 {
		t.Errorf("Notiz unvollständig: devices=%d assignments=%d", len(note.Devices), len(note.Assignments))
	}
}

// seedParts liest die Geraete, das Szenario und die Zuordnungen aus dem Testseed.
type seedParts struct {
	Devices  map[string]json.RawMessage `json:"devices"`
	Scenario json.RawMessage            `json:"scenario"`
}

// writeConfigs schreibt die Konfigurationen mit den Beispiel-Teilen aus dem
// Seed. host ersetzt die Adresse der Steckdose, threshold den Schwellwert
// des Szenarios (leer heisst unveraendert), manual schaltet die
// Praefixe auf manuell. Die Schemas sind offen, damit Save jeden Stand annimmt.
func writeConfigs(t *testing.T, dir, host, threshold string, manual bool) *config.Manager {
	t.Helper()
	var seed seedParts
	if err := json.Unmarshal([]byte(testSeed), &seed); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"apsystems_devices", "shelly_devices", "automation_rules"} {
		if err := os.WriteFile(filepath.Join(dir, name+".schema.json"), []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	apsystems := string(seed.Devices["apsystems_devices"])
	shelly := deviceArray(t, seed.Devices["shelly_devices"], "example_plug", host)
	scenario := string(seed.Scenario)
	if threshold != "" {
		scenario = strings.Replace(scenario, `"threshold": 1500`, `"threshold": `+threshold, 1)
	}
	prefixes := `"publish_allowed_prefixes":["outstation/example_plug/relay/0/set"],"publish_allowed_prefixes_auto":true`
	if manual {
		prefixes = `"publish_allowed_prefixes":["outstation/example_plug/relay/0/set","manuell/topic"],"publish_allowed_prefixes_auto":false`
	}
	automation := `{"version":1,"settings":{"tick_interval_s":10,"settling_seconds":60,"balance_max_age_s":30,` +
		prefixes + `},"rules":[],"scenarios":[` + scenario + `]}`
	files := map[string]string{
		"apsystems_devices": apsystems,
		"shelly_devices":    shelly,
		"automation_rules":  automation,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(content), 0640); err != nil {
			t.Fatal(err)
		}
	}
	return config.NewManager(dir)
}

// deviceArray setzt bei dem Geraet editID die Adresse auf host. Ohne editID
// bleibt die Liste unveraendert.
func deviceArray(t *testing.T, raw json.RawMessage, editID, host string) string {
	t.Helper()
	var devices []map[string]any
	if err := json.Unmarshal(raw, &devices); err != nil {
		t.Fatal(err)
	}
	for _, device := range devices {
		if device["id"] == editID && host != "" {
			device["host"] = host
		}
	}
	out, err := json.Marshal(devices)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// readDocArray liest eine Geraeteliste aus den Konfigurationen.
func readDocArray(t *testing.T, configs *config.Manager, name string) []map[string]any {
	t.Helper()
	raw, err := configs.Read(name)
	if err != nil {
		t.Fatal(err)
	}
	var devices []map[string]any
	if err := json.Unmarshal(raw, &devices); err != nil {
		t.Fatal(err)
	}
	return devices
}

// readAutomationDoc liest die Automatisierungskonfiguration als Map.
func readAutomationDoc(t *testing.T, configs *config.Manager) map[string]any {
	t.Helper()
	raw, err := configs.Read("automation_rules")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// stopFixture legt Konfigurationen und Seed an und uebernimmt den Seed, so
// wie der Start es tut.
func stopFixture(t *testing.T, host, threshold string, manual bool) (string, *settings.Store, *config.Manager) {
	t.Helper()
	dir, st := newFixture(t)
	configs := writeConfigs(t, dir, host, threshold, manual)
	writeSeed(t, dir)
	if err := ApplySeed(dir, st); err != nil {
		t.Fatalf("ApplySeed: %v", err)
	}
	return dir, st, configs
}

func TestStopOnlyTurnsSimulationOff(t *testing.T) {
	dir, st, configs := stopFixture(t, "", "", false)
	var published []bool
	res, err := Stop(dir, false, configs, st, func(on bool) { published = append(published, on) })
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if len(res.Removed) != 0 || len(res.Kept) != 0 {
		t.Errorf("Result = %+v, want leer", res)
	}
	if len(published) != 1 || published[0] {
		t.Errorf("publish = %v, want [false]", published)
	}
	if _, err := os.Stat(filepath.Join(dir, NoteFileName)); err != nil {
		t.Fatalf("Notiz fehlt nach Stop ohne remove: %v", err)
	}
	if n := len(readDocArray(t, configs, "shelly_devices")); n != 2 {
		t.Errorf("shelly_devices = %d Geraete, want 2", n)
	}
	cfg, err := st.LoadMQTT()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SimulationActive {
		t.Error("SimulationActive nach Stop = true, want false")
	}
}

func TestStopRemoveDeletesUnchangedParts(t *testing.T) {
	dir, st, configs := stopFixture(t, "", "", false)
	var published []bool
	res, err := Stop(dir, true, configs, st, func(on bool) { published = append(published, on) })
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if len(published) != 1 || published[0] {
		t.Errorf("publish = %v, want [false]", published)
	}
	if len(res.Kept) != 0 {
		t.Errorf("Kept = %v, want leer", res.Kept)
	}
	if len(res.Removed) == 0 {
		t.Error("Removed leer, want Beispiel-Teile")
	}
	if n := len(readDocArray(t, configs, "apsystems_devices")); n != 0 {
		t.Errorf("apsystems_devices = %d Geraete, want 0", n)
	}
	if n := len(readDocArray(t, configs, "shelly_devices")); n != 0 {
		t.Errorf("shelly_devices = %d Geraete, want 0", n)
	}
	doc := readAutomationDoc(t, configs)
	if scenarios := doc["scenarios"].([]any); len(scenarios) != 0 {
		t.Errorf("Szenarien = %d, want 0", len(scenarios))
	}
	if prefixes := doc["settings"].(map[string]any)["publish_allowed_prefixes"].([]any); len(prefixes) != 0 {
		t.Errorf("publish_allowed_prefixes = %v, want leer", prefixes)
	}
	cfg, err := st.LoadEnergy()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"example_pv_total_power", "example_plug_relay"} {
		if _, ok := cfg.Assignments[id]; ok {
			t.Errorf("Zuordnung %s bleibt, want entfernt", id)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, NoteFileName)); !os.IsNotExist(err) {
		t.Errorf("Notiz nach vollem Entfernen noch vorhanden: %v", err)
	}
}

func TestStopRemoveKeepsEditedDevice(t *testing.T) {
	dir, st, configs := stopFixture(t, "10.0.0.7", "", false)
	res, err := Stop(dir, true, configs, st, func(bool) {})
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	shelly := readDocArray(t, configs, "shelly_devices")
	if len(shelly) != 1 || shelly[0]["id"] != "example_plug" || shelly[0]["host"] != "10.0.0.7" {
		t.Fatalf("shelly_devices = %v, want nur die geaenderte Steckdose", shelly)
	}
	note, err := LoadNote(dir)
	if err != nil || note == nil {
		t.Fatalf("LoadNote = %v, %v, want Notiz", note, err)
	}
	if len(note.Devices) != 1 || len(note.Devices["shelly_devices"]) != 1 {
		t.Fatalf("Notiz-Geraete = %v, want nur die Steckdose", note.Devices)
	}
	if !contains(res.Kept, "device:example_plug") {
		t.Errorf("Kept = %v, want device:example_plug", res.Kept)
	}
}

func TestStopRemoveKeepsEditedScenario(t *testing.T) {
	dir, st, configs := stopFixture(t, "", "2000", false)
	res, err := Stop(dir, true, configs, st, func(bool) {})
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if scenarios := readAutomationDoc(t, configs)["scenarios"].([]any); len(scenarios) != 1 {
		t.Fatalf("Szenarien = %d, want das geaenderte Szenario", len(scenarios))
	}
	note, err := LoadNote(dir)
	if err != nil || note == nil {
		t.Fatalf("LoadNote = %v, %v, want Notiz", note, err)
	}
	if len(note.Scenario) == 0 {
		t.Error("Notiz ohne Szenario, want es behalten")
	}
	if !contains(res.Kept, "scenario:example_surplus") {
		t.Errorf("Kept = %v, want scenario:example_surplus", res.Kept)
	}
}

func TestStopRemoveKeepsManualPrefixes(t *testing.T) {
	dir, st, configs := stopFixture(t, "", "", true)
	res, err := Stop(dir, true, configs, st, func(bool) {})
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	prefixes := readAutomationDoc(t, configs)["settings"].(map[string]any)["publish_allowed_prefixes"].([]any)
	if len(prefixes) != 2 {
		t.Errorf("publish_allowed_prefixes = %v, want beide Praefixe erhalten", prefixes)
	}
	note, err := LoadNote(dir)
	if err != nil || note == nil {
		t.Fatalf("LoadNote = %v, %v, want Notiz", note, err)
	}
	if note.PublishPrefix != "outstation/example_plug/relay/0/set" {
		t.Errorf("PublishPrefix = %q, want behalten", note.PublishPrefix)
	}
	if !contains(res.Kept, "publish_prefix:outstation/example_plug/relay/0/set") {
		t.Errorf("Kept = %v, want publish_prefix", res.Kept)
	}
}

func TestActive(t *testing.T) {
	dir, st, configs := stopFixture(t, "", "", false)
	if !Active(dir, st) {
		t.Fatal("Active nach dem Seed = false, want true")
	}
	if _, err := Stop(dir, false, configs, st, func(bool) {}); err != nil {
		t.Fatal(err)
	}
	if Active(dir, st) {
		t.Fatal("Active nach Stop = true, want false")
	}

	// Ohne Notiz ist die Simulation kein Beispiel, auch wenn sie laeuft.
	bare, bareSt := newFixture(t)
	cfg, err := bareSt.LoadMQTT()
	if err != nil {
		t.Fatal(err)
	}
	cfg.SimulationActive = true
	if err := bareSt.SaveMQTT(cfg); err != nil {
		t.Fatal(err)
	}
	if Active(bare, bareSt) {
		t.Fatal("Active ohne Notiz = true, want false")
	}
}

// TestApplySeedReadsBootstrapSeed prüft den Seed-Vertrag mit dem Bootstrap:
// die Datei, die example_apply.py schreibt, muss ApplySeed übernehmen. Der
// Pfad kommt aus EXAMPLE_SEED_FILE, scripts/tests/test_example_seed_roundtrip.sh
// setzt ihn. Ohne Umgebungsvariable wird der Test übersprungen.
func TestApplySeedReadsBootstrapSeed(t *testing.T) {
	src := os.Getenv("EXAMPLE_SEED_FILE")
	if src == "" {
		t.Skip("EXAMPLE_SEED_FILE nicht gesetzt")
	}
	if base := filepath.Base(src); base != SeedFileName {
		t.Fatalf("seed file written by bootstrap is called %q, the dashboard reads %q", base, SeedFileName)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("Seed lesen: %v", err)
	}
	dir, st := newFixture(t)
	if err := os.WriteFile(filepath.Join(dir, SeedFileName), data, 0640); err != nil {
		t.Fatal(err)
	}
	if err := ApplySeed(dir, st); err != nil {
		t.Fatalf("ApplySeed: %v", err)
	}
	note, err := LoadNote(dir)
	if err != nil {
		t.Fatalf("LoadNote: %v", err)
	}
	if note == nil {
		t.Fatal("LoadNote = nil, want Notiz aus dem Bootstrap-Seed")
	}
	if got := note.Assignments["example_pv_total_power"].Role; got != energy.RolePV {
		t.Errorf("example_pv_total_power Rolle = %q, want %q", got, energy.RolePV)
	}
}
