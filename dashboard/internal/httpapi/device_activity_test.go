package httpapi

import (
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func activityTime(clock string) time.Time {
	t, err := time.Parse(time.RFC3339, clock)
	if err != nil {
		panic(err)
	}
	return t
}

func activityLabels(entries []ActivityEntry) []string {
	labels := make([]string, 0, len(entries))
	for _, entry := range entries {
		labels = append(labels, entry.Label+"="+entry.Value)
	}
	return labels
}

func TestActivitySplitsJSONPayload(t *testing.T) {
	dev := registry.DeviceView{Name: "Wallbox", Entities: []registry.EntityView{{
		UniqueID:    "wb_state",
		Name:        "State",
		StateTopic:  "wb/state",
		LastMessage: &registry.MQTTMessage{Topic: "wb/state", Payload: `{"a":1,"b":{"c":2}}`, At: activityTime("2026-10-18T10:00:00Z")},
	}}}

	entries := buildActivity(dev, nil)

	want := []string{"a=1", "b.c=2"}
	if got := activityLabels(entries); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("labels = %v, want %v", got, want)
	}
	if entries[0].Kind != "message" || entries[0].Key != "msg-wb_state-2026-10-18T10:00:00Z-a" {
		t.Fatalf("first entry = %+v", entries[0])
	}
}

func TestActivityScalarUsesEntityName(t *testing.T) {
	dev := registry.DeviceView{Name: "Wallbox", Entities: []registry.EntityView{{
		UniqueID:    "wb_status",
		Name:        "Wallbox Status",
		LastMessage: &registry.MQTTMessage{Topic: "wb/status", Payload: "online", At: activityTime("2026-10-18T10:00:00Z")},
	}}}

	entries := buildActivity(dev, nil)

	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Label != "Status" || entries[0].Value != "online" || entries[0].Key != "msg-wb_status-2026-10-18T10:00:00Z-value" {
		t.Fatalf("entry = %+v", entries[0])
	}
}

func TestActivityCountsSharedTopicOnce(t *testing.T) {
	shared := &registry.MQTTMessage{Topic: "energy/balance", Payload: `{"pv":1}`, At: activityTime("2026-10-18T10:00:00Z")}
	dev := registry.DeviceView{Name: "Energy", Entities: []registry.EntityView{
		{UniqueID: "pv", Name: "PV", LastMessage: shared},
		{UniqueID: "grid", Name: "Grid", LastMessage: &registry.MQTTMessage{Topic: "energy/balance", Payload: `{"pv":1}`, At: shared.At}},
		{UniqueID: "other", Name: "Other", LastMessage: &registry.MQTTMessage{Topic: "other", Payload: "1", At: activityTime("2026-10-18T09:00:00Z")}},
	}}

	entries := buildActivity(dev, nil)

	want := []string{"pv=1", "Other=1"}
	if got := activityLabels(entries); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("labels = %v, want %v", got, want)
	}
}

func TestActivityCommandsFirst(t *testing.T) {
	dev := registry.DeviceView{Name: "Relay", Entities: []registry.EntityView{{
		UniqueID:    "relay",
		Name:        "Relay",
		LastMessage: &registry.MQTTMessage{Topic: "relay/state", Payload: "ON", At: activityTime("2026-10-18T10:00:00Z")},
	}}}
	commands := []CommandAction{
		{At: activityTime("2026-10-18T10:01:00Z"), EntityID: "relay", Result: "published"},
		{At: activityTime("2026-10-18T10:02:00Z"), EntityID: "relay", Result: "failed"},
		{At: activityTime("2026-10-18T10:03:00Z"), EntityID: "relay", Result: "published"},
		{At: activityTime("2026-10-18T10:04:00Z"), EntityID: "relay", Result: "published"},
	}

	entries := buildActivity(dev, commands)

	if len(entries) != 4 {
		t.Fatalf("entries = %d, want 4 (three commands, one message)", len(entries))
	}
	for i := 0; i < 3; i++ {
		if entries[i].Kind != "command" {
			t.Fatalf("entry %d kind = %q, want command", i, entries[i].Kind)
		}
	}
	if !entries[0].OK || entries[1].OK || !entries[2].OK {
		t.Fatalf("ok flags = %v %v %v, want true false true", entries[0].OK, entries[1].OK, entries[2].OK)
	}
	if entries[0].Key != "cmd-2026-10-18T10:01:00Z-relay" || entries[0].Label != "relay" {
		t.Fatalf("command entry = %+v", entries[0])
	}
	if entries[3].Kind != "message" || entries[3].Label != "Relay" || entries[3].Value != "ON" {
		t.Fatalf("message entry = %+v", entries[3])
	}
}

func TestActivityEmptyPayloadUsesEmptyKey(t *testing.T) {
	dev := registry.DeviceView{Name: "Sensor", Entities: []registry.EntityView{{
		UniqueID:    "sensor",
		Name:        "Sensor",
		LastMessage: &registry.MQTTMessage{Topic: "sensor/state", Payload: "  ", At: activityTime("2026-10-18T10:00:00Z")},
	}}}

	entries := buildActivity(dev, nil)

	if len(entries) != 1 || entries[0].Key != "msg-sensor-2026-10-18T10:00:00Z-empty" || entries[0].Label != "sensor/state" || entries[0].Value != "" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestActivityFormatsJSONValuesLikeJavaScript(t *testing.T) {
	dev := registry.DeviceView{Name: "Meter", Entities: []registry.EntityView{{
		UniqueID:    "meter",
		Name:        "Meter",
		LastMessage: &registry.MQTTMessage{Topic: "meter", Payload: `{"n":1.50,"big":1e21,"nul":null,"arr":[1,"x",{"k":true}],"empty":{},"dup":1,"dup":2,"10":"i","9":"j"}`, At: activityTime("2026-10-18T10:00:00Z")},
	}}}

	entries := buildActivity(dev, nil)

	want := []string{"9=j", "10=i", "n=1.5", "big=1e+21", "nul=–", `arr=[1,"x",{"k":true}]`, "empty={}", "dup=2"}
	got := activityLabels(entries)
	if len(got) != len(want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("labels = %v, want %v", got, want)
		}
	}
}
