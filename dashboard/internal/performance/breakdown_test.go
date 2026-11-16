package performance

import (
	"math"
	"testing"
)

func breakdownPct(t *testing.T, got []PartPct, id string) float64 {
	t.Helper()
	for _, p := range got {
		if p.ID == id {
			return p.Pct
		}
	}
	t.Fatalf("no entry %q in %v", id, got)
	return 0
}

func breakdownNear(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestDashboardBreakdownSumsToUnit(t *testing.T) {
	snap := Snapshot{
		Units: []UnitStats{{ID: DashboardUnit, CPUP95: 10, Fixed: true}},
		Dashboard: &DashboardSplit{
			GCShare: 0.2,
			Shares:  map[Part]float64{PartPush: 0.5, PartMQTT: 0.5},
		},
	}
	got := DashboardBreakdown(snap)

	if push := breakdownPct(t, got, "push"); !breakdownNear(push, 4) {
		t.Errorf("push = %v, want 4", push)
	}
	if gc := breakdownPct(t, got, "gc"); !breakdownNear(gc, 2) {
		t.Errorf("gc = %v, want 2", gc)
	}
	var sum float64
	for _, p := range got {
		sum += p.Pct
	}
	if !breakdownNear(sum, 10) {
		t.Errorf("sum = %v, want 10", sum)
	}
	if got[len(got)-1].ID != "gc" {
		t.Errorf("gc must come last, got order %v", got)
	}
}

func TestDashboardBreakdownNilWithoutSplit(t *testing.T) {
	snap := Snapshot{Units: []UnitStats{{ID: DashboardUnit, CPUP95: 10, Fixed: true}}}
	if got := DashboardBreakdown(snap); got != nil {
		t.Errorf("without split = %v, want nil", got)
	}
}

func TestParseAutomationSkipsDisabled(t *testing.T) {
	raw := []byte(`{
		"rules": [
			{"id": "r1", "name": "Heizung", "enabled": true},
			{"id": "r2", "name": "Alt", "enabled": false}
		],
		"scenarios": [
			{"id": "s1", "name": "Aus", "enabled": false, "while": {"regulator": {}}},
			{"id": "s2", "name": "Ein", "enabled": true, "while": {"regulator": null}}
		]
	}`)
	got, err := ParseAutomation(raw)
	if err != nil {
		t.Fatalf("ParseAutomation: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(got), got)
	}
	if got[0].ID != "r1" || got[0].Kind != "rule" || got[0].Regulator {
		t.Errorf("entry 0 = %+v, want rule r1 without regulator", got[0])
	}
	if got[1].ID != "s2" || got[1].Kind != "scenario" || got[1].Regulator {
		t.Errorf("entry 1 = %+v, want scenario s2 without regulator", got[1])
	}
	if HasRegulator(got) {
		t.Errorf("HasRegulator = true, want false without an active regulator")
	}
}

func TestParseAutomationNameFallsBackToID(t *testing.T) {
	raw := []byte(`{
		"rules": [{"id": "r7", "name": "", "enabled": true}],
		"scenarios": [{"id": "s3", "enabled": true, "while": {"regulator": {"output": {"topic": "x"}}}}]
	}`)
	got, err := ParseAutomation(raw)
	if err != nil {
		t.Fatalf("ParseAutomation: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Name != "r7" {
		t.Errorf("rule name = %q, want r7", got[0].Name)
	}
	if got[1].Name != "s3" {
		t.Errorf("scenario name = %q, want s3", got[1].Name)
	}
	if !got[1].Regulator || !HasRegulator(got) {
		t.Errorf("scenario with regulator object not detected: %+v", got[1])
	}
}

func TestAutomationBreakdownSplitsEvenly(t *testing.T) {
	rows := []Row{
		{ID: RowTick, CostPct: 6, Present: true},
		{ID: RowRegulator, CostPct: 3, Present: true},
	}
	entries := []AutomationEntry{
		{ID: "r1", Name: "A", Kind: "rule"},
		{ID: "r2", Name: "B", Kind: "rule"},
		{ID: "s1", Name: "C", Kind: "scenario", Regulator: true},
	}
	got := AutomationBreakdown(rows, entries, 6)

	if len(got) != 2 {
		t.Fatalf("got %d groups, want 2: %+v", len(got), got)
	}
	tick, reg := got[0], got[1]
	if tick.Row != RowTick || !breakdownNear(tick.Pct, 6) || len(tick.Items) != 3 {
		t.Fatalf("tick group = %+v", tick)
	}
	for _, it := range tick.Items {
		if !breakdownNear(it.Pct, 2) {
			t.Errorf("tick item %s = %v, want 2", it.ID, it.Pct)
		}
	}
	if reg.Row != RowRegulator || !breakdownNear(reg.Pct, 3) || len(reg.Items) != 1 {
		t.Fatalf("regulator group = %+v", reg)
	}
	if reg.Items[0].ID != "s1" || !breakdownNear(reg.Items[0].Pct, 3) {
		t.Errorf("regulator item = %+v, want s1 at 3", reg.Items[0])
	}
}

func TestAutomationBreakdownFoldsRest(t *testing.T) {
	rows := []Row{
		{ID: RowTick, CostPct: 9, Present: true},
		{ID: RowRegulator, CostPct: 0, Present: false},
	}
	var entries []AutomationEntry
	for _, id := range []string{"r0", "r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8"} {
		entries = append(entries, AutomationEntry{ID: id, Name: id, Kind: "rule"})
	}
	got := AutomationBreakdown(rows, entries, 6)

	if len(got) != 1 {
		t.Fatalf("got %d groups, want 1 (regulator row absent): %+v", len(got), got)
	}
	g := got[0]
	if len(g.Items) != 6 {
		t.Errorf("items = %d, want 6", len(g.Items))
	}
	if g.More != 3 {
		t.Errorf("More = %d, want 3", g.More)
	}
	if !breakdownNear(g.MorePct, 3) {
		t.Errorf("MorePct = %v, want 3", g.MorePct)
	}
}
