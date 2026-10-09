package dataflow

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

type fakeSource struct {
	docs    []config.Document
	files   map[string]string
	schemas map[string]string
}

func (f fakeSource) Scan() ([]config.Document, error) { return f.docs, nil }
func (f fakeSource) Read(name string) (json.RawMessage, error) {
	if data, ok := f.files[name]; ok {
		return json.RawMessage(data), nil
	}
	return nil, os.ErrNotExist
}
func (f fakeSource) ReadSchema(name string) (json.RawMessage, error) {
	if data, ok := f.schemas[name]; ok {
		return json.RawMessage(data), nil
	}
	return nil, os.ErrNotExist
}

func TestBuildMergesAdaptersAndToleratesBrokenFiles(t *testing.T) {
	src := fakeSource{
		docs: []config.Document{{Name: "automation_rules"}, {Name: "kaputt_devices"}, {Name: "shelly_devices"}},
		files: map[string]string{
			"automation_rules": rulesDoc,
			"kaputt_devices":   `{`,
			"shelly_devices":   `[]`,
		},
		schemas: map[string]string{
			"automation_rules": `{}`,
			"kaputt_devices":   `{"type":"array","items":{"type":"object","properties":{"t":{"type":"string","format":"mqtt-topic","x-dataflow":{"direction":"input","label":"T"}}}}}`,
			"shelly_devices":   `{"type":"array","items":{"type":"object"}}`,
		},
	}
	graph := Build(src, automationDevices())
	if len(graph.Nodes) != 2 {
		t.Errorf("nodes = %+v", graph.Nodes)
	}
	found := false
	for _, u := range graph.Unresolved {
		if u.Config == "kaputt_devices" && u.Reason == ReasonConfigInvalid {
			found = true
		}
	}
	if !found {
		t.Errorf("a broken file must appear as config_invalid, got %+v", graph.Unresolved)
	}
}

func TestBuildWithoutSourceIsEmptyNotNull(t *testing.T) {
	data, err := json.Marshal(Build(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"nodes":[],"edges":[],"unresolved":[]}` {
		t.Fatalf("json = %s", data)
	}
}

func testDevices() []registry.DeviceView {
	return []registry.DeviceView{
		{ID: "bms_bank_a", Entities: []registry.EntityView{
			{UniqueID: "bank_a_voltage", StateTopic: "bms/bank_a/voltage"},
		}},
		{ID: "werkstatt", Entities: []registry.EntityView{
			{UniqueID: "werkstatt_power", StateTopic: "werkstatt/status", ValueTemplate: "{{ value_json.apower }}"},
			{UniqueID: "werkstatt_voltage", StateTopic: "werkstatt/status", ValueTemplate: "{{ value_json.voltage }}"},
		}},
		{ID: "wallbox", Entities: []registry.EntityView{
			{UniqueID: "wallbox_current", StateTopic: "wallbox/current", CommandTopic: "wallbox/current/set"},
		}},
	}
}

func TestIndexResolvesInputsByTopicAndJSONKey(t *testing.T) {
	ix := NewIndex(testDevices())
	if ref, reason := ix.Input("bms/bank_a/voltage", ""); reason != "" || ref.DeviceID != "bms_bank_a" || ref.EntityID != "bank_a_voltage" {
		t.Errorf("bare topic: %+v %q", ref, reason)
	}
	if ref, reason := ix.Input("werkstatt/status", "voltage"); reason != "" || ref.EntityID != "werkstatt_voltage" {
		t.Errorf("json key: %+v %q", ref, reason)
	}
	if _, reason := ix.Input("werkstatt/status", ""); reason != ReasonKeyUnmatched {
		t.Errorf("shared JSON topic without key must not pick an entity, got %q", reason)
	}
	if _, reason := ix.Input("werkstatt/status", "temperature"); reason != ReasonKeyUnmatched {
		t.Errorf("unknown key: %q", reason)
	}
	if _, reason := ix.Input("nobody/publishes", ""); reason != ReasonNoProducer {
		t.Errorf("unknown topic: %q", reason)
	}
}

func TestIndexResolvesOutputsByCommandTopic(t *testing.T) {
	ix := NewIndex(testDevices())
	if ref, reason := ix.Output("wallbox/current/set"); reason != "" || ref.DeviceID != "wallbox" {
		t.Errorf("command topic: %+v %q", ref, reason)
	}
	if _, reason := ix.Output("wallbox/current"); reason != ReasonNoConsumer {
		t.Errorf("a state topic is no consumer: %q", reason)
	}
}

func TestIndexKnowsDevicesAndEntities(t *testing.T) {
	ix := NewIndex(testDevices())
	if !ix.HasDevice("wallbox") || ix.HasDevice("gone") {
		t.Error("HasDevice")
	}
	if ref, ok := ix.Entity("werkstatt_power"); !ok || ref.DeviceID != "werkstatt" {
		t.Errorf("Entity: %+v %v", ref, ok)
	}
}

func TestVirtualIDsEscapeFreeText(t *testing.T) {
	if got := RuleID("Überschuss in Wallbox/2"); got != "rule:%C3%9Cberschuss%20in%20Wallbox%2F2" {
		t.Errorf("RuleID = %q", got)
	}
	if got := ServiceID("battery_soc_devices", "bank a"); got != "service:battery_soc_devices/bank%20a" {
		t.Errorf("ServiceID = %q", got)
	}
	if got := ServiceID("demo_devices", ""); got != "service:demo_devices" {
		t.Errorf("ServiceID without item = %q", got)
	}
}
