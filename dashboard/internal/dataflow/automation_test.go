package dataflow

import (
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
