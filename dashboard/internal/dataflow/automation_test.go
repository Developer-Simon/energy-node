package dataflow

import (
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

const rulesDoc = `{"version":1,"settings":{},"rules":[
 {"id":"ueberschuss wallbox","name":"Überschuss in Wallbox","enabled":true,
  "conditions":[{"type":"balance_threshold","field":"grid_export","comparison":"above","threshold":1500},
                {"type":"time_window","start":"08:00","end":"18:00"}],
  "actions":[{"type":"publish","topic":"wallbox/current/set","payload_source":"constant","payload":"10"}]},
 {"id":"akku_leer","name":"Akku leer, WP aus","enabled":false,
  "conditions":[{"type":"entity_value","entity_id":"batterie_soc","comparison":"below","threshold":20},
                {"type":"topic_value","topic":"werkstatt/status","json_key":"apower","comparison":"above","threshold":10}],
  "actions":[{"type":"publish","topic":"wp/enable/set","payload_source":"topic","source_topic":"bms/bank_a/voltage"},
             {"type":"notification","severity":"info","title":"x","message":"y"}]}]}`

func automationDevices() []registry.DeviceView {
	return append(testDevices(),
		registry.DeviceView{ID: "batterie", Entities: []registry.EntityView{{UniqueID: "batterie_soc", StateTopic: "batterie/soc"}}})
}

func TestAutomationGraphBuildsRuleNodesAndEdges(t *testing.T) {
	graph, err := automationGraph([]byte(rulesDoc), NewIndex(automationDevices()))
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 2 {
		t.Fatalf("nodes = %+v", graph.Nodes)
	}
	first := graph.Nodes[0]
	if first.VirtualID != "rule:ueberschuss%20wallbox" || first.Kind != "rule" || first.Label != "Überschuss in Wallbox" || len(first.Conditions) != 2 || first.Link.Tab != "automations" || first.Link.Target != "ueberschuss wallbox" {
		t.Errorf("rule node = %+v", first)
	}
	if graph.Nodes[1].Enabled == nil || *graph.Nodes[1].Enabled {
		t.Error("enabled=false must be carried over")
	}
	byID := map[string]Edge{}
	for _, edge := range graph.Edges {
		byID[edge.ID] = edge
	}
	balance := byID["auto:rule:ueberschuss%20wallbox/c0"]
	if balance.From.VirtualID != BalanceID || balance.To.VirtualID != first.VirtualID || balance.Details.Field != "grid_export" || balance.TitleKey != "devicemap.flow.title.balance_threshold" {
		t.Errorf("balance edge = %+v", balance)
	}
	if _, ok := byID["auto:rule:ueberschuss%20wallbox/c1"]; ok {
		t.Error("a time window has no edge")
	}
	action := byID["auto:rule:ueberschuss%20wallbox/a0"]
	if action.From.VirtualID != first.VirtualID || action.To.DeviceID != "wallbox" || action.Details.Value != "10" {
		t.Errorf("action edge = %+v", action)
	}
	entity := byID["auto:rule:akku_leer/c0"]
	if entity.From.DeviceID != "batterie" || entity.From.EntityID != "batterie_soc" {
		t.Errorf("entity edge = %+v", entity)
	}
	topic := byID["auto:rule:akku_leer/c1"]
	if topic.From.EntityID != "werkstatt_power" {
		t.Errorf("topic edge = %+v", topic)
	}
	source := byID["auto:rule:akku_leer/a0/src"]
	if source.From.DeviceID != "bms_bank_a" || source.Details.Kind != "payload_topic" {
		t.Errorf("source edge = %+v", source)
	}
	if len(graph.Unresolved) != 1 || graph.Unresolved[0].ID != "auto:rule:akku_leer/a0" || graph.Unresolved[0].Reason != ReasonNoConsumer {
		t.Errorf("unresolved = %+v", graph.Unresolved)
	}
}

func TestAutomationGraphRejectsBrokenJSON(t *testing.T) {
	if _, err := automationGraph([]byte(`{"rules":`), NewIndex(nil)); err == nil {
		t.Fatal("expected an error")
	}
}

func TestAutomationGraphLinksTheServiceDeviceToEveryRule(t *testing.T) {
	devices := append(automationDevices(), registry.DeviceView{ID: "automation"})
	graph, err := automationGraph([]byte(rulesDoc), NewIndex(devices))
	if err != nil {
		t.Fatal(err)
	}
	runs := 0
	for _, edge := range graph.Edges {
		if edge.Details.Kind != "executes" {
			continue
		}
		runs++
		if edge.From.DeviceID != "automation" || edge.To.VirtualID == "" || edge.TitleKey != "devicemap.flow.title.executes" || edge.Link.Tab != "automations" {
			t.Errorf("executes edge = %+v", edge)
		}
	}
	if runs != 2 {
		t.Fatalf("want one executes edge per rule, got %d", runs)
	}
}

func TestAutomationGraphWithoutServiceDeviceHasNoExecutesEdge(t *testing.T) {
	graph, _ := automationGraph([]byte(rulesDoc), NewIndex(automationDevices()))
	for _, edge := range graph.Edges {
		if edge.Details.Kind == "executes" {
			t.Fatalf("unexpected executes edge %+v", edge)
		}
	}
}

const scenarioDoc = `{"version":1,"settings":{},"rules":[],"scenarios":[{
 "id":"nulleinspeisung","name":"Nulleinspeisung","enabled":true,"template":"zero_feed","history_enabled":true,
 "when":{"conditions":[{"type":"sun_window","from":"sunrise","to":"sunset"}]},
 "until":null,
 "on_start":{"actions":[{"type":"notification","severity":"info","title":"Nulleinspeisung","message":"startet"}]},
 "while":{"mode":"regulate","regulator":{
   "output":{"topic":"bkw/limit/set","entity_id":"bkw_limit","min":50,"max":800,"step":10,"decimals":0},
   "direction":"producer","target_w":30,"deadband_w":20,"gain":0.8,
   "base":{"source":"entity","entity_id":"bkw_power","topic":"bkw/state","value_template":"{{ value_json.power }}"},
   "fallback":{"payload":"800"}}},
 "on_end":{"actions":[{"type":"publish","topic":"bkw/limit/set","payload_source":"constant","payload":"800"}]}}]}`

func scenarioDevices() []registry.DeviceView {
	return append(testDevices(),
		registry.DeviceView{ID: "bkw", Entities: []registry.EntityView{{UniqueID: "bkw_power", StateTopic: "bkw/state"}}},
		registry.DeviceView{ID: "wr", Entities: []registry.EntityView{{UniqueID: "bkw_limit", StateTopic: "bkw/limit", CommandTopic: "bkw/limit/set"}}})
}

func TestAutomationGraphBuildsScenarioEdges(t *testing.T) {
	graph, err := automationGraph([]byte(scenarioDoc), NewIndex(scenarioDevices()))
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("nodes = %+v", graph.Nodes)
	}
	node := graph.Nodes[0]
	if node.VirtualID != "rule:nulleinspeisung" || node.Kind != "rule" || node.Label != "Nulleinspeisung" || node.Link.Tab != "automations" || node.Link.Target != "nulleinspeisung" {
		t.Errorf("scenario node = %+v", node)
	}
	byID := map[string]Edge{}
	for _, edge := range graph.Edges {
		byID[edge.ID] = edge
	}
	out := byID["auto:rule:nulleinspeisung/out"]
	if out.From.VirtualID != node.VirtualID || out.To.DeviceID != "wr" || out.To.EntityID != "bkw_limit" || out.Details.Kind != "regulator_output" || out.TitleKey != "devicemap.flow.title.regulator_output" {
		t.Errorf("output edge = %+v", out)
	}
	base := byID["auto:rule:nulleinspeisung/base"]
	if base.From.DeviceID != "bkw" || base.From.EntityID != "bkw_power" || base.To.VirtualID != node.VirtualID || base.Details.Kind != "regulator_base" || base.TitleKey != "devicemap.flow.title.regulator_base" {
		t.Errorf("base edge = %+v", base)
	}
	end := byID["auto:rule:nulleinspeisung/e0"]
	if end.From.VirtualID != node.VirtualID || end.To.DeviceID != "wr" {
		t.Errorf("on_end edge = %+v", end)
	}
	if len(graph.Unresolved) != 0 {
		t.Errorf("unresolved = %+v", graph.Unresolved)
	}
}

func TestAutomationGraphReportsAnUnknownRegulatorOutput(t *testing.T) {
	doc := strings.Replace(scenarioDoc, `"topic":"bkw/limit/set","entity_id"`, `"topic":"nobody/limit/set","entity_id"`, 1)
	graph, err := automationGraph([]byte(doc), NewIndex(scenarioDevices()))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range graph.Unresolved {
		if item.ID == "auto:rule:nulleinspeisung/out" {
			found = true
			if item.Item != "nulleinspeisung" || item.Reason != ReasonNoConsumer || item.TitleKey != "devicemap.flow.title.regulator_output" {
				t.Errorf("unresolved output = %+v", item)
			}
		}
	}
	if !found {
		t.Fatalf("no unresolved output in %+v", graph.Unresolved)
	}
}
