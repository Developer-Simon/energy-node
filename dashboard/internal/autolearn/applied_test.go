package autolearn

import (
	"testing"
)

func appliedDoc(t *testing.T, conditions string) Doc {
	t.Helper()
	raw := `{"rules":[{"id":"r1","name":"Heizung","enabled":true,"conditions":[` + conditions + `],"actions":[]}],"scenarios":[]}`
	doc, err := ParseDoc([]byte(raw))
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	return doc
}

func addConditionFix(value map[string]any) Fix {
	return Fix{Code: "add_condition", RuleID: "r1", Ops: []Op{{Path: []any{"conditions", 0}, Value: value}}}
}

func TestFixAppliedIgnoresSlightlyDifferentValue(t *testing.T) {
	doc := appliedDoc(t, `{"type":"entity_value","topic":"t/out","entity_id":"sensor.out","comparison":"below","value":15.4}`)
	fix := addConditionFix(map[string]any{"type": "entity_value", "topic": "t/out", "entity_id": "sensor.out", "comparison": "below", "value": 15.0})
	if !FixApplied(fix, doc) {
		t.Fatal("condition with another value not recognised as applied")
	}
}

func TestFixAppliedNeedsSameComparison(t *testing.T) {
	doc := appliedDoc(t, `{"type":"entity_value","topic":"t/out","entity_id":"sensor.out","comparison":"above","value":15}`)
	fix := addConditionFix(map[string]any{"type": "entity_value", "topic": "t/out", "entity_id": "sensor.out", "comparison": "below", "value": 15.0})
	if FixApplied(fix, doc) {
		t.Fatal("different comparison counted as applied")
	}
}

func TestFixAppliedNeedsSameEntity(t *testing.T) {
	doc := appliedDoc(t, `{"type":"entity_value","topic":"t/other","entity_id":"sensor.other","comparison":"below","value":15}`)
	fix := addConditionFix(map[string]any{"type": "entity_value", "topic": "t/out", "entity_id": "sensor.out", "comparison": "below", "value": 15.0})
	if FixApplied(fix, doc) {
		t.Fatal("other entity counted as applied")
	}
}

func TestFixAppliedBalanceThresholdByField(t *testing.T) {
	doc := appliedDoc(t, `{"type":"balance_threshold","field":"load_total","comparison":"above","threshold":2000}`)
	same := addConditionFix(map[string]any{"type": "balance_threshold", "field": "load_total", "comparison": "above", "threshold": 2100.0})
	other := addConditionFix(map[string]any{"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 2100.0})
	if !FixApplied(same, doc) || FixApplied(other, doc) {
		t.Fatal("balance threshold must match by field only")
	}
}

func TestFixAppliedTimeWindowNeedsEqualFields(t *testing.T) {
	doc := appliedDoc(t, `{"type":"time_window","start":"06:00","end":"22:00","weekdays":[0,1]}`)
	exact := addConditionFix(map[string]any{"type": "time_window", "start": "06:00", "end": "22:00", "weekdays": []int{0, 1}})
	other := addConditionFix(map[string]any{"type": "time_window", "start": "06:00", "end": "22:00", "weekdays": []int{0, 1, 2}})
	if !FixApplied(exact, doc) {
		t.Fatal("equal time window not recognised as applied")
	}
	if FixApplied(other, doc) {
		t.Fatal("different weekdays counted as applied")
	}
}

func TestFixAppliedOnlyAddCondition(t *testing.T) {
	doc := appliedDoc(t, `{"type":"entity_value","topic":"t/out","entity_id":"sensor.out","comparison":"below","value":15}`)
	fix := addConditionFix(map[string]any{"type": "entity_value", "topic": "t/out", "entity_id": "sensor.out", "comparison": "below", "value": 15.0})
	fix.Code = "shift_time"
	if FixApplied(fix, doc) {
		t.Fatal("non add_condition fix counted as applied")
	}
	if FixApplied(addConditionFix(nil), Doc{}) {
		t.Fatal("empty document counted as applied")
	}
}

// opsDoc has two rules with one balance threshold each.
const opsDoc = `{"rules":[
 {"id":"on","name":"An","enabled":true,"conditions":[{"type":"balance_threshold","field":"grid_export","comparison":"above","threshold":1200}],"actions":[]},
 {"id":"off","name":"Aus","enabled":true,"cooldown_seconds":600,"conditions":[{"type":"balance_threshold","field":"grid_export","comparison":"below","threshold":300}],"actions":[]}]}`

func TestOpsAppliedNeedsEveryRuleOfTheFix(t *testing.T) {
	doc, err := ParseDoc([]byte(opsDoc))
	if err != nil {
		t.Fatal(err)
	}
	fix := Fix{Code: "feedback_thresholds", Rules: []RuleOps{
		{RuleID: "on", Ops: []Op{{Path: []any{"conditions", 0, "threshold"}, Value: 1200}}},
		{RuleID: "off", Ops: []Op{{Path: []any{"conditions", 0, "threshold"}, Value: 300.0}}},
	}}
	if !OpsApplied(fix, doc) {
		t.Fatal("both thresholds are saved, want applied")
	}
	fix.Rules[1].Ops[0].Value = 400
	if OpsApplied(fix, doc) {
		t.Fatal("one threshold differs, want not applied")
	}
}

func TestOpsAppliedSingleRuleAndOddCases(t *testing.T) {
	doc, err := ParseDoc([]byte(opsDoc))
	if err != nil {
		t.Fatal(err)
	}
	if !OpsApplied(Fix{Code: "set_cooldown", RuleID: "off", Ops: []Op{{Path: []any{"cooldown_seconds"}, Value: 600}}}, doc) {
		t.Fatal("cooldown 600 is saved, want applied")
	}
	if OpsApplied(Fix{Code: "set_threshold", RuleID: "gone", Ops: []Op{{Path: []any{"conditions", 0, "threshold"}, Value: 1200}}}, doc) {
		t.Fatal("missing rule, want not applied")
	}
	if OpsApplied(Fix{Code: "open_rule", RuleID: "on"}, doc) {
		t.Fatal("fix without ops, want not applied")
	}
	if OpsApplied(Fix{Code: "set_threshold", RuleID: "on", Ops: []Op{{Path: []any{"conditions", 3, "threshold"}, Value: 1200}}}, doc) {
		t.Fatal("index out of range, want not applied")
	}
}
