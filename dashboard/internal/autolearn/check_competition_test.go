package autolearn

import (
	"context"
	"testing"
	"time"
)

// compRule switches the entity on when the export is above x.
func compRule(id, entity string, x float64) Rule {
	return Rule{ID: id, Name: id, Enabled: true,
		Conditions: []Condition{{Type: "balance_threshold", Field: "grid_export", Comparison: "above", Threshold: x}},
		Actions:    []Action{{Type: "publish", EntityID: entity}}}
}

// compDevices are two switchable loads: plug.a with 1500 W, plug.b with 2000 W.
func compDevices() map[string]DeviceInfo {
	return map[string]DeviceInfo{
		"plug.a": {ID: "plug.a", Name: "Waschmaschine", Switchable: true, Entities: []string{"switch.plug.a"}},
		"plug.b": {ID: "plug.b", Name: "Trockner", Switchable: true, Entities: []string{"switch.plug.b"}},
	}
}

// compLearned gives both loads their operating power.
func compLearned() map[string]LearnedEntry {
	return map[string]LearnedEntry{
		"device/plug.a/op_power_w": {Key: "device/plug.a/op_power_w", Device: "plug.a", Kind: ValueNumber, Unit: "W", Value: 1500.0, N: 10},
		"device/plug.b/op_power_w": {Key: "device/plug.b/op_power_w", Device: "plug.b", Kind: ValueNumber, Unit: "W", Value: 2000.0, N: 10},
	}
}

// compWeek builds 14 days. The export alternates between 1000 and 1200 W, so the
// noise is 100 W and the reserve 200 W. On the first fireDays both rules a and b
// fire in quarter 40 (10:00) with the export at fireExport, and the import in the
// two quarters after is 2500 W. With bLater set, b fires in quarter 60 instead.
func compWeek(loc *time.Location, fireDays int, fireExport float32, bLater bool) []DayFile {
	var days []DayFile
	for d := 0; d < 14; d++ {
		day := relDay(fbDay(loc, 10+d))
		for q := 0; q < dayQuarters; q++ {
			day.Site.Seen[q] = 1
			day.Site.GridExport[q] = 1000
			if q%2 == 1 {
				day.Site.GridExport[q] = 1200
			}
		}
		if d < fireDays {
			day.Site.GridExport[40] = fireExport
			day.Site.GridImport[41] = 2500
			day.Site.GridImport[42] = 2500
			relRuleAction(&day, relAt(fbDay(loc, 10+d), 10, 0, 5), "plug.a", "a", true, true)
			bAt := relAt(fbDay(loc, 10+d), 10, 0, 20)
			if bLater {
				bAt = relAt(fbDay(loc, 10+d), 15, 0, 20)
			}
			relRuleAction(&day, bAt, "plug.b", "b", true, true)
		}
		days = append(days, day)
	}
	// One finished cycle of plug.a makes its load a fixed cycle energy.
	days[0].Cycles = []CycleRec{{Device: "plug.a", Start: relAt(fbDay(loc, 10), 9, 0, 0), DurS: 3600, Wh: 1500}}
	return days
}

func TestCompetitionStaircaseOfTwoRules(t *testing.T) {
	loc := berlinLoc(t)
	in := relInput(loc, compWeek(loc, 6, 4000, false), compDevices(),
		compRule("a", "switch.plug.a", 1800), compRule("b", "switch.plug.b", 1800))
	in.Learned = compLearned()
	got := competitionCandidates(in)
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1", len(got))
	}
	f := got[0].Finding
	if f.ID != "competition:grid_export" || f.Kind != "competition" || f.Group != "check" || f.Severity != "info" {
		t.Errorf("finding = id %q kind %q group %q severity %q", f.ID, f.Kind, f.Group, f.Severity)
	}
	if f.Summary.Code != "check.competition" || f.Device != "" {
		t.Errorf("summary %q device %q", f.Summary.Code, f.Device)
	}
	if len(f.RuleIDs) != 2 || f.RuleIDs[0] != "a" || f.RuleIDs[1] != "b" {
		t.Errorf("rule ids = %v, want [a b]", f.RuleIDs)
	}
	// The fixed cycle energy of plug.a puts it first: 1500 + 200 and 1500 + 2000 + 200.
	if len(f.Evidence.Marks) != 2 || f.Evidence.Marks[0].Value != 1700 || f.Evidence.Marks[1].Value != 3700 {
		t.Fatalf("marks = %+v, want 1700 and 3700", f.Evidence.Marks)
	}
	for _, m := range f.Evidence.Marks {
		if m.Dormant {
			t.Errorf("mark %v is dormant, want reached on 6 days", m.Value)
		}
	}
	if len(f.Apply) != 2 || f.Apply[0].Code != "stagger" || f.Apply[1].Code != "open_rule" || f.Apply[1].RuleID != "a" {
		t.Fatalf("apply = %+v, want stagger then open_rule on a", f.Apply)
	}
	rules := f.Apply[0].Rules
	if len(rules) != 2 || rules[0].RuleID != "a" || rules[1].RuleID != "b" {
		t.Fatalf("stagger rules = %+v, want a then b", rules)
	}
	if rules[0].Ops[0].Value != 1700.0 || rules[1].Ops[0].Value != 3700.0 {
		t.Errorf("stagger values = %v and %v, want 1700 and 3700", rules[0].Ops[0].Value, rules[1].Ops[0].Value)
	}
	if rules[0].Ops[0].Path[1] != 0 || rules[0].Ops[0].Path[2] != "threshold" {
		t.Errorf("path = %v, want conditions 0 threshold", rules[0].Ops[0].Path)
	}
}

func TestCompetitionMarksDormantStage(t *testing.T) {
	loc := berlinLoc(t)
	// The export only reaches 2000 W when the rules fire: the first stage is reached, the second is not.
	in := relInput(loc, compWeek(loc, 6, 2000, false), compDevices(),
		compRule("a", "switch.plug.a", 1800), compRule("b", "switch.plug.b", 1800))
	in.Learned = compLearned()
	got := competitionCandidates(in)
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1", len(got))
	}
	marks := got[0].Finding.Evidence.Marks
	if len(marks) != 2 || marks[0].Dormant || !marks[1].Dormant {
		t.Fatalf("marks = %+v, want the first reached and the second dormant", marks)
	}
}

// Review Focus 2: two rules on the same surplus that never fire together give no finding,
// even though the sum of their loads is above the surplus.
func TestCompetitionNoFindingWhenRulesNeverFireTogether(t *testing.T) {
	loc := berlinLoc(t)
	in := relInput(loc, compWeek(loc, 6, 4000, true), compDevices(),
		compRule("a", "switch.plug.a", 1800), compRule("b", "switch.plug.b", 1800))
	in.Learned = compLearned()
	if got := competitionCandidates(in); len(got) != 0 {
		t.Fatalf("candidates = %+v, want none when the rules never fire in the same quarter hour", got)
	}
}

func TestCompetitionNeedsThreeDays(t *testing.T) {
	loc := berlinLoc(t)
	in := relInput(loc, compWeek(loc, 2, 4000, false), compDevices(),
		compRule("a", "switch.plug.a", 1800), compRule("b", "switch.plug.b", 1800))
	in.Learned = compLearned()
	if got := competitionCandidates(in); len(got) != 0 {
		t.Fatalf("candidates = %+v, want none on 2 days", got)
	}
}

func TestCompetitionSingleRuleGivesNothing(t *testing.T) {
	loc := berlinLoc(t)
	in := relInput(loc, compWeek(loc, 6, 4000, false), compDevices(),
		compRule("a", "switch.plug.a", 1800))
	in.Learned = compLearned()
	if got := competitionCandidates(in); len(got) != 0 {
		t.Fatalf("candidates = %+v, want none for one rule", got)
	}
}

func TestCompetitionDetectorRunsDailyInReliability(t *testing.T) {
	if CompetitionDetector.Name != "competition" || CompetitionDetector.Group != "reliability" || CompetitionDetector.Light {
		t.Fatalf("detector = %+v, want daily competition in reliability", CompetitionDetector)
	}
	loc := berlinLoc(t)
	in := relInput(loc, compWeek(loc, 6, 4000, false), compDevices(),
		compRule("a", "switch.plug.a", 1800), compRule("b", "switch.plug.b", 1800))
	in.Learned = compLearned()
	if got := CompetitionDetector.Run(context.Background(), in, ""); len(got) != 1 {
		t.Fatalf("run = %d candidates, want 1", len(got))
	}
}
