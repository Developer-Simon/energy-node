package autolearn

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const testRuleDocument = `{
  "version": 2,
  "settings": {"tick_interval_s": 10},
  "rules": [
    {
      "id": "export_pump",
      "name": "Pump on export",
      "enabled": true,
      "cooldown_seconds": 300,
      "repeat_seconds": 60,
      "unknown_future_field": {"x": 1},
      "conditions": [
        {"type": "balance_threshold", "field": "grid_export", "comparison": "above",
         "threshold": 500, "hysteresis": 50, "hold_seconds": 30}
      ],
      "actions": [
        {"type": "publish", "topic": "wallbox/set", "payload_source": "constant", "payload": "on"}
      ]
    },
    {
      "id": "keller_warm",
      "name": "Cellar warm",
      "enabled": false,
      "conditions": [
        {"type": "entity_value", "entity_id": "ht_keller_temperature", "topic": "ha/state",
         "comparison": "above", "value": 18.5}
      ],
      "actions": [
        {"type": "notification", "severity": "info", "title": "Warm", "message": "Cellar warm"}
      ]
    },
    {
      "id": "daytime",
      "name": "Daytime",
      "enabled": true,
      "conditions": [
        {"type": "time_window", "start": "08:00", "end": "18:00", "weekdays": [0, 1]}
      ],
      "actions": []
    }
  ],
  "scenarios": [
    {
      "id": "heater_regulator",
      "name": "Heater regulator",
      "enabled": true,
      "when": {"conditions": []},
      "until": null,
      "on_start": {"actions": [
        {"type": "publish", "topic": "heater/mode/set", "payload_source": "constant", "payload": "auto"}
      ]},
      "while": {
        "actions": [],
        "regulator": {
          "output": {"topic": "heater/power/set"},
          "base": {"source": "balance", "field": "grid_export"}
        }
      },
      "on_end": {"actions": [
        {"type": "publish", "topic": "heater/mode/set", "payload_source": "constant", "payload": "off"}
      ]}
    }
  ]
}`

func TestParseDocReadsRuleShapes(t *testing.T) {
	doc, err := ParseDoc([]byte(testRuleDocument))
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if len(doc.Rules) != 3 || len(doc.Scenarios) != 1 {
		t.Fatalf("got %d rules and %d scenarios, want 3 and 1", len(doc.Rules), len(doc.Scenarios))
	}

	pump := doc.Rules[0]
	if pump.ID != "export_pump" || pump.Name != "Pump on export" || !pump.Enabled {
		t.Errorf("rule header = %+v", pump)
	}
	if pump.CooldownSeconds != 300 || pump.RepeatSeconds != 60 {
		t.Errorf("cooldown/repeat = %v/%v, want 300/60", pump.CooldownSeconds, pump.RepeatSeconds)
	}
	if len(pump.Conditions) != 1 {
		t.Fatalf("conditions = %d, want 1", len(pump.Conditions))
	}
	cond := pump.Conditions[0]
	if cond.Type != "balance_threshold" || cond.Field != "grid_export" || cond.Comparison != "above" {
		t.Errorf("condition = %+v", cond)
	}
	if cond.Threshold != 500 || cond.Hysteresis != 50 {
		t.Errorf("threshold/hysteresis = %v/%v, want 500/50", cond.Threshold, cond.Hysteresis)
	}
	if cond.Value != nil {
		t.Errorf("Value = %v, want nil for balance_threshold", *cond.Value)
	}
	if !strings.Contains(string(cond.Raw), `"hold_seconds": 30`) {
		t.Errorf("Raw = %s, want the original condition JSON", cond.Raw)
	}
	if len(pump.Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(pump.Actions))
	}
	act := pump.Actions[0]
	if act.Type != "publish" || act.Topic != "wallbox/set" || act.PayloadSource != "constant" || act.Payload != "on" {
		t.Errorf("action = %+v", act)
	}

	cellar := doc.Rules[1]
	if cellar.Enabled {
		t.Errorf("cellar rule enabled, want disabled")
	}
	entity := cellar.Conditions[0]
	if entity.EntityID != "ht_keller_temperature" || entity.Topic != "ha/state" {
		t.Errorf("entity condition = %+v", entity)
	}
	if entity.Value == nil || *entity.Value != 18.5 {
		t.Errorf("entity Value = %v, want 18.5", entity.Value)
	}
	if cellar.Actions[0].Type != "notification" {
		t.Errorf("notification action type = %q", cellar.Actions[0].Type)
	}

	window := doc.Rules[2].Conditions[0]
	if window.Type != "time_window" || window.Topic != "" || window.Value != nil {
		t.Errorf("time_window condition = %+v", window)
	}

	scenario := doc.Scenarios[0]
	if scenario.ID != "heater_regulator" || scenario.Name != "Heater regulator" || !scenario.Enabled {
		t.Errorf("scenario header = %+v", scenario)
	}
	if scenario.RegulatorTopic != "heater/power/set" {
		t.Errorf("RegulatorTopic = %q, want heater/power/set", scenario.RegulatorTopic)
	}
	wantTopics := []string{"heater/mode/set", "heater/mode/set"}
	if !reflect.DeepEqual(scenario.ActionTopics, wantTopics) {
		t.Errorf("ActionTopics = %v, want %v", scenario.ActionTopics, wantTopics)
	}
}

func TestParseDocRejectsInvalidJSON(t *testing.T) {
	if _, err := ParseDoc([]byte(`{"rules": [`)); err == nil {
		t.Fatal("ParseDoc accepted invalid JSON")
	}
}

func TestParseConditionValueKey(t *testing.T) {
	doc, err := ParseDoc([]byte(testRuleDocument))
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	cases := []struct {
		name string
		cond Condition
		want string
	}{
		{"balance", doc.Rules[0].Conditions[0], "balance:grid_export"},
		{"entity", doc.Rules[1].Conditions[0], "entity:ht_keller_temperature"},
		{"time window", doc.Rules[2].Conditions[0], ""},
	}
	for _, tc := range cases {
		if got := tc.cond.ValueKey(); got != tc.want {
			t.Errorf("%s: ValueKey() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestParseFindingID(t *testing.T) {
	if got := FindingID("duplicate", "b", "a"); got != "duplicate:a:b" {
		t.Errorf("FindingID = %q, want duplicate:a:b", got)
	}
	ids := []string{"b", "a"}
	FindingID("duplicate", ids...)
	if !reflect.DeepEqual(ids, []string{"b", "a"}) {
		t.Errorf("FindingID reordered its input: %v", ids)
	}
}

func TestParseFindingJSONNames(t *testing.T) {
	value := 1.5
	finding := Finding{
		ID: "scale:a", Kind: "scale", Group: "check", RuleIDs: []string{"a"},
		Severity: "warn", Confidence: "high",
		Summary: Text{Code: "scale.summary", Params: map[string]any{"n": 1}},
		Facts:   []Fact{{Code: "fact.code", Value: 2}},
		Apply: []Fix{{Code: "fix.code", RuleID: "a", Ops: []Op{
			{Path: []any{"conditions", 0, "threshold"}, Value: 3},
		}}},
		Evidence: Evidence{
			Shape: "events", From: 1, To: 2, Ist: []int64{3}, SollSpans: [][2]int64{{4, 5}},
			Soll: []int64{6}, Unit: "W", Max: 7, Seen: [2]float64{8, 9}, IstValue: &value,
			SollValue: &value, SeasonMax: &value, Target: "t", IstNames: []string{"x"},
			SollNames: []string{"y"}, Missing: true,
		},
	}
	raw, err := json.Marshal(finding)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("Unmarshal keys: %v", err)
	}
	var facts []map[string]json.RawMessage
	var apply []map[string]json.RawMessage
	var summary, evidence map[string]json.RawMessage
	mustDecode(t, keys["facts"], &facts)
	mustDecode(t, keys["apply"], &apply)
	mustDecode(t, keys["summary"], &summary)
	mustDecode(t, keys["evidence"], &evidence)
	if len(facts) != 1 || len(apply) != 1 {
		t.Fatalf("facts/apply = %d/%d, want 1/1", len(facts), len(apply))
	}
	var ops []map[string]json.RawMessage
	mustDecode(t, apply[0]["ops"], &ops)
	if len(ops) != 1 {
		t.Fatalf("ops = %d, want 1", len(ops))
	}

	assertKeys(t, "finding", keys, []string{"id", "kind", "group", "rule_ids", "severity", "confidence", "summary", "facts", "apply", "evidence", "device", "season", "benefit"})
	assertKeys(t, "summary", summary, []string{"code", "params"})
	assertKeys(t, "fact", facts[0], []string{"code", "value"})
	assertKeys(t, "fix", apply[0], []string{"code", "rule_id", "ops"})
	assertKeys(t, "op", ops[0], []string{"path", "value"})
	assertKeys(t, "evidence", evidence, []string{"shape", "from", "to", "ist", "soll_spans", "soll", "unit", "max", "seen", "ist_value", "soll_value", "season_max", "target", "ist_names", "soll_names", "missing"})
}

func TestFindingJSONNewFields(t *testing.T) {
	finding := Finding{
		ID: "day:habit", Kind: "habit", Group: "learn", RuleIDs: []string{"a"},
		Device:  "light.kitchen",
		Season:  &Season{FromWeek: 10, ToWeek: 20},
		Benefit: &Benefit{Unit: "kwh_week", Value: 1.25},
		Apply: []Fix{
			{Code: "fix.new", RuleID: "a", Template: "scenario", Prefill: map[string]any{"device": "x"}},
			{Code: "fix.plain", RuleID: "a"},
		},
		Evidence: Evidence{
			Shape: "day", To: 1440, Ist: []int64{390}, Soll: []int64{400},
			SollSpans: [][2]int64{{380, 420}}, IstSpans: [][2]int64{{385, 395}},
		},
	}
	raw, err := json.Marshal(finding)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var keys map[string]json.RawMessage
	mustDecode(t, raw, &keys)
	assertKeys(t, "finding", keys, []string{"id", "kind", "group", "rule_ids", "severity", "confidence", "summary", "facts", "apply", "evidence", "device", "season", "benefit"})

	var season, benefit map[string]json.RawMessage
	mustDecode(t, keys["season"], &season)
	mustDecode(t, keys["benefit"], &benefit)
	assertKeys(t, "season", season, []string{"from_week", "to_week"})
	assertKeys(t, "benefit", benefit, []string{"unit", "value"})

	var evidence map[string]json.RawMessage
	mustDecode(t, keys["evidence"], &evidence)
	assertKeys(t, "evidence", evidence, []string{"shape", "to", "ist", "soll", "soll_spans", "ist_spans", "seen"})
	var shape string
	mustDecode(t, evidence["shape"], &shape)
	if shape != "day" {
		t.Errorf("shape = %q, want day", shape)
	}

	var apply []map[string]json.RawMessage
	mustDecode(t, keys["apply"], &apply)
	if len(apply) != 2 {
		t.Fatalf("apply = %d, want 2", len(apply))
	}
	assertKeys(t, "fix with template", apply[0], []string{"code", "rule_id", "ops", "template", "prefill"})
	assertKeys(t, "fix without template", apply[1], []string{"code", "rule_id", "ops"})
}

func mustDecode(t *testing.T, raw json.RawMessage, into any) {
	t.Helper()
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

func assertKeys(t *testing.T, name string, got map[string]json.RawMessage, want []string) {
	t.Helper()
	var gotKeys []string
	for key := range got {
		gotKeys = append(gotKeys, key)
	}
	sort.Strings(gotKeys)
	sort.Strings(want)
	if !reflect.DeepEqual(gotKeys, want) {
		t.Errorf("%s keys = %v, want %v", name, gotKeys, want)
	}
}
