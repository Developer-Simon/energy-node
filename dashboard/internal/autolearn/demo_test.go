package autolearn

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

// demoFixture is the path of a demo fixture from this package directory.
func demoFixture(name ...string) string {
	return filepath.Join(append([]string{"..", "..", "..", "demo", "fixtures"}, name...)...)
}

// demoRegistryDevices builds the devices the detectors see, from the discovery
// messages of the demo broker fixture. It follows learnDevices: the device ID is
// device.identifiers[0], the entities are the unique IDs, a switch component
// makes the device switchable.
func demoRegistryDevices(t *testing.T) map[string]DeviceInfo {
	t.Helper()
	raw, err := os.ReadFile(demoFixture("demo.json"))
	if err != nil {
		t.Fatalf("read demo broker fixture: %v", err)
	}
	var msgs []struct {
		Topic   string          `json:"topic"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &msgs); err != nil {
		t.Fatalf("parse demo broker fixture: %v", err)
	}
	type config struct {
		UniqueID string `json:"unique_id"`
		Device   struct {
			Identifiers []string `json:"identifiers"`
			Name        string   `json:"name"`
		} `json:"device"`
	}
	devices := map[string]DeviceInfo{}
	for _, m := range msgs {
		parts := strings.Split(m.Topic, "/")
		if len(parts) < 4 || parts[0] != "homeassistant" || parts[len(parts)-1] != "config" {
			continue
		}
		var c config
		if err := json.Unmarshal(m.Payload, &c); err != nil || len(c.Device.Identifiers) == 0 {
			continue
		}
		id := c.Device.Identifiers[0]
		info := devices[id]
		info.ID, info.Name = id, c.Device.Name
		info.Entities = append(info.Entities, c.UniqueID)
		if parts[1] == "switch" {
			info.Switchable = true
		}
		sort.Strings(info.Entities)
		devices[id] = info
	}
	return devices
}

// demoRulesRaw reads the demo rule document, which carries the demo location.
func demoRulesRaw(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(demoFixture("devices", "demo", "automation_rules.json"))
	if err != nil {
		t.Fatalf("read demo rules: %v", err)
	}
	return raw
}

// demoSeedInputFor seeds the demo days into dir, reads them back through the day
// store and builds the detector input from the registry devices and the demo rules.
func demoSeedInputFor(t *testing.T, dir string, now time.Time, loc *time.Location) *Input {
	t.Helper()
	if err := SeedDemo(dir, now, loc); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}
	store := OpenDayStore(dir, loc, keepAll, noLog)
	days, err := store.Days(now.AddDate(0, 0, -demoSeedDays), now.AddDate(0, 0, -1))
	if err != nil {
		t.Fatalf("Days: %v", err)
	}
	if len(days) != demoSeedDays {
		t.Fatalf("days = %d, want %d", len(days), demoSeedDays)
	}
	doc, err := ParseDoc(demoRulesRaw(t))
	if err != nil {
		t.Fatalf("parse demo rules: %v", err)
	}
	return &Input{Now: now, Loc: loc, Days: days, Doc: doc, Devices: demoRegistryDevices(t), Base: BuildBase(days, loc)}
}

// demoRunPerDevice runs a per-device detector on every device of the input and
// returns the candidates in device order.
func demoRunPerDevice(in *Input, d Detector) []Candidate {
	ids := make([]string, 0, len(in.Devices))
	for id := range in.Devices {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []Candidate
	for _, id := range ids {
		out = append(out, d.Run(context.Background(), in, id)...)
	}
	return out
}

func TestDemoSeedUsesRegisteredDevicesAndEntities(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	in := demoSeedInputFor(t, t.TempDir(), now, loc)
	for _, f := range in.Days {
		if !f.Fake {
			t.Fatalf("day %s is not marked fake", f.Day)
		}
		for id := range f.Devices {
			if _, ok := in.Devices[id]; !ok {
				t.Errorf("day %s has device %q that the registry does not know", f.Day, id)
			}
		}
		for _, ev := range f.Events {
			info, ok := in.Devices[ev.Device]
			if !ok {
				t.Errorf("event on unregistered device %q", ev.Device)
				continue
			}
			if !containsString(info.Entities, ev.Entity) {
				t.Errorf("event entity %q is not an entity of %s (%v)", ev.Entity, ev.Device, info.Entities)
			}
		}
	}
}

func TestDemoSeedYieldsFourFindings(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	in := demoSeedInputFor(t, t.TempDir(), now, loc)

	var off []Candidate
	for _, c := range demoRunPerDevice(in, SwitchTimeDetector) {
		if strings.HasPrefix(c.Finding.ID, "switch_time:"+demoPlugBalkon+":off:") {
			off = append(off, c)
		}
	}
	if len(off) != 1 {
		t.Errorf("switch_time off candidates of %s = %v, want one", demoPlugBalkon, ids(off))
	}
	rt := demoRunPerDevice(in, RuntimeDetector)
	if len(rt) != 1 || rt[0].Finding.Device != demoPlugDry {
		t.Errorf("runtime candidates = %v, want one for %s", ids(rt), demoPlugDry)
	}
	co := CouplingDetector.Run(context.Background(), in, "")
	if len(co) != 1 || !strings.Contains(co[0].Finding.ID, demoRelais) || !strings.Contains(co[0].Finding.ID, demoPlugBalkon) {
		t.Errorf("coupling candidates = %v, want one for %s and %s", ids(co), demoRelais, demoPlugBalkon)
	}
	ov := demoRunPerDevice(in, OverriddenDetector)
	if len(ov) != 1 || ov[0].Finding.Device != demoHeatPump || len(ov[0].Finding.RuleIDs) != 1 || ov[0].Finding.RuleIDs[0] != demoRuleID {
		t.Errorf("overridden candidates = %v, want one for rule %s on %s", ids(ov), demoRuleID, demoHeatPump)
	}
}

// demoEngineRun seeds the demo days into dir and runs the real engine over them
// at the daily slot, the way the dashboard does at 03:30.
func demoEngineRun(t *testing.T, dir string, now time.Time, loc *time.Location) *Engine {
	t.Helper()
	if err := SeedDemo(dir, now, loc); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}
	days := OpenDayStore(dir, loc, keepAll, noLog)
	raw := demoRulesRaw(t)
	devices := demoRegistryDevices(t)
	e := NewEngine(days, NewReactions(),
		func() ([]byte, error) { return raw, nil },
		func() map[string]DeviceInfo { return devices },
		func() learnrec.Limits { return learnrec.LimitsFor(learnrec.LevelBalanced, 4) },
		loc)
	for _, d := range []Detector{SwitchTimeDetector, RuntimeDetector, CouplingDetector, OverriddenDetector,
		AwayStandbyDetector, TemperatureDetector, LearnSwitchTimeDetector, LearnRuntimeDetector, LearnDayBoundsDetector, LearnHouseStateDetector} {
		e.Register(d)
	}
	season := LoadSeasonStore("", noLog)
	for _, w := range DemoSeason(now) {
		season.Put(w)
	}
	e.SetSeason(season)
	e.SetOutdoor(func() OutdoorSource {
		return OutdoorSource{EntityID: demoOutdoorEntity, Topic: "outstation/outdoor/state", ValueTemplate: "{{ value_json.temp }}"}
	})
	groups := map[string]bool{"habit": true, "energy": true}
	// The first tick only starts the engine, the daily round runs at 03:30.
	e.Tick(time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, loc), groups)
	for i := 0; i < 3; i++ {
		e.Tick(time.Date(now.Year(), now.Month(), now.Day(), 3, 30, 0, 0, loc), groups)
	}
	return e
}

func TestDemoSeedLearnsValuesThroughEngine(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	vals := demoEngineRun(t, t.TempDir(), now, loc).Values()

	off, ok := learnedValueFor(vals, "device/"+demoPlugBalkon+"/usual_off")
	if !ok {
		t.Fatalf("no usual_off of %s in %v", demoPlugBalkon, valueKeys(vals))
	}
	if off.Kind != ValueTime || math.Abs(float64(hhmmMinutes(t, off.Text)-demoPlugOffMin)) > 10 {
		t.Errorf("usual_off = %+v, want a time near %d minutes", off, demoPlugOffMin)
	}

	run, ok := learnedValueFor(vals, "device/"+demoPlugDry+"/usual_runtime_s")
	if !ok {
		t.Fatalf("no usual_runtime_s of %s in %v", demoPlugDry, valueKeys(vals))
	}
	if run.Kind != ValueDuration || math.Abs(run.Num-demoRunMinutes*60) > 120 {
		t.Errorf("usual_runtime_s = %+v, want about %d seconds", run, demoRunMinutes*60)
	}

	for _, key := range []string{"site/day_start", "site/night_rest"} {
		v, ok := learnedValueFor(vals, key)
		if !ok {
			t.Errorf("no %s in %v", key, valueKeys(vals))
			continue
		}
		if v.Kind != ValueTime {
			t.Errorf("%s = %+v, want a clock time", key, v)
		}
	}
}

func TestDemoSeedSuggestsAwayStandbyOnly(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	var away []string
	for _, c := range demoEngineRun(t, t.TempDir(), now, loc).Candidates() {
		if c.Finding.Kind == "away_standby" {
			away = append(away, c.Finding.Device)
		}
	}
	if len(away) != 1 || away[0] != demoHeatPump {
		t.Errorf("away_standby devices = %v, want only %s", away, demoHeatPump)
	}
}

// demoOutdoorEntity is the unique ID of the outdoor temperature of the demo.
const demoOutdoorEntity = "aussentemperatur"

// TestDemoSeedSuggestsSunOnHabit checks that the sunset habit of the balcony
// plug arises as a sun suggestion for the switch-on.
func TestDemoSeedSuggestsSunOnHabit(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	in := demoSeedInputFor(t, t.TempDir(), now, loc)
	var codes []string
	for _, c := range demoRunPerDevice(in, SwitchTimeDetector) {
		if c.Finding.Device == demoPlugBalkon && c.Finding.Summary.Code == "switch_time.sun_on" {
			return
		}
		codes = append(codes, c.Finding.Summary.Code)
	}
	t.Errorf("no switch_time.sun_on for %s, got codes %v", demoPlugBalkon, codes)
}

// TestDemoSeedSuggestsTemperatureLimit checks that the heat pump rule gets a
// temperature limit at the demo knee.
func TestDemoSeedSuggestsTemperatureLimit(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	var codes []string
	for _, c := range demoEngineRun(t, t.TempDir(), now, loc).Candidates() {
		codes = append(codes, c.Finding.Summary.Code)
		if c.Finding.Summary.Code == "temperature.limit" && c.Finding.Device == demoHeatPump {
			return
		}
	}
	t.Errorf("no temperature.limit for %s, got codes %v", demoHeatPump, codes)
}

// valueKeys lists the keys of learned values for failure messages.
func valueKeys(vals []Value) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = v.Key
	}
	return out
}

func TestDemoSeedWritesGzipDayFilesAndIsDeterministic(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	dirA, dirB := t.TempDir(), t.TempDir()
	if err := SeedDemo(dirA, now, loc); err != nil {
		t.Fatal(err)
	}
	if err := SeedDemo(dirB, now, loc); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dirA)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != demoSeedDays {
		t.Fatalf("files = %d, want %d", len(entries), demoSeedDays)
	}
	for _, e := range entries {
		a, err := os.ReadFile(filepath.Join(dirA, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dirB, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Fatalf("%s differs between two seeds", e.Name())
		}
	}
}

// containsString reports whether list holds s.
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// ids lists the finding IDs of candidates for failure messages.
func ids(cands []Candidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.Finding.ID
	}
	return out
}

func TestWithDemoHouseStateAddsQuietOnlyWhenMissing(t *testing.T) {
	got := WithDemoHouseState([]Value{{Key: "site/day_start", Kind: ValueTime, Text: "07:00"}})
	if len(got) != 2 || got[1].Key != "site/house_state" || got[1].Text != "quiet" || got[1].Kind != ValueState {
		t.Fatalf("demo house state missing: %+v", got)
	}
	real := []Value{{Key: "site/house_state", Kind: ValueState, Text: "active", N: 9}}
	if kept := WithDemoHouseState(real); len(kept) != 1 || kept[0].Text != "active" {
		t.Fatalf("learned house state replaced: %+v", kept)
	}
}
