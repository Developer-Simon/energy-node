package autolearn

import (
	"encoding/json"
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
