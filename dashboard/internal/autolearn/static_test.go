package autolearn

import (
	"testing"
)

func staticDoc(t *testing.T, raw string) Doc {
	t.Helper()
	doc, err := ParseDoc([]byte(raw))
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	return doc
}

func staticExists(known ...string) func(string) bool {
	set := map[string]bool{}
	for _, id := range known {
		set[id] = true
	}
	return func(id string) bool { return set[id] }
}

func expectSingleFinding(t *testing.T, findings []Finding) Finding {
	t.Helper()
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1: %+v", len(findings), findings)
	}
	if findings[0].Group != "check" {
		t.Errorf("Group = %q, want check", findings[0].Group)
	}
	return findings[0]
}

func TestStaticRegulatorConflict(t *testing.T) {
	doc := staticDoc(t, `{
	  "rules": [{"id": "pump", "name": "Pump", "enabled": true,
	    "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 500}],
	    "actions": [{"type": "publish", "topic": "heater/power/set", "payload_source": "constant", "payload": "50"}]}],
	  "scenarios": [{"id": "heater", "name": "Heater regulator", "enabled": true,
	    "while": {"regulator": {"output": {"topic": "heater/power/set"}}}}]
	}`)
	f := expectSingleFinding(t, StaticFindings(doc, staticExists()))
	if f.ID != "regulator_conflict:pump:heater" || f.Kind != "regulator_conflict" {
		t.Errorf("ID/Kind = %q/%q", f.ID, f.Kind)
	}
	if f.Severity != "bad" {
		t.Errorf("Severity = %q, want bad", f.Severity)
	}
	if len(f.RuleIDs) != 1 || f.RuleIDs[0] != "pump" {
		t.Errorf("RuleIDs = %v, want [pump]", f.RuleIDs)
	}
	if f.Evidence.Shape != "links" || f.Evidence.Target != "heater/power/set" {
		t.Errorf("Evidence = %+v", f.Evidence)
	}
	if !equalStrings(f.Evidence.IstNames, []string{"Pump", "Heater regulator"}) ||
		!equalStrings(f.Evidence.SollNames, []string{"Heater regulator"}) {
		t.Errorf("names = %v / %v", f.Evidence.IstNames, f.Evidence.SollNames)
	}
	if len(f.Apply) != 1 || f.Apply[0].Code != "open_rule" || f.Apply[0].RuleID != "pump" || len(f.Apply[0].Ops) != 0 {
		t.Errorf("Apply = %+v, want one open_rule on pump", f.Apply)
	}
}

func TestStaticContradiction(t *testing.T) {
	// hold_seconds is not part of the condition identity, so the two rules
	// still have the same conditions.
	doc := staticDoc(t, `{
	  "rules": [
	    {"id": "a", "name": "Wallbox on", "enabled": true,
	     "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 500, "hold_seconds": 30}],
	     "actions": [{"type": "publish", "topic": "wallbox/set", "payload_source": "constant", "payload": "on"}]},
	    {"id": "b", "name": "Wallbox off", "enabled": true,
	     "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 500, "hold_seconds": 60}],
	     "actions": [{"type": "publish", "topic": "wallbox/set", "payload_source": "constant", "payload": "off"}]}
	  ]
	}`)
	f := expectSingleFinding(t, StaticFindings(doc, staticExists()))
	if f.ID != "contradiction:a:b" || f.Kind != "contradiction" {
		t.Errorf("ID/Kind = %q/%q", f.ID, f.Kind)
	}
	if f.Severity != "bad" {
		t.Errorf("Severity = %q, want bad", f.Severity)
	}
	if !equalStrings(f.RuleIDs, []string{"a", "b"}) {
		t.Errorf("RuleIDs = %v, want [a b]", f.RuleIDs)
	}
	if f.Evidence.Shape != "links" || !equalStrings(f.Evidence.IstNames, []string{"Wallbox on", "Wallbox off"}) ||
		len(f.Evidence.SollNames) != 0 {
		t.Errorf("Evidence = %+v", f.Evidence)
	}
	if len(f.Apply) != 2 || f.Apply[0].Code != "open_rule" || f.Apply[0].RuleID != "a" ||
		f.Apply[1].Code != "open_rule" || f.Apply[1].RuleID != "b" {
		t.Errorf("Apply = %+v, want open_rule on a and b", f.Apply)
	}
}

func TestStaticDuplicate(t *testing.T) {
	doc := staticDoc(t, `{
	  "rules": [
	    {"id": "first", "name": "Pump first", "enabled": true,
	     "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 500}],
	     "actions": [{"type": "publish", "topic": "pump/set", "payload_source": "constant", "payload": "on"}]},
	    {"id": "second", "name": "Pump second", "enabled": true,
	     "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 500}],
	     "actions": [{"type": "publish", "topic": "pump/set", "payload_source": "constant", "payload": "on"}]}
	  ]
	}`)
	f := expectSingleFinding(t, StaticFindings(doc, staticExists()))
	if f.ID != "duplicate:first:second" || f.Kind != "duplicate" {
		t.Errorf("ID/Kind = %q/%q", f.ID, f.Kind)
	}
	if f.Severity != "info" {
		t.Errorf("Severity = %q, want info", f.Severity)
	}
	if f.Evidence.Shape != "links" || !equalStrings(f.Evidence.SollNames, []string{"Pump first"}) {
		t.Errorf("Evidence = %+v", f.Evidence)
	}
	if len(f.Apply) != 1 || f.Apply[0].Code != "disable_duplicate" || f.Apply[0].RuleID != "second" {
		t.Fatalf("Apply = %+v, want disable_duplicate on second", f.Apply)
	}
	ops := f.Apply[0].Ops
	if len(ops) != 1 || len(ops[0].Path) != 1 || ops[0].Path[0] != "enabled" || ops[0].Value != false {
		t.Errorf("Ops = %+v, want [{[enabled] false}]", ops)
	}
}

func TestStaticUnsatisfiable(t *testing.T) {
	doc := staticDoc(t, `{
	  "rules": [{"id": "impossible", "name": "Impossible", "enabled": true,
	    "conditions": [
	      {"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 80},
	      {"type": "balance_threshold", "field": "grid_export", "comparison": "below", "threshold": 50}],
	    "actions": [{"type": "publish", "topic": "x/set", "payload_source": "constant", "payload": "on"}]}]
	}`)
	f := expectSingleFinding(t, StaticFindings(doc, staticExists()))
	if f.ID != "unsatisfiable:impossible" || f.Kind != "unsatisfiable" {
		t.Errorf("ID/Kind = %q/%q", f.ID, f.Kind)
	}
	if f.Severity != "bad" {
		t.Errorf("Severity = %q, want bad", f.Severity)
	}
	if f.Evidence.Shape != "scale" || f.Evidence.IstValue == nil || *f.Evidence.IstValue != 80 ||
		f.Evidence.SollValue != nil || f.Evidence.Seen != [2]float64{50, 80} {
		t.Errorf("Evidence = %+v", f.Evidence)
	}
	if len(f.Apply) != 1 || f.Apply[0].Code != "open_rule" || f.Apply[0].RuleID != "impossible" {
		t.Errorf("Apply = %+v, want open_rule", f.Apply)
	}
}

func TestStaticOrphan(t *testing.T) {
	doc := staticDoc(t, `{
	  "rules": [{"id": "warm", "name": "Warm", "enabled": true,
	    "conditions": [{"type": "entity_value", "entity_id": "sensor.gone", "comparison": "above", "value": 18.5}],
	    "actions": [{"type": "publish", "topic": "x/set", "payload_source": "constant", "payload": "on"}]}]
	}`)
	f := expectSingleFinding(t, StaticFindings(doc, staticExists("sensor.known")))
	if f.ID != "orphan:warm:sensor.gone" || f.Kind != "orphan" {
		t.Errorf("ID/Kind = %q/%q", f.ID, f.Kind)
	}
	if f.Severity != "warn" {
		t.Errorf("Severity = %q, want warn", f.Severity)
	}
	if f.Evidence.Shape != "links" || f.Evidence.Target != "sensor.gone" || !f.Evidence.Missing ||
		!equalStrings(f.Evidence.SollNames, []string{"Warm"}) {
		t.Errorf("Evidence = %+v", f.Evidence)
	}
	if len(f.Apply) != 1 || f.Apply[0].Code != "open_rule" || f.Apply[0].RuleID != "warm" {
		t.Errorf("Apply = %+v, want open_rule", f.Apply)
	}
}

func TestStaticSkipsDisabledAndOnOff(t *testing.T) {
	// Disabled rules and scenarios produce nothing, and an on/off pair on the
	// same topic with different conditions is the normal case.
	doc := staticDoc(t, `{
	  "rules": [
	    {"id": "on", "name": "Wallbox on", "enabled": true,
	     "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 500}],
	     "actions": [{"type": "publish", "topic": "wallbox/set", "payload_source": "constant", "payload": "on"}]},
	    {"id": "off", "name": "Wallbox off", "enabled": true,
	     "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "below", "threshold": 300}],
	     "actions": [{"type": "publish", "topic": "wallbox/set", "payload_source": "constant", "payload": "off"}]},
	    {"id": "ghost", "name": "Ghost", "enabled": false,
	     "conditions": [{"type": "entity_value", "entity_id": "sensor.gone", "comparison": "above", "value": 1}],
	     "actions": [{"type": "publish", "topic": "heater/power/set", "payload_source": "constant", "payload": "50"}]},
	    {"id": "ghost_twin", "name": "Ghost twin", "enabled": false,
	     "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 500}],
	     "actions": [{"type": "publish", "topic": "wallbox/set", "payload_source": "constant", "payload": "on"}]},
	    {"id": "pump", "name": "Pump", "enabled": true,
	     "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 500}],
	     "actions": [{"type": "publish", "topic": "old/out", "payload_source": "constant", "payload": "on"}]}
	  ],
	  "scenarios": [
	    {"id": "heater", "name": "Heater regulator", "enabled": false,
	     "while": {"regulator": {"output": {"topic": "heater/power/set"}}}},
	    {"id": "old", "name": "Old regulator", "enabled": false,
	     "while": {"regulator": {"output": {"topic": "old/out"}}}}
	  ]
	}`)
	if findings := StaticFindings(doc, staticExists()); len(findings) != 0 {
		t.Fatalf("got %d findings, want none: %+v", len(findings), findings)
	}
}

func TestStaticSortsBySeverity(t *testing.T) {
	doc := staticDoc(t, `{
	  "rules": [
	    {"id": "dup_a", "name": "Dup A", "enabled": true,
	     "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 500}],
	     "actions": [{"type": "publish", "topic": "d/set", "payload_source": "constant", "payload": "on"}]},
	    {"id": "dup_b", "name": "Dup B", "enabled": true,
	     "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 500}],
	     "actions": [{"type": "publish", "topic": "d/set", "payload_source": "constant", "payload": "on"}]},
	    {"id": "impossible", "name": "Impossible", "enabled": true,
	     "conditions": [
	      {"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 80},
	      {"type": "balance_threshold", "field": "grid_export", "comparison": "below", "threshold": 50}],
	     "actions": [{"type": "publish", "topic": "x/set", "payload_source": "constant", "payload": "on"}]}
	  ]
	}`)
	findings := StaticFindings(doc, staticExists())
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2: %+v", len(findings), findings)
	}
	if findings[0].Severity != "bad" || findings[1].Severity != "info" {
		t.Errorf("severities = %q, %q, want bad then info", findings[0].Severity, findings[1].Severity)
	}
	for _, f := range findings {
		if f.Group != "check" {
			t.Errorf("finding %s: Group = %q, want check", f.ID, f.Group)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
