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
	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
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

// learnedEntriesOf gives the learned numbers of the values by key, the way the
// learned store hands them to the energy checks.
func learnedEntriesOf(vals []Value) map[string]LearnedEntry {
	out := make(map[string]LearnedEntry, len(vals))
	for _, v := range vals {
		out[v.Key] = LearnedEntry{Key: v.Key, Device: v.Device, Unit: v.Unit, Value: v.Num, N: v.N}
	}
	return out
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
		AwayStandbyDetector, TemperatureDetector, LearnSwitchTimeDetector, LearnRuntimeDetector, LearnDayBoundsDetector, LearnHouseStateDetector,
		LearnCyclesDetector, FeedbackDetector, CompetitionDetector, BatteryDetector, BaseLoadDetector} {
		e.Register(d)
	}
	// The energy checks read the operating powers the engine learned before them.
	e.SetLearned(func() map[string]LearnedEntry { return learnedEntriesOf(e.Values()) })
	season := LoadSeasonStore("", noLog)
	for _, w := range DemoSeason(now) {
		season.Put(w)
	}
	e.SetSeason(season)
	e.SetOutdoor(func() OutdoorSource {
		return OutdoorSource{EntityID: demoOutdoorEntity, Topic: "outstation/outdoor/state", ValueTemplate: "{{ value_json.temp }}"}
	})
	groups := map[string]bool{"habit": true, "energy": true, "reliability": true}
	// The first tick only starts the engine, the daily round runs at 03:30.
	e.Tick(time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, loc), groups)
	for i := 0; i < 3; i++ {
		e.Tick(time.Date(now.Year(), now.Month(), now.Day(), 3, 30, 0, 0, loc), groups)
	}
	// The energy checks read the learned operating powers of the day before, so
	// the next day's daily round and a light round after it see them.
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 3, 30, 0, 0, loc)
	e.Tick(next, groups)
	e.Tick(next.Add(time.Hour), groups)
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

// TestDemoSeedSuggestsTemperatureLimit checks that the rule that switches the
// heat pump on gets a temperature limit at the demo knee, and that the rule
// that switches it off does not.
func TestDemoSeedSuggestsTemperatureLimit(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	var codes []string
	onLimit, offLimit := false, false
	for _, c := range demoEngineRun(t, t.TempDir(), now, loc).Candidates() {
		codes = append(codes, c.Finding.Summary.Code)
		if c.Finding.Summary.Code != "temperature.limit" {
			continue
		}
		switch {
		case c.Finding.Device == demoHeatPump && containsString(c.Finding.RuleIDs, demoPVRuleID):
			onLimit = true
		case containsString(c.Finding.RuleIDs, demoRuleID):
			offLimit = true
		}
	}
	if !onLimit {
		t.Errorf("no temperature.limit for the switch on rule %s, got codes %v", demoPVRuleID, codes)
	}
	if offLimit {
		t.Errorf("temperature.limit for the switch off rule %s, want none", demoRuleID)
	}
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

// TestDemoOutdoorSensorIsInBrokerAndSettings checks that the demo broker
// publishes the outdoor temperature sensor and that the demo settings select it.
func TestDemoOutdoorSensorIsInBrokerAndSettings(t *testing.T) {
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
	var config struct {
		UniqueID    string `json:"unique_id"`
		StateTopic  string `json:"state_topic"`
		DeviceClass string `json:"device_class"`
		Unit        string `json:"unit_of_measurement"`
	}
	found, state := false, false
	for _, m := range msgs {
		if m.Topic == "homeassistant/sensor/aussensensor/temperature/config" {
			if err := json.Unmarshal(m.Payload, &config); err != nil {
				t.Fatalf("outdoor config: %v", err)
			}
			found = true
		}
		if m.Topic == "outstation/aussensensor/temperature" {
			state = true
		}
	}
	if !found || !state {
		t.Fatalf("broker fixture lacks the outdoor sensor (config %v, state %v)", found, state)
	}
	if config.UniqueID != demoOutdoorEntity || config.DeviceClass != "temperature" || config.Unit != "°C" {
		t.Errorf("outdoor config = %+v, want unique_id %s, device_class temperature, unit °C", config, demoOutdoorEntity)
	}

	rawSettings, err := os.ReadFile(demoFixture("seed", "demo", "settings.json"))
	if err != nil {
		t.Fatalf("read demo settings: %v", err)
	}
	var settings struct {
		Outdoor string `json:"automation_outdoor_entity"`
		Level   string `json:"learning_level"`
	}
	if err := json.Unmarshal(rawSettings, &settings); err != nil {
		t.Fatalf("parse demo settings: %v", err)
	}
	if settings.Outdoor != demoOutdoorEntity {
		t.Errorf("automation_outdoor_entity = %q, want %q", settings.Outdoor, demoOutdoorEntity)
	}
	if settings.Level != "eco" {
		t.Errorf("learning_level = %q, want eco, the level the next-level suggestion starts from", settings.Level)
	}
}

// TestDemoSeasonYearCourseOfBedtime checks that the demo season carries a learned
// bedtime for at least eight weeks, so the yearly line of the value shows.
func TestDemoSeasonYearCourseOfBedtime(t *testing.T) {
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, berlinLoc(t))
	weeks := DemoSeason(now)
	if len(weeks) != demoSeasonWeeks+2*(demoPriorWeeks+1) || demoSeasonWeeks < 20 {
		t.Fatalf("season weeks = %d, want %d (at least 20)", len(weeks), demoSeasonWeeks+2*(demoPriorWeeks+1))
	}
	count := 0
	for _, w := range weeks {
		if v, ok := w.Learned[demoBedtimeKey]; ok {
			count++
			if math.Abs(v-demoBedtimeMin) > 10 {
				t.Errorf("week %s learned bedtime %.1f, want about %.0f", w.Key, v, demoBedtimeMin)
			}
		}
	}
	if count < 8 {
		t.Errorf("weeks with a learned bedtime = %d, want at least 8", count)
	}
}

// TestDemoDailyShareSuggestsNextLevel checks that the demo learning state
// suggests the next level from eco, where the demo settings start.
func TestDemoDailyShareSuggestsNextLevel(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, loc)
	st := learnrec.State{DailyShare: DemoDailyShare(now)}
	if n := len(st.DailyShare); n != demoShareDays {
		t.Fatalf("daily share days = %d, want %d", n, demoShareDays)
	}
	next, ok := learnrec.SuggestUp(st, learnrec.LevelEco, now)
	if !ok || next != learnrec.LevelBalanced {
		t.Errorf("SuggestUp(eco) = %q, %v, want balanced", next, ok)
	}
}

// TestDemoSeedOverriddenSplitsBySun checks that the overridden check of the
// heat pump rule splits by the sun, with a sun window as the fix, and not by
// the clock.
func TestDemoSeedOverriddenSplitsBySun(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	in := demoSeedInputFor(t, t.TempDir(), now, loc)
	ov := demoRunPerDevice(in, OverriddenDetector)
	if len(ov) != 1 || len(ov[0].Finding.Apply) != 1 || len(ov[0].Finding.Apply[0].Ops) != 1 {
		t.Fatalf("overridden candidates = %v, want one with one add_condition op", ids(ov))
	}
	cond, _ := ov[0].Finding.Apply[0].Ops[0].Value.(map[string]any)
	if cond["type"] != "sun_window" {
		t.Errorf("overridden condition = %v, want a sun_window", cond)
	}
}

// TestDemoSeedHeatingSeasonIsVisible checks that the heat pump has a heating
// knee and that the recent outdoor temperature lies below it, so a finding of
// the heat pump carries the heating season and is shown.
func TestDemoSeedHeatingSeasonIsVisible(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	e := demoEngineRun(t, t.TempDir(), now, loc)
	knee, ok := e.HeatingKnee(demoHeatPump)
	if !ok {
		t.Fatalf("no heating knee for %s", demoHeatPump)
	}
	recent := e.RecentOutdoorC()
	if recent == nil {
		t.Fatalf("no recent outdoor temperature")
	}
	if *recent > knee {
		t.Errorf("recent outdoor %.1f °C is above the knee %.1f °C, the heating season would rest", *recent, knee)
	}
}

// TestDemoSeedClockAndSunHabitsContrast checks that the demo shows a sun off
// for the relay and a plain clock off for the balcony plug, next to the sun on.
func TestDemoSeedClockAndSunHabitsContrast(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	codes := map[string]string{}
	for _, c := range demoEngineRun(t, t.TempDir(), now, loc).Candidates() {
		if c.Finding.Kind == "switch_time" {
			codes[c.Finding.ID] = c.Finding.Summary.Code
		}
	}
	want := map[string]string{
		"switch_time:" + demoRelais + ":off:0:1830":     "switch_time.sun_off",
		"switch_time:" + demoPlugBalkon + ":off:0:2230": "switch_time.off",
		"switch_time:" + demoPlugBalkon + ":on:0:1700":  "switch_time.sun_on",
	}
	for id, code := range want {
		if codes[id] != code {
			t.Errorf("%s = %q, want %q (all: %v)", id, codes[id], code, codes)
		}
	}
}

// demoSolarInput seeds the demo days, adds the demo roof model and raster to the
// input and a copy of the last demo day as today, which the surplus needs.
func demoSolarInput(t *testing.T, now time.Time, loc *time.Location) (*Input, *Raster, PVModel) {
	t.Helper()
	in := demoSeedInputFor(t, t.TempDir(), now, loc)
	raster := DemoPVRaster(now, demoLat, demoLon)
	model := DemoPVModel(raster)
	in.PV = func() (PVModel, *Raster, bool) { return model, raster, true }
	return in, raster, model
}

func TestDemoShadeCyclesAndSurplus(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	in, raster, model := demoSolarInput(t, now, loc)
	if !model.Safe || len(model.Surfaces) == 0 {
		t.Fatalf("demo model = %+v, want a safe model with a surface", model)
	}
	ctx := context.Background()

	shade := NewShadeDetector(func() *Raster { return raster }, func() (PVModel, bool) { return model, true })
	var shades []string
	for _, c := range shade.Run(ctx, in, "") {
		shades = append(shades, c.Finding.ID)
	}
	t.Logf("shade findings: %v", shades)
	if len(shades) == 0 {
		t.Fatal("demo shows no shade")
	}

	cycles := LearnCyclesDetector.Run(ctx, in, demoWasherDevice)
	cw, ok := learnedValueFor(learnedValues(cycles), "device/"+demoWasherDevice+"/cycle_wh")
	if !ok {
		t.Fatal("demo gives no cycle_wh for the washing machine")
	}
	if cw.Num < 850 || cw.Num > 950 {
		t.Errorf("cycle_wh = %v, want 900 +- 50", cw.Num)
	}

	td := in.Days[len(in.Days)-1]
	td.Day = now.Format(dayLayout)
	in.Days = append(in.Days, td)
	wh, _, ok := SurplusLeft(in, now)
	t.Logf("cycle_wh %v, surplus left %v Wh", cw.Num, wh)
	if !ok || wh <= 0 {
		t.Errorf("surplus left = %v, ok %v, want a positive value", wh, ok)
	}
}

func TestSeedDemoPVWritesRasterAndModel(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	dir := t.TempDir()
	rasterPath := filepath.Join(dir, "pvraster.json")
	modelPath := filepath.Join(dir, "pvmodel.json")
	if err := SeedDemoPV(rasterPath, modelPath, now); err != nil {
		t.Fatalf("SeedDemoPV: %v", err)
	}
	state, err := os.ReadFile(rasterPath)
	if err != nil {
		t.Fatalf("read raster: %v", err)
	}
	r := LoadRaster(state, demoPVAzCellDeg, demoPVElCellDeg)
	if _, ok := r.Cell(180, 24); !ok {
		t.Error("loaded raster has no value at the noon sky")
	}
	m, ok := NewPVModelStore(modelPath).Model()
	if !ok || !m.Safe {
		t.Errorf("stored model = %+v, ok %v, want a safe model", m, ok)
	}
}

func TestSeedDemoTodayGivesSurplusLeft(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 11, 10, 0, 0, loc)
	dir := t.TempDir()
	in := demoSeedInputFor(t, dir, now, loc)
	if err := SeedDemoToday(dir, now, loc); err != nil {
		t.Fatalf("SeedDemoToday: %v", err)
	}
	store := OpenDayStore(dir, loc, keepAll, noLog)
	days, err := store.Days(now.AddDate(0, 0, -demoSeedDays), now)
	if err != nil {
		t.Fatalf("Days: %v", err)
	}
	if len(days) != demoSeedDays+1 || days[len(days)-1].Day != now.Format(dayLayout) {
		t.Fatalf("days = %d, last %q, want %d with today", len(days), days[len(days)-1].Day, demoSeedDays+1)
	}
	td := days[len(days)-1]
	if td.Site.Seen[43] == 0 || td.Site.Seen[44] != 0 || len(td.Events) != 0 {
		t.Errorf("today covers the wrong quarters or has events: seen[43]=%d seen[44]=%d events=%d",
			td.Site.Seen[43], td.Site.Seen[44], len(td.Events))
	}
	raster := DemoPVRaster(now, demoLat, demoLon)
	model := DemoPVModel(raster)
	in.PV = func() (PVModel, *Raster, bool) { return model, raster, true }
	in.Days = days
	wh, _, ok := SurplusLeft(in, now)
	if !ok || wh <= 0 {
		t.Errorf("surplus left = %v, ok %v, want a positive value", wh, ok)
	}
}

func TestSeedDemoTodaySkipsTheFirstQuarter(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	if err := SeedDemoToday(dir, time.Date(2026, 11, 20, 0, 10, 0, 0, loc), loc); err != nil {
		t.Fatal(err)
	}
	if empty, err := DaysDirEmpty(dir); err != nil || !empty {
		t.Errorf("dir empty = %v, err %v, want no file in the first quarter", empty, err)
	}
}

// TestDemoShadeCurveSkipsTheRunningDay checks that the curve of the shade
// finding shows a whole past day, not the cut day of today.
func TestDemoShadeCurveSkipsTheRunningDay(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 11, 10, 0, 0, loc)
	dir := t.TempDir()
	in := demoSeedInputFor(t, dir, now, loc)
	if err := SeedDemoToday(dir, now, loc); err != nil {
		t.Fatalf("SeedDemoToday: %v", err)
	}
	days, err := OpenDayStore(dir, loc, keepAll, noLog).Days(now.AddDate(0, 0, -demoSeedDays), now)
	if err != nil {
		t.Fatalf("Days: %v", err)
	}
	in.Days = days
	raster := DemoPVRaster(now, demoLat, demoLon)
	model := DemoPVModel(raster)
	shade := NewShadeDetector(func() *Raster { return raster }, func() (PVModel, bool) { return model, true })
	cands := shade.Run(context.Background(), in, "")
	if len(cands) == 0 {
		t.Fatal("demo shows no shade")
	}
	ev := cands[0].Finding.Evidence
	// 14:00 is quarter 56, after now, so only a past day has PV there.
	if len(ev.Quarters) != dayQuarters || ev.Quarters[56] <= 0 {
		t.Fatalf("shade curve has no afternoon PV, it shows the running day: %+v", ev)
	}
}

// TestDemoSeedYieldsEnergyFindings checks that the demo pipeline yields the
// energy findings: a feedback pair, two competing loads, a battery that is
// full early and empty before morning, and a rising base load.
func TestDemoSeedYieldsEnergyFindings(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	ids := map[string]bool{}
	kinds := map[string]bool{}
	for _, c := range demoEngineRun(t, t.TempDir(), now, loc).Candidates() {
		ids[c.Finding.ID] = true
		kinds[c.Finding.Kind] = true
	}
	for _, kind := range []string{"feedback", "competition", "baseload"} {
		if !kinds[kind] {
			t.Errorf("no %s finding in the demo pipeline, kinds %v", kind, kinds)
		}
	}
	for _, id := range []string{"battery:full", "battery:empty"} {
		if !ids[id] {
			t.Errorf("no %s finding in the demo pipeline, ids %v", id, ids)
		}
	}
	for _, c := range demoEngineRun(t, t.TempDir(), now, loc).Candidates() {
		if c.Finding.Kind == "baseload" && (c.Finding.Device != "" || c.Finding.Group != "check") {
			t.Errorf("baseload finding device %q group %q, want no culprit and group check", c.Finding.Device, c.Finding.Group)
		}
		if c.Finding.ID == "battery:empty" && c.Finding.Group != "check" {
			t.Errorf("battery:empty group %q, want check without culprit", c.Finding.Group)
		}
	}
}

// TestDemoSeedKeepsTheHabitSuggestions checks the suggestions the demo had
// before the energy findings: run time of the dehumidifier and the away standby.
func TestDemoSeedKeepsTheHabitSuggestions(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	ids := map[string]bool{}
	for _, c := range demoEngineRun(t, t.TempDir(), now, loc).Candidates() {
		ids[c.Finding.ID] = true
	}
	for _, id := range []string{"runtime:plug_entfeuchter", "away_standby:waermepumpe"} {
		if !ids[id] {
			t.Errorf("no %s in the demo pipeline, ids %v", id, ids)
		}
	}
}

// TestDemoPVYearRestsTheFeedInRule checks that the demo year raster knows the
// summer sun and that the feed-in rule of the demo rests until a spring week.
func TestDemoPVYearRestsTheFeedInRule(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	r := DemoPVRasterYear(now, demoLat, demoLon)
	m := DemoPVModel(r)
	if !m.Safe || !rasterCoversYear(r, sun.Location{Lat: demoLat, Lon: demoLon}, now) {
		t.Fatalf("model %+v safe %v, raster must cover the year", m, m.Safe)
	}
	doc, err := ParseDoc(demoRulesRaw(t))
	if err != nil {
		t.Fatal(err)
	}
	st := neverFiredStateFor("grid_export", now.Add(-40*24*time.Hour), 900)
	// The observer knows the feed-in rule for 40 days, it never fired.
	st.Rules["regel_einspeisung_entfeuchter"] = RuleStats{FirstSeen: now.Add(-40 * 24 * time.Hour), Hours: map[int64]int{}}
	src := func() (PVModel, *Raster, bool) { return m, r, true }
	var got *Finding
	for _, f := range ObservedFindingsWithPV(doc, st, nil, src, now, DefaultThresholds()) {
		if f.ID == FindingID("never_fired", "regel_einspeisung_entfeuchter") {
			f := f
			got = &f
		}
	}
	if got == nil || got.Summary.Code != "check.never_fired_season" {
		t.Fatalf("feed-in rule finding = %+v, want check.never_fired_season", got)
	}
	week, _ := got.Summary.Params["week"].(int)
	if week < 5 || week > 20 {
		t.Fatalf("week = %v, want a spring week", got.Summary.Params["week"])
	}
}
