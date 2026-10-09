package dataflow

import (
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

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
