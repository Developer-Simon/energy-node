package autolearn

import (
	"reflect"
	"strings"
	"testing"
)

func TestTemplateIntent(t *testing.T) {
	cases := []struct {
		name string
		fix  Fix
		want Intent
		ok   bool
	}{
		{"time_point off", Fix{Template: "time_point", Prefill: map[string]any{"entity": "switch.lamp", "on": false}},
			Intent{Entity: "switch.lamp", On: false, Trigger: "time"}, true},
		{"time_point on", Fix{Template: "time_point", Prefill: map[string]any{"entity": "switch.lamp", "on": true}},
			Intent{Entity: "switch.lamp", On: true, Trigger: "time"}, true},
		{"runtime_off", Fix{Template: "runtime_off", Prefill: map[string]any{"entity": "switch.a"}},
			Intent{Entity: "switch.a", On: false, Trigger: "runtime"}, true},
		{"quiet_off", Fix{Template: "quiet_off", Prefill: map[string]any{"entity": "switch.b"}},
			Intent{Entity: "switch.b", On: false, Trigger: "quiet"}, true},
		{"night_off", Fix{Template: "night_off", Prefill: map[string]any{"entity": "switch.c"}},
			Intent{Entity: "switch.c", On: false, Trigger: "night"}, true},
		{"after_battery_full", Fix{Template: "after_battery_full", Prefill: map[string]any{"entity": "switch.d"}},
			Intent{Entity: "switch.d", On: true, Trigger: "battery_full"}, true},
		{"follow_switch", Fix{Template: "follow_switch", Prefill: map[string]any{
			"source_entity": "switch.s", "target_entity": "switch.t", "target_on": true}},
			Intent{Entity: "switch.t", On: true, Trigger: "follow"}, true},
		{"shift_start", Fix{Template: "shift_start", Prefill: map[string]any{"device": "dev"}},
			Intent{On: true, Trigger: "surplus"}, true},
		{"unknown template", Fix{Template: "surplus_switch", Prefill: map[string]any{"entity": "switch.e"}}, Intent{}, false},
		{"empty template", Fix{Code: "fix"}, Intent{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := TemplateIntent(tc.fix)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("TemplateIntent = %+v, %v; want %+v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

// ruleWith builds a rule with the given raw conditions and one publish action
// to entity, which is on or off.
func ruleWith(t *testing.T, entity string, conditions ...string) (Rule, []Target) {
	t.Helper()
	doc, err := ParseDoc([]byte(`{"rules":[{"id":"r","name":"R","enabled":true,"conditions":[` +
		strings.Join(conditions, ",") + `],"actions":[{"type":"publish","topic":"x/set","entity_id":"` + entity +
		`","payload_source":"constant","payload":"off"}]}]}`))
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	off := false
	return doc.Rules[0], []Target{{Entity: entity, On: &off}}
}

func TestRuleIntentsTriggers(t *testing.T) {
	cases := []struct {
		name       string
		conditions []string
		want       []string
	}{
		{"time window", []string{`{"type":"time_window","start":"08:00","end":"22:00"}`}, []string{"time"}},
		{"sun window", []string{`{"type":"sun_window","from":"sunset","to":"sunrise"}`}, []string{"time"}},
		{"night window", []string{`{"type":"time_window","start":"22:00","end":"06:00"}`}, []string{"night"}},
		{"night window learned", []string{`{"type":"time_window","start":{"learned":"site/night_rest","fallback":"23:00"},"end":{"learned":"site/day_start","fallback":"06:00"}}`}, []string{"night"}},
		{"quiet", []string{`{"type":"learned_state","learned":"site/house_state","equals":"quiet","fallback":"active"}`}, []string{"quiet"}},
		{"battery full", []string{`{"type":"balance_threshold","field":"battery_soc","comparison":"above","threshold":96}`}, []string{"battery_full"}},
		{"surplus", []string{`{"type":"balance_threshold","field":"grid_export","comparison":"above","threshold":500}`}, []string{"surplus"}},
		{"runtime", []string{`{"type":"entity_value","entity_id":"switch.lamp","comparison":"equals","text":"ON","hold_seconds":3600}`}, []string{"runtime"}},
		{"runtime learned", []string{`{"type":"entity_value","entity_id":"switch.lamp","comparison":"equals","text":"ON","hold_seconds":{"learned":"device/lamp/usual_runtime_s","fallback":3600}}`}, []string{"runtime"}},
		{"follow", []string{`{"type":"entity_value","entity_id":"switch.other","comparison":"equals","text":"ON"}`}, []string{"follow"}},
		{"own state without hold", []string{`{"type":"entity_value","entity_id":"switch.lamp","comparison":"equals","text":"ON"}`}, nil},
		{"other balance field", []string{`{"type":"balance_threshold","field":"grid_import","comparison":"above","threshold":100}`}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule, targets := ruleWith(t, "switch.lamp", tc.conditions...)
			got := RuleIntents(rule, targets)
			var triggers []string
			for _, in := range got {
				if in.Entity != "switch.lamp" || in.On {
					t.Fatalf("intent %+v, want entity switch.lamp off", in)
				}
				triggers = append(triggers, in.Trigger)
			}
			if !reflect.DeepEqual(triggers, tc.want) {
				t.Fatalf("triggers = %v, want %v", triggers, tc.want)
			}
		})
	}
}

func TestRuleIntentsWithoutTargetsIsEmpty(t *testing.T) {
	rule, _ := ruleWith(t, "switch.lamp", `{"type":"time_window","start":"08:00","end":"22:00"}`)
	if got := RuleIntents(rule, nil); len(got) != 0 {
		t.Fatalf("intents = %+v, want none without a target", got)
	}
}
