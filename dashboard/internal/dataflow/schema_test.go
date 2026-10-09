package dataflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func batterySchema(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "services", "battery_soc", "battery_soc_devices.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func batteryDevices() []registry.DeviceView {
	return []registry.DeviceView{
		{ID: "bms_bank_a", Entities: []registry.EntityView{{UniqueID: "bank_a_voltage", StateTopic: "bms/bank_a/voltage"}}},
		{ID: "bms_bank_b", Entities: []registry.EntityView{{UniqueID: "bank_b_voltage", StateTopic: "bms/bank_b/voltage"}}},
		{ID: "batterie_wr", Entities: []registry.EntityView{{UniqueID: "batterie_dc_strom", StateTopic: "outstation/batterie_wr/state", ValueTemplate: "{{ value_json.dc_current }}"}}},
		{ID: "bank", Entities: []registry.EntityView{{UniqueID: "bank_soc", StateTopic: "bank/soc"}}},
	}
}

func TestServiceGraphEndsAtTheServiceDevice(t *testing.T) {
	doc := []byte(`[{"id":"bank","name":"Batterie SoC","system_type":"dc_only","bank_b_enabled":true,"topology":"series",
		"bank_a_voltage_topic":"bms/bank_a/voltage","bank_b_voltage_topic":"bms/bank_b/voltage",
		"charger_dc_power_topic":"outstation/batterie_wr/state","charger_dc_power_json_key":"dc_current","charger_dc_power_unit":"A"}]`)
	graph, err := serviceGraph("battery_soc_devices", "", "Batterie", doc, batterySchema(t), NewIndex(batteryDevices()))
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 0 {
		t.Errorf("the device bank exists, no virtual node expected, got %+v", graph.Nodes)
	}
	byField := map[string]Edge{}
	for _, edge := range graph.Edges {
		byField[edge.Details.Field] = edge
	}
	a := byField["bank_a_voltage_topic"]
	if a.From.DeviceID != "bms_bank_a" || a.To.DeviceID != "bank" || a.Cat != CatService {
		t.Errorf("bank A edge = %+v", a)
	}
	if a.TitleKey != "dataflow.battery_soc_devices.bank_a_voltage" || a.Title != "Voltage bank A" {
		t.Errorf("bank A title = %q / %q", a.Title, a.TitleKey)
	}
	if a.Link != (Link{Tab: "config", Target: "battery_soc_devices", Item: "bank"}) {
		t.Errorf("link = %+v", a.Link)
	}
	dc := byField["charger_dc_power_topic"]
	if dc.From.EntityID != "batterie_dc_strom" || dc.Details.JSONKey != "dc_current" || dc.Details.Unit != "A" {
		t.Errorf("DC edge = %+v", dc)
	}
	if _, ok := byField["bank_b_voltage_topic"]; !ok {
		t.Error("bank B is active in series topology and must have an edge")
	}
}

func TestServiceGraphSkipsFieldsOfInactiveBranches(t *testing.T) {
	doc := []byte(`[{"id":"bank","name":"Batterie SoC","bank_b_enabled":false,
		"bank_a_voltage_topic":"bms/bank_a/voltage","bank_b_voltage_topic":"bms/bank_b/voltage"}]`)
	graph, err := serviceGraph("battery_soc_devices", "", "Batterie", doc, batterySchema(t), NewIndex(batteryDevices()))
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range graph.Edges {
		if edge.Details.Field == "bank_b_voltage_topic" {
			t.Fatal("bank B is disabled, the service does not read its topic")
		}
	}
}

func TestServiceGraphUsesAVirtualNodeWithoutDevice(t *testing.T) {
	doc := []byte(`[{"id":"neu","name":"Neue Bank","bank_b_enabled":false,"bank_a_voltage_topic":"gibt/es/nicht"}]`)
	graph, err := serviceGraph("battery_soc_devices", "", "Batterie", doc, batterySchema(t), NewIndex(batteryDevices()))
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 1 || graph.Nodes[0].VirtualID != "service:battery_soc_devices/neu" || graph.Nodes[0].Label != "Neue Bank" {
		t.Fatalf("nodes = %+v", graph.Nodes)
	}
	if len(graph.Unresolved) != 1 {
		t.Fatalf("unresolved = %+v", graph.Unresolved)
	}
	u := graph.Unresolved[0]
	if u.Reason != ReasonNoProducer || u.Target.VirtualID != "service:battery_soc_devices/neu" || u.Topic != "gibt/es/nicht" || u.Direction != "input" {
		t.Errorf("unresolved = %+v", u)
	}
}

func TestServiceGraphReportsAnInvalidEntry(t *testing.T) {
	doc := []byte(`[{"id":"kaputt","name":"Kaputt","bank_a_voltage_topic":42}]`)
	graph, err := serviceGraph("battery_soc_devices", "", "Batterie", doc, batterySchema(t), NewIndex(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Unresolved) != 1 || graph.Unresolved[0].Reason != ReasonConfigInvalid || graph.Unresolved[0].Item != "kaputt" {
		t.Fatalf("unresolved = %+v", graph.Unresolved)
	}
}

func TestServiceGraphWithoutAnnotationsIsEmpty(t *testing.T) {
	graph, err := serviceGraph("demo_devices", "", "Demo", []byte(`[{"id":"a","topic":"x"}]`),
		[]byte(`{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"topic":{"type":"string","format":"mqtt-topic","x-dataflow":false}}}}`), NewIndex(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes)+len(graph.Edges)+len(graph.Unresolved) != 0 {
		t.Fatalf("graph = %+v", graph)
	}
}
