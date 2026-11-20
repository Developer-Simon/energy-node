package autolearn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
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
	raw, err := os.ReadFile(demoFixture("devices", "demo", "automation_rules.json"))
	if err != nil {
		t.Fatalf("read demo rules: %v", err)
	}
	doc, err := ParseDoc(raw)
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

	sw := demoRunPerDevice(in, SwitchTimeDetector)
	if len(sw) != 1 || !strings.HasPrefix(sw[0].Finding.ID, "switch_time:"+demoPlugBalkon+":off:") {
		t.Errorf("switch_time candidates = %v, want one off-time for %s", ids(sw), demoPlugBalkon)
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
