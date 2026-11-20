package autolearn

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// demoSeedInputFor seeds the demo days into a fresh directory, reads them back
// through the day store and builds the detector input with the demo rule document.
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
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "demo", "fixtures", "devices", "demo", "automation_rules.json"))
	if err != nil {
		t.Fatalf("read demo rules: %v", err)
	}
	doc, err := ParseDoc(raw)
	if err != nil {
		t.Fatalf("parse demo rules: %v", err)
	}
	devices := map[string]DeviceInfo{}
	for _, id := range []string{demoPump, demoHeater, demoWasher, demoBoiler} {
		devices[id] = DeviceInfo{ID: id, Name: id, Switchable: true, Entities: []string{"switch." + id}}
	}
	return &Input{Now: now, Loc: loc, Days: days, Doc: doc, Devices: devices, Base: BuildBase(days, loc)}
}

// demoRunPerDevice runs a per-device detector on every demo device and returns
// the candidates in device order.
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

func TestDemoSeedYieldsFourFindings(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	in := demoSeedInputFor(t, t.TempDir(), now, loc)

	sw := demoRunPerDevice(in, SwitchTimeDetector)
	if len(sw) != 1 || !strings.HasPrefix(sw[0].Finding.ID, "switch_time:"+demoPump+":off:") {
		t.Errorf("switch_time candidates = %+v, want one off-time for %s", ids(sw), demoPump)
	}
	rt := demoRunPerDevice(in, RuntimeDetector)
	if len(rt) != 1 || rt[0].Finding.Device != demoHeater {
		t.Errorf("runtime candidates = %+v, want one for %s", ids(rt), demoHeater)
	}
	co := CouplingDetector.Run(context.Background(), in, "")
	if len(co) != 1 || !strings.Contains(co[0].Finding.ID, demoWasher) || !strings.Contains(co[0].Finding.ID, demoBoiler) {
		t.Errorf("coupling candidates = %+v, want one for %s and %s", ids(co), demoWasher, demoBoiler)
	}
	ov := demoRunPerDevice(in, OverriddenDetector)
	if len(ov) != 1 || ov[0].Finding.Device != demoPump || len(ov[0].Finding.RuleIDs) != 1 || ov[0].Finding.RuleIDs[0] != demoRuleID {
		t.Errorf("overridden candidates = %+v, want one for rule %s on %s", ids(ov), demoRuleID, demoPump)
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
		if !bytes.Equal(a, b) {
			t.Fatalf("%s differs between two seeds", e.Name())
		}
	}
}

// ids lists the finding IDs of candidates for failure messages.
func ids(cands []Candidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.Finding.ID
	}
	return out
}
