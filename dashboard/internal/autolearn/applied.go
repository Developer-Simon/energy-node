package autolearn

import (
	"encoding/json"
	"math"
	"reflect"
)

// AppliedIn reports whether the rule document already holds the condition that
// one of the add_condition fixes of f would add. A finding that was applied
// stops being a suggestion.
func (f Finding) AppliedIn(doc Doc) bool {
	for _, fix := range f.Apply {
		if FixApplied(fix, doc) {
			return true
		}
	}
	return false
}

// FixApplied reports whether the rule of an add_condition fix already holds
// the condition the fix adds. A condition with a comparison matches by type,
// value key (entity, topic or balance field) and comparison, whatever its
// value. Any other condition matches when every field of the fix is equal.
func FixApplied(fix Fix, doc Doc) bool {
	if fix.Code != "add_condition" {
		return false
	}
	var rule *Rule
	for i := range doc.Rules {
		if doc.Rules[i].ID == fix.RuleID {
			rule = &doc.Rules[i]
			break
		}
	}
	if rule == nil {
		return false
	}
	for _, op := range fix.Ops {
		want, ok := normalizedCondition(op.Value)
		if !ok {
			continue
		}
		for _, c := range rule.Conditions {
			if conditionMatches(c, want) {
				return true
			}
		}
	}
	return false
}

// normalizedCondition returns the condition of a fix value as JSON-like map,
// so that numbers and slices compare as they do in the rule document.
func normalizedCondition(value any) (map[string]any, bool) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil || want == nil {
		return nil, false
	}
	return want, true
}

// conditionMatches reports whether the existing condition c is the condition
// want of a fix, in the sense of FixApplied.
func conditionMatches(c Condition, want map[string]any) bool {
	wantType, _ := want["type"].(string)
	if c.Type != wantType {
		return false
	}
	comparison, _ := want["comparison"].(string)
	if comparison != "" {
		wantField, _ := want["field"].(string)
		wantTopic, _ := want["topic"].(string)
		wantEntity, _ := want["entity_id"].(string)
		wantKey := Condition{Type: wantType, Field: wantField, Topic: wantTopic, EntityID: wantEntity}.ValueKey()
		return wantKey != "" && c.Comparison == comparison && c.ValueKey() == wantKey
	}
	if len(c.Raw) == 0 {
		return false
	}
	var have map[string]any
	if err := json.Unmarshal(c.Raw, &have); err != nil {
		return false
	}
	for key, value := range want {
		if !reflect.DeepEqual(have[key], value) {
			return false
		}
	}
	return true
}

// OpsApplied reports whether the saved rule document already holds every
// value the ops of fix would write: for its one rule, or for every rule of
// fix.Rules. The engine keeps a finding until its detector runs again, so a
// finding whose fix was saved is stale. A fix without ops, a missing rule and
// a path it cannot read report false.
func OpsApplied(fix Fix, doc Doc) bool {
	groups := fix.Rules
	if len(groups) == 0 {
		groups = []RuleOps{{RuleID: fix.RuleID, Ops: fix.Ops}}
	}
	seen := false
	for _, g := range groups {
		// A regulator fix names a scenario by its ID, a rule fix names a rule.
		if rule := ruleNamed(doc, g.RuleID); rule != nil {
			for _, op := range g.Ops {
				got, ok := ruleValueAt(*rule, op.Path)
				if !ok || !sameValue(got, op.Value) {
					return false
				}
				seen = true
			}
			continue
		}
		sc := scenarioNamed(doc, g.RuleID)
		if sc == nil {
			return false
		}
		for _, op := range g.Ops {
			got, ok := scenarioValueAt(*sc, op.Path)
			if !ok || !sameValue(got, op.Value) {
				return false
			}
			seen = true
		}
	}
	return seen
}

// ruleNamed returns the rule with the given ID, nil without one.
func ruleNamed(doc Doc, id string) *Rule {
	for i := range doc.Rules {
		if doc.Rules[i].ID == id {
			return &doc.Rules[i]
		}
	}
	return nil
}

// scenarioNamed returns the scenario with the given ID, nil without one.
func scenarioNamed(doc Doc, id string) *Scenario {
	for i := range doc.Scenarios {
		if doc.Scenarios[i].ID == id {
			return &doc.Scenarios[i]
		}
	}
	return nil
}

// scenarioValueAt reads the regulator fields that the regulator fixes write:
// while.regulator.gain, deadband_w and max_change_w. A scenario without a
// regulator and any other path report false. Without a max_change_w limit the
// value is nil, which no fix writes.
func scenarioValueAt(sc Scenario, path []any) (any, bool) {
	if sc.Regulator == nil || len(path) != 3 || path[0] != "while" || path[1] != "regulator" {
		return nil, false
	}
	switch path[2] {
	case "gain":
		return sc.Regulator.Gain, true
	case "deadband_w":
		return sc.Regulator.DeadbandW, true
	case "max_change_w":
		if sc.Regulator.MaxChangeW == nil {
			return nil, true
		}
		return *sc.Regulator.MaxChangeW, true
	}
	return nil, false
}

// ruleValueAt reads a rule field or a field of one condition, the paths the
// fixes write. Any other path reports false.
func ruleValueAt(rule Rule, path []any) (any, bool) {
	if len(path) == 1 {
		switch path[0] {
		case "enabled":
			return rule.Enabled, true
		case "cooldown_seconds":
			return rule.CooldownSeconds, true
		case "repeat_seconds":
			return rule.RepeatSeconds, true
		}
		return nil, false
	}
	if len(path) != 3 || path[0] != "conditions" {
		return nil, false
	}
	idx, ok := pathIndex(path[1])
	key, isKey := path[2].(string)
	if !ok || !isKey || idx < 0 || idx >= len(rule.Conditions) {
		return nil, false
	}
	var fields map[string]any
	if err := json.Unmarshal(rule.Conditions[idx].Raw, &fields); err != nil {
		return nil, false
	}
	v, found := fields[key]
	return v, found
}

// pathIndex is a list index of a path, as Go int or as JSON number.
func pathIndex(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), n == math.Trunc(n)
	}
	return 0, false
}

// sameValue compares two values the way JSON sees them, so 1000 and 1000.0
// are equal.
func sameValue(a, b any) bool {
	ra, errA := json.Marshal(a)
	rb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	var na, nb any
	if json.Unmarshal(ra, &na) != nil || json.Unmarshal(rb, &nb) != nil {
		return false
	}
	return reflect.DeepEqual(na, nb)
}
