package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The automation schema carries scenarios next to rules. These cases pin
// that the shipped rules file still validates, that the spec example of a
// scenario validates, that the scenario limits hold, and that the scenario
// item schemas stay copies of the rule item schemas.
func automationSchema(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "services", "automation", "automation_rules.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// automationDoc wraps a settings object and a scenarios array into a rules
// document with an empty rules list.
func automationDoc(settings, scenarios string) string {
	return `{"version":1,"settings":` + settings + `,"rules":[],"scenarios":` + scenarios + `}`
}

const defaultAutomationSettings = `{"tick_interval_s":10,"settling_seconds":60,"balance_max_age_s":30,"publish_allowed_prefixes":[]}`

// specScenario is the example from spec 1.3 without the null fields.
const specScenario = `{
  "id": "nulleinspeisung",
  "name": "Nulleinspeisung",
  "enabled": true,
  "template": "zero_feed",
  "history_enabled": true,
  "when": { "conditions": [ { "type": "sun_window", "from": "sunrise", "to": "sunset" } ] },
  "on_start": { "actions": [ { "type": "notification", "severity": "info", "title": "Nulleinspeisung", "message": "startet" } ] },
  "while": {
    "mode": "regulate",
    "regulator": {
      "output": { "topic": "werkstatt/bkw/limit/set", "entity_id": "bkw_limit", "min": 50, "max": 800, "step": 10, "decimals": 0 },
      "direction": "producer",
      "target_w": 30,
      "deadband_w": 20,
      "gain": 0.8,
      "base": { "source": "entity", "entity_id": "bkw_power", "topic": "werkstatt/bkw/state", "value_template": "{{ value_json.power }}" },
      "fallback": { "payload": "800" }
    }
  },
  "on_end": { "actions": [ { "type": "publish", "topic": "werkstatt/bkw/limit/set", "payload_source": "constant", "payload": "800" } ] }
}`

func minimalScenario(id string) string {
	return fmt.Sprintf(`{"id":%q,"name":"S","enabled":true,"when":{"conditions":[{"type":"sun_window","from":"sunrise","to":"sunset"}]}}`, id)
}

func TestAutomationSchemaAcceptsTheShippedRules(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "services", "automation", "automation_rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(data, automationSchema(t)); err != nil {
		t.Fatal(err)
	}
}

func TestAutomationSchemaAcceptsTheSpecScenario(t *testing.T) {
	doc := automationDoc(defaultAutomationSettings, "["+specScenario+"]")
	if err := ValidateDocument([]byte(doc), automationSchema(t)); err != nil {
		t.Fatal(err)
	}
}

func TestAutomationSchemaCases(t *testing.T) {
	nineScenarios := make([]string, 9)
	for i := range nineScenarios {
		nineScenarios[i] = minimalScenario(fmt.Sprintf("s%d", i))
	}
	cases := []struct {
		name string
		doc  string
		ok   bool
	}{
		{"negative repeat_seconds in while", automationDoc(defaultAutomationSettings,
			`[`+strings.Replace(minimalScenario("x"), `"enabled":true,`,
				`"enabled":true,"while":{"mode":"hold","actions":[],"repeat_seconds":-1},`, 1)+`]`), false},
		{"nine scenarios", automationDoc(defaultAutomationSettings, "["+strings.Join(nineScenarios, ",")+"]"), false},
		{"unknown key in scenario", automationDoc(defaultAutomationSettings,
			`[`+strings.Replace(minimalScenario("x"), `"enabled":true,`, `"enabled":true,"unknown":1,`, 1)+`]`), false},
		{"publish_min_interval_s below five", automationDoc(
			`{"tick_interval_s":10,"settling_seconds":60,"balance_max_age_s":30,"publish_allowed_prefixes":[],"publish_min_interval_s":4}`,
			`[]`), false},
		{"eight scenarios", automationDoc(defaultAutomationSettings, "["+strings.Join(nineScenarios[:8], ",")+"]"), true},
	}
	for _, tc := range cases {
		err := ValidateDocument([]byte(tc.doc), automationSchema(t))
		if tc.ok && err != nil {
			t.Errorf("%s: expected acceptance, got %v", tc.name, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s: expected rejection", tc.name)
		}
	}
}

// The five scenario item schemas are copies of the rule item schemas. A
// change to one side must land on the other, or the editor and the service
// disagree about what a condition or action may contain.
func TestScenarioItemSchemasMatchTheRuleItemSchemas(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(automationSchema(t), &schema); err != nil {
		t.Fatal(err)
	}
	props := func(v any, path ...string) map[string]any {
		t.Helper()
		node, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("schema node is %T, want object", v)
		}
		for _, key := range path {
			node, ok = node[key].(map[string]any)
			if !ok {
				t.Fatalf("schema path %v missing at %q", path, key)
			}
		}
		return node
	}
	root := props(schema, "properties")
	rule := props(root, "rules", "items")
	ruleConditions := props(rule, "properties", "conditions", "items")
	ruleActions := props(rule, "properties", "actions", "items")
	scenario := props(root, "scenarios", "items")

	copies := []struct {
		name string
		got  map[string]any
		want map[string]any
	}{
		{"when.conditions", props(scenario, "properties", "when", "properties", "conditions", "items"), ruleConditions},
		{"until.conditions", props(scenario, "properties", "until", "properties", "conditions", "items"), ruleConditions},
		{"on_start.actions", props(scenario, "properties", "on_start", "properties", "actions", "items"), ruleActions},
		{"on_end.actions", props(scenario, "properties", "on_end", "properties", "actions", "items"), ruleActions},
		{"while.actions", props(scenario, "properties", "while", "properties", "actions", "items"), ruleActions},
	}
	for _, c := range copies {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s schema differs from the rule item schema", c.name)
		}
	}
}
