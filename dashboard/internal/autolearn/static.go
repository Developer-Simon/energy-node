package autolearn

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

var severityRank = map[string]int{"bad": 0, "warn": 1, "info": 2}

// StaticFindings checks the rule document without any history: regulator
// conflicts, contradicting and duplicate rules, conditions that can never be
// true and references to unknown entities. Only enabled rules and scenarios
// are checked. entityExists may be nil, which skips the orphan check. The
// findings are sorted by severity, then by ID.
func StaticFindings(doc Doc, entityExists func(entityID string) bool) []Finding {
	var findings []Finding
	var enabled []Rule
	for _, rule := range doc.Rules {
		if !rule.Enabled {
			continue
		}
		enabled = append(enabled, rule)
		findings = append(findings, regulatorConflicts(rule, doc.Scenarios)...)
		findings = append(findings, unsatisfiable(rule)...)
		findings = append(findings, orphans(rule, entityExists)...)
	}
	findings = append(findings, pairFindings(enabled)...)
	sort.SliceStable(findings, func(i, j int) bool {
		ri, rj := severityRank[findings[i].Severity], severityRank[findings[j].Severity]
		if ri != rj {
			return ri < rj
		}
		return findings[i].ID < findings[j].ID
	})
	return findings
}

func regulatorConflicts(rule Rule, scenarios []Scenario) []Finding {
	var out []Finding
	for _, scenario := range scenarios {
		if !scenario.Enabled || scenario.RegulatorTopic == "" {
			continue
		}
		for _, action := range rule.Actions {
			if action.Type != "publish" || action.Topic != scenario.RegulatorTopic {
				continue
			}
			out = append(out, Finding{
				ID:         FindingID("regulator_conflict", rule.ID) + ":" + scenario.ID,
				Kind:       "regulator_conflict",
				Group:      "check",
				RuleIDs:    []string{rule.ID},
				Severity:   "bad",
				Confidence: "high",
				Summary: Text{Code: "check.regulator_conflict", Params: map[string]any{
					"rule": rule.Name, "other": scenario.Name, "topic": action.Topic}},
				Evidence: Evidence{Shape: "links", Target: action.Topic,
					IstNames: []string{rule.Name, scenario.Name}, SollNames: []string{scenario.Name}},
				Apply: []Fix{{Code: "open_rule", RuleID: rule.ID}},
			})
			break
		}
	}
	return out
}

// unsatisfiable reports a rule with "above X" and "below Y" on the same value
// where X >= Y, since no value can satisfy both. It reports the first such pair.
func unsatisfiable(rule Rule) []Finding {
	for _, above := range rule.Conditions {
		if above.Comparison != "above" {
			continue
		}
		key := above.ValueKey()
		x, ok := conditionNumber(above)
		if key == "" || !ok {
			continue
		}
		for _, below := range rule.Conditions {
			if below.Comparison != "below" || below.ValueKey() != key {
				continue
			}
			y, ok := conditionNumber(below)
			if !ok || x < y {
				continue
			}
			ist := x
			return []Finding{{
				ID:         FindingID("unsatisfiable", rule.ID),
				Kind:       "unsatisfiable",
				Group:      "check",
				RuleIDs:    []string{rule.ID},
				Severity:   "bad",
				Confidence: "high",
				Summary: Text{Code: "check.unsatisfiable", Params: map[string]any{
					"rule": rule.Name}},
				Evidence: Evidence{Shape: "scale", IstValue: &ist, Seen: [2]float64{y, x}},
				Apply:    []Fix{{Code: "open_rule", RuleID: rule.ID}},
			}}
		}
	}
	return nil
}

// conditionNumber returns the number a condition compares against. Threshold
// belongs to balance_threshold, Value to topic_value and entity_value.
func conditionNumber(c Condition) (float64, bool) {
	switch c.Type {
	case "balance_threshold":
		return c.Threshold, true
	case "topic_value", "entity_value":
		if c.Value != nil {
			return *c.Value, true
		}
	}
	return 0, false
}

func orphans(rule Rule, entityExists func(entityID string) bool) []Finding {
	if entityExists == nil {
		return nil
	}
	var out []Finding
	seen := map[string]bool{}
	check := func(entityID string) {
		if entityID == "" || seen[entityID] || entityExists(entityID) {
			return
		}
		seen[entityID] = true
		out = append(out, Finding{
			ID:         FindingID("orphan", rule.ID) + ":" + entityID,
			Kind:       "orphan",
			Group:      "check",
			RuleIDs:    []string{rule.ID},
			Severity:   "warn",
			Confidence: "high",
			Summary: Text{Code: "check.orphan", Params: map[string]any{
				"rule": rule.Name, "entity": entityID}},
			Evidence: Evidence{Shape: "links", Target: entityID, Missing: true,
				SollNames: []string{rule.Name}},
			Apply: []Fix{{Code: "open_rule", RuleID: rule.ID}},
		})
	}
	for _, c := range rule.Conditions {
		check(c.EntityID)
	}
	for _, a := range rule.Actions {
		check(a.EntityID)
	}
	return out
}

// pairFindings compares every pair of enabled rules in document order. Same
// conditions and same actions is a duplicate, same conditions with a shared
// publish topic and another payload is a contradiction.
func pairFindings(rules []Rule) []Finding {
	var out []Finding
	for i := range rules {
		for j := i + 1; j < len(rules); j++ {
			a, b := rules[i], rules[j]
			if conditionsKey(a) != conditionsKey(b) {
				continue
			}
			if actionsKey(a) == actionsKey(b) {
				out = append(out, duplicateFinding(a, b))
				continue
			}
			if topic, ok := conflictingPublishTopic(a, b); ok {
				out = append(out, contradictionFinding(a, b, topic))
			}
		}
	}
	return out
}

func duplicateFinding(first, second Rule) Finding {
	return Finding{
		ID:         FindingID("duplicate", first.ID, second.ID),
		Kind:       "duplicate",
		Group:      "check",
		RuleIDs:    []string{first.ID, second.ID},
		Severity:   "info",
		Confidence: "high",
		Summary: Text{Code: "check.duplicate", Params: map[string]any{
			"rule": second.Name, "other": first.Name}},
		Evidence: Evidence{Shape: "links", IstNames: []string{second.Name},
			SollNames: []string{first.Name}},
		Apply: []Fix{{Code: "disable_duplicate", RuleID: second.ID,
			Ops: []Op{{Path: []any{"enabled"}, Value: false}}}},
	}
}

func contradictionFinding(a, b Rule, topic string) Finding {
	return Finding{
		ID:         FindingID("contradiction", a.ID, b.ID),
		Kind:       "contradiction",
		Group:      "check",
		RuleIDs:    []string{a.ID, b.ID},
		Severity:   "bad",
		Confidence: "high",
		Summary: Text{Code: "check.contradiction", Params: map[string]any{
			"rule": a.Name, "other": b.Name, "topic": topic}},
		Evidence: Evidence{Shape: "links", IstNames: []string{a.Name, b.Name}},
		Apply: []Fix{
			{Code: "open_rule", RuleID: a.ID},
			{Code: "open_rule", RuleID: b.ID},
		},
	}
}

// conditionsKey is the canonical form of the conditions of a rule, without
// hold_seconds, so that the order of the conditions does not matter.
func conditionsKey(rule Rule) string {
	keys := make([]string, 0, len(rule.Conditions))
	for _, c := range rule.Conditions {
		keys = append(keys, canonicalCondition(c.Raw))
	}
	return sortedKey(keys)
}

func canonicalCondition(raw json.RawMessage) string {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return string(raw)
	}
	delete(fields, "hold_seconds")
	out, err := json.Marshal(fields)
	if err != nil {
		return string(raw)
	}
	return string(out)
}

func actionsKey(rule Rule) string {
	keys := make([]string, 0, len(rule.Actions))
	for _, a := range rule.Actions {
		keys = append(keys, fmt.Sprintf("%q|%q|%q|%q|%q",
			a.Type, a.Topic, a.PayloadSource, a.Payload, a.EntityID))
	}
	return sortedKey(keys)
}

func sortedKey(keys []string) string {
	sort.Strings(keys)
	return strings.Join(keys, "\n")
}

func conflictingPublishTopic(a, b Rule) (string, bool) {
	for _, pa := range a.Actions {
		if pa.Type != "publish" || pa.Topic == "" {
			continue
		}
		for _, pb := range b.Actions {
			if pb.Type == "publish" && pb.Topic == pa.Topic && pb.Payload != pa.Payload {
				return pa.Topic, true
			}
		}
	}
	return "", false
}
