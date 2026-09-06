package registry

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSnapshotPreservesOptionalIcon(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{
		Device: DeviceInfo{ID: "battery", Name: "Battery"},
		Entity: EntityInfo{UniqueID: "battery_soc", ObjectID: "soc", Name: "Battery SoC", Icon: "mdi:battery"},
	})

	views := reg.Snapshot()
	if len(views) != 1 || len(views[0].Entities) != 1 {
		t.Fatalf("unexpected snapshot: %#v", views)
	}
	if views[0].Entities[0].Icon != "mdi:battery" {
		t.Fatalf("icon = %q, want %q", views[0].Entities[0].Icon, "mdi:battery")
	}

	data, err := json.Marshal(views[0].Entities[0])
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	if encoded["icon"] != "mdi:battery" {
		t.Fatalf("snapshot JSON does not contain icon: %s", data)
	}
}

func TestSnapshotOmitsMissingIcon(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{
		Device: DeviceInfo{ID: "node"},
		Entity: EntityInfo{UniqueID: "node_temp", ObjectID: "temp", Name: "Temperature"},
	})

	data, err := json.Marshal(reg.Snapshot()[0].Entities[0])
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := encoded["icon"]; ok {
		t.Fatalf("snapshot JSON unexpectedly contains missing icon: %s", data)
	}
}

func TestSnapshotIncludesFirmwareAndBoundedLastMQTTMessage(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{
		Device: DeviceInfo{ID: "node", Firmware: "1.2.3"},
		Entity: EntityInfo{UniqueID: "node_temp", StateTopic: "state/node/temp"},
	})
	at := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	payload := []byte(strings.Repeat("x", maxLastMessagePayload+20))
	reg.UpdateTopicWithQoS("state/node/temp", payload, true, 1, at)

	view := reg.Snapshot()[0]
	if view.Firmware != "1.2.3" {
		t.Fatalf("firmware = %q, want %q", view.Firmware, "1.2.3")
	}
	message := view.Entities[0].LastMessage
	if message == nil || message.Topic != "state/node/temp" || !message.Retained || message.QoS != 1 || !message.At.Equal(at) {
		t.Fatalf("last message = %#v", message)
	}
	if len(message.Payload) != maxLastMessagePayload+3 || !strings.HasSuffix(message.Payload, "...") {
		t.Fatalf("last message payload length/content = %d/%q", len(message.Payload), message.Payload[len(message.Payload)-8:])
	}
}

func TestAvailabilityUpdatesLastMQTTMessageWithoutChangingStateFreshness(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "node"}, Entity: EntityInfo{
		UniqueID: "node_temp", StateTopic: "state/node/temp", AvailabilityTopic: "status/node", PayloadAvailable: "online",
	}})
	stateAt := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	availabilityAt := stateAt.Add(time.Minute)
	reg.UpdateTopicWithQoS("state/node/temp", []byte("21"), false, 0, stateAt)
	reg.UpdateTopicWithQoS("status/node", []byte("online"), true, 2, availabilityAt)

	entity := reg.Snapshot()[0].Entities[0]
	if entity.LastMessage == nil || entity.LastMessage.Topic != "status/node" || entity.LastMessage.QoS != 2 {
		t.Fatalf("last availability message = %#v", entity.LastMessage)
	}
	if !entity.LastSeen.Equal(stateAt) || entity.Stale {
		t.Fatalf("state freshness changed after availability replay: %#v", entity)
	}
}

func TestSnapshotDerivesStableParentAndChildRelations(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "child-b", Name: "Child B", ViaDevice: "parent"}, Entity: EntityInfo{UniqueID: "b"}})
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "parent", Name: "Parent"}, Entity: EntityInfo{UniqueID: "parent_entity"}})
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "child-a", Name: "Child A", ViaDevice: "parent"}, Entity: EntityInfo{UniqueID: "a"}})

	parent, ok := reg.Get("parent")
	if !ok || len(parent.Relations) != 2 || parent.Relations[0].ID != "child-a" || parent.Relations[1].ID != "child-b" {
		t.Fatalf("parent relations = %#v", parent.Relations)
	}
	child, ok := reg.Get("child-a")
	if !ok || len(child.Relations) != 1 || child.Relations[0].Kind != "parent" || child.Relations[0].ID != "parent" {
		t.Fatalf("child relations = %#v", child.Relations)
	}
}

func TestSetRelationOverridesAddsEdgesWithoutLosingViaDeviceRelations(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "child-a", Name: "Child A", ViaDevice: "parent"}, Entity: EntityInfo{UniqueID: "a"}})
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "parent", Name: "Parent"}, Entity: EntityInfo{UniqueID: "parent_entity"}})
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "standalone", Name: "Standalone"}, Entity: EntityInfo{UniqueID: "s"}})

	reg.SetRelationOverrides([]RelationOverride{
		{ID: "relation-1", ChildID: "standalone", ParentID: "parent", Kind: "via_device"},
	})

	parent, ok := reg.Get("parent")
	if !ok || len(parent.Relations) != 2 {
		t.Fatalf("parent relations = %#v, want via_device child + override child", parent.Relations)
	}
	hasChild := func(relations []DeviceRelation, id string) bool {
		for _, relation := range relations {
			if relation.Kind == "child" && relation.ID == id {
				return true
			}
		}
		return false
	}
	if !hasChild(parent.Relations, "child-a") {
		t.Fatalf("parent relations lost the via_device-derived child: %#v", parent.Relations)
	}
	if !hasChild(parent.Relations, "standalone") {
		t.Fatalf("parent relations missing the override-derived child: %#v", parent.Relations)
	}

	standalone, ok := reg.Get("standalone")
	if !ok || len(standalone.Relations) != 1 || standalone.Relations[0].Kind != "parent" || standalone.Relations[0].ID != "parent" {
		t.Fatalf("standalone relations = %#v, want a single override-derived parent", standalone.Relations)
	}

	// Snapshot() takes a different code path than Get() (see snapshotLocked
	// vs relationsLocked) and must agree with it.
	for _, view := range reg.Snapshot() {
		if view.ID == "parent" && !hasChild(view.Relations, "standalone") {
			t.Fatalf("Snapshot() parent relations missing override-derived child: %#v", view.Relations)
		}
	}

	reg.SetRelationOverrides(nil)
	standalone, ok = reg.Get("standalone")
	if !ok || len(standalone.Relations) != 0 {
		t.Fatalf("standalone relations after clearing overrides = %#v, want none", standalone.Relations)
	}
}

func TestCommandReturnsToggleMetadataAndSnapshotMarksEntityCommandable(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{
		Device: DeviceInfo{ID: "shelly"},
		Entity: EntityInfo{UniqueID: "shelly_relay", Component: "switch", StateTopic: "state/shelly/relay", CommandTopic: "shelly/relay/0/set", PayloadOn: "ON", PayloadOff: "OFF"},
	})
	reg.UpdateState("state/shelly/relay", []byte("ON"), false, time.Now())

	entity := reg.Snapshot()[0].Entities[0]
	if !entity.Commandable {
		t.Fatal("switch with complete command metadata was not marked commandable")
	}
	command, ok := reg.Command("shelly_relay")
	if !ok || command.Topic != "shelly/relay/0/set" || command.PayloadOn != "ON" || command.PayloadOff != "OFF" {
		t.Fatalf("command = %#v, ok = %v", command, ok)
	}
}

func TestSnapshotPreservesDiscoveryJSONForWebViews(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{
		Device:  DeviceInfo{ID: "node"},
		Entity:  EntityInfo{UniqueID: "node_temp", ObjectID: "temp"},
		RawJSON: `{"name":"Temperature"}`,
	})

	views := reg.Snapshot()
	if len(views) != 1 || len(views[0].Entities) != 1 {
		t.Fatalf("unexpected discovery JSON snapshot: %#v", views)
	}
	if views[0].Entities[0].DiscoveryJSON != `{"name":"Temperature"}` {
		t.Fatalf("discovery JSON = %q", views[0].Entities[0].DiscoveryJSON)
	}
}

func TestExtractValueBooleanTemplate(t *testing.T) {
	payload := []byte(`{"undervoltage_now":true,"throttled_now":false}`)

	tests := []struct {
		name, template, want string
	}{
		{name: "true", template: `{{ 'ON' if value_json.undervoltage_now else 'OFF' }}`, want: "ON"},
		{name: "false", template: `{{ 'ON' if value_json.throttled_now else 'OFF' }}`, want: "OFF"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := extractValue(payload, test.template); got != test.want {
				t.Fatalf("extractValue() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestExtractValueJSONField(t *testing.T) {
	if got := extractValue([]byte(`{"soc":42.5}`), `{{ value_json.soc }}`); got != "42.5" {
		t.Fatalf("extractValue() = %q, want %q", got, "42.5")
	}
}

func TestExtractValueNestedServiceUpdateTemplate(t *testing.T) {
	payload := []byte(`{"service_last_updates":{"apsystems":1785875793}}`)
	template := `{{ value_json.service_last_updates['apsystems'] | default(0) | int | timestamp_local }}`
	want := time.Unix(1785875793, 0).Local().Format(time.RFC3339)
	if got := extractValue(payload, template); got != want {
		t.Fatalf("extractValue() = %q, want %q", got, want)
	}
}

func TestExtractValueJSONNull(t *testing.T) {
	if got := extractValue([]byte(`{"tailscale_connected":null}`), `{{ value_json.tailscale_connected }}`); got != "" {
		t.Fatalf("extractValue() = %q, want empty value", got)
	}
}

func TestRestoreStateExposesRuntimeCacheUntilFreshStateArrives(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "node"}, Entity: EntityInfo{UniqueID: "power", StateTopic: "state/power"}})
	lastSeen := time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC)
	if !reg.RestoreState("node", "power", "42", true, true, lastSeen) {
		t.Fatal("runtime cache state was not restored")
	}
	entity := reg.Snapshot()[0].Entities[0]
	if entity.Source != "runtime-cache" || entity.Freshness != "stale" || entity.Value != "42" {
		t.Fatalf("restored entity = %#v", entity)
	}

	reg.UpdateState("state/power", []byte("43"), false, lastSeen.Add(time.Minute))
	entity = reg.Snapshot()[0].Entities[0]
	if entity.Source != "live" || entity.Freshness != "fresh" || entity.Value != "43" {
		t.Fatalf("live entity = %#v", entity)
	}
}

func TestBeginPendingCommandWithholdsValueUntilLiveConfirmation(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "shelly"}, Entity: EntityInfo{UniqueID: "relay", StateTopic: "state/relay", Component: "switch"}})
	reg.UpdateState("state/relay", []byte("OFF"), false, time.Now())

	now := time.Now()
	ok, conflict := reg.BeginPendingCommand("relay", "ON", now, time.Hour)
	if !ok || conflict {
		t.Fatalf("BeginPendingCommand = ok=%v conflict=%v, want ok=true conflict=false", ok, conflict)
	}
	entity := reg.Snapshot()[0].Entities[0]
	if entity.Value != "OFF" || !entity.Pending || entity.PendingValue != "ON" {
		t.Fatalf("entity after begin = %#v, want unchanged value with pending=true", entity)
	}

	// A second command while one is still pending must be rejected, not
	// silently override the running one (P1.3: "Schutz gegen
	// Mehrfachauslösung während eines laufenden Pending-Vorgangs").
	ok, conflict = reg.BeginPendingCommand("relay", "OFF", now.Add(time.Second), time.Hour)
	if ok || !conflict {
		t.Fatalf("second BeginPendingCommand = ok=%v conflict=%v, want ok=false conflict=true", ok, conflict)
	}

	reg.UpdateState("state/relay", []byte("ON"), false, now.Add(2*time.Second))
	entity = reg.Snapshot()[0].Entities[0]
	if entity.Value != "ON" || entity.Pending || entity.Source != "live" || entity.LastCommandResult != "success" {
		t.Fatalf("entity after confirmation = %#v", entity)
	}
}

func TestExpirePendingCommandsTimesOutWithoutRevertingValue(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "shelly"}, Entity: EntityInfo{UniqueID: "relay", StateTopic: "state/relay", Component: "switch"}})
	reg.UpdateState("state/relay", []byte("OFF"), false, time.Now())

	now := time.Now()
	if ok, _ := reg.BeginPendingCommand("relay", "ON", now, 10*time.Second); !ok {
		t.Fatal("BeginPendingCommand failed")
	}

	if changes := reg.ExpirePendingCommands(now.Add(9 * time.Second)); len(changes) != 0 {
		t.Fatalf("expired before deadline: %#v", changes)
	}
	entity := reg.Snapshot()[0].Entities[0]
	if !entity.Pending {
		t.Fatalf("entity resolved before deadline: %#v", entity)
	}

	changes := reg.ExpirePendingCommands(now.Add(11 * time.Second))
	if len(changes) != 1 || changes[0].UniqueID != "relay" {
		t.Fatalf("changes after deadline = %#v", changes)
	}
	entity = reg.Snapshot()[0].Entities[0]
	if entity.Pending || entity.LastCommandResult != "timeout" || entity.Value != "OFF" {
		t.Fatalf("entity after timeout = %#v, want pending=false, last_command_result=timeout, value unchanged", entity)
	}
}

func TestCancelPendingCommandUndoesReservationWithoutMarkingTimeout(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "shelly"}, Entity: EntityInfo{UniqueID: "relay", StateTopic: "state/relay", Component: "switch"}})

	now := time.Now()
	if ok, _ := reg.BeginPendingCommand("relay", "ON", now, time.Hour); !ok {
		t.Fatal("BeginPendingCommand failed")
	}
	reg.CancelPendingCommand("relay")

	entity := reg.Snapshot()[0].Entities[0]
	if entity.Pending || entity.LastCommandResult != "" {
		t.Fatalf("entity after cancel = %#v, want pending=false and no last_command_result", entity)
	}
	if ok, conflict := reg.BeginPendingCommand("relay", "ON", now.Add(time.Second), time.Hour); !ok || conflict {
		t.Fatalf("BeginPendingCommand after cancel = ok=%v conflict=%v, want ok=true conflict=false", ok, conflict)
	}
}

func TestAvailabilityReplayDoesNotMarkStateStale(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "node"}, Entity: EntityInfo{UniqueID: "power", StateTopic: "state/power", AvailabilityTopic: "status/node", PayloadAvailable: "online"}})
	stateAt := time.Now().UTC().Add(-time.Minute)
	topicAt := stateAt.Add(time.Second)
	reg.UpdateState("state/power", []byte("42"), false, stateAt)
	reg.UpdateAvailability("status/node", []byte("online"), true, topicAt)
	entity := reg.Snapshot()[0].Entities[0]
	if entity.Source != "live" || entity.Freshness != "fresh" || entity.Stale || !entity.LastSeen.Equal(stateAt) || !entity.LastTopicAt.Equal(topicAt) {
		t.Fatalf("availability replay changed state freshness: %#v", entity)
	}
}

func TestAvailabilityArrayAggregatesAnyMode(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "node"}, Entity: EntityInfo{
		UniqueID: "power", StateTopic: "state/power", AvailabilityMode: "any",
		Availability: []AvailabilityInfo{
			{Topic: "status/node", PayloadAvailable: "online"},
			{Topic: "status/bridge", PayloadAvailable: "1"},
		},
	}})

	seenAt := time.Now().UTC()
	reg.UpdateAvailability("status/node", []byte("offline"), false, seenAt)
	reg.UpdateAvailability("status/bridge", []byte("1"), false, seenAt.Add(time.Second))
	entity := reg.Snapshot()[0].Entities[0]
	if !entity.Available || len(entity.Availability) != 2 || entity.AvailabilityMode != "any" {
		t.Fatalf("any availability = %#v", entity)
	}

	reg.UpdateAvailability("status/bridge", []byte("0"), false, seenAt.Add(2*time.Second))
	if entity = reg.Snapshot()[0].Entities[0]; entity.Available {
		t.Fatalf("any availability remained online after all topics went offline: %#v", entity)
	}
}

func TestOfflineStateUpdateKeepsLastKnownValue(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "node"}, Entity: EntityInfo{UniqueID: "power", StateTopic: "state/power", AvailabilityTopic: "status/node", PayloadAvailable: "online"}})
	reg.UpdateState("state/power", []byte("42"), false, time.Now().UTC())
	reg.UpdateAvailability("status/node", []byte("offline"), false, time.Now().UTC())
	reg.UpdateState("state/power", []byte("0"), false, time.Now().UTC())

	entity := reg.Snapshot()[0].Entities[0]
	if entity.Value != "42" || !entity.HasValue || entity.Available {
		t.Fatalf("offline entity = %#v, want retained last known value and unavailable state", entity)
	}
}

func TestDuplicateUniqueIDsAreTrackedDeterministically(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "z-node"}, Entity: EntityInfo{UniqueID: "shared", StateTopic: "state/z"}})
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "a-node"}, Entity: EntityInfo{UniqueID: "shared", StateTopic: "state/a"}})

	duplicates := reg.DuplicateUniqueIDs()
	if got := duplicates["shared"]; len(got) != 2 || got[0] != "a-node" || got[1] != "z-node" {
		t.Fatalf("duplicates = %#v, want sorted device IDs", duplicates)
	}

	reg.RemoveEntity("shared")
	if len(reg.DuplicateUniqueIDs()) != 0 {
		t.Fatalf("duplicates after removal = %#v, want none", reg.DuplicateUniqueIDs())
	}
}

func TestUpsertRemovesReplacedTopicMappings(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{
		Device:         DeviceInfo{ID: "node"},
		Entity:         EntityInfo{UniqueID: "power", StateTopic: "state/old", AvailabilityTopic: "status/old"},
		DiscoveryTopic: "homeassistant/sensor/node/power/config",
	})

	newTopics, orphanTopics := reg.UpsertEntity(Discovery{
		Device:         DeviceInfo{ID: "node"},
		Entity:         EntityInfo{UniqueID: "power", StateTopic: "state/new", AvailabilityTopic: "status/new"},
		DiscoveryTopic: "homeassistant/sensor/node/power/config",
	})
	if got, want := newTopics, []string{"state/new", "status/new"}; !equalStrings(got, want) {
		t.Fatalf("new topics = %#v, want %#v", got, want)
	}
	if got, want := orphanTopics, []string{"state/old", "status/old"}; !equalStrings(got, want) {
		t.Fatalf("orphan topics = %#v, want %#v", got, want)
	}
	if got := reg.Topics(); !equalStrings(got, newTopics) {
		t.Fatalf("registry topics = %#v, want %#v", got, newTopics)
	}
}

func TestRemoveDiscoveryUsesRegisteredUniqueID(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{
		Device:         DeviceInfo{ID: "node"},
		Entity:         EntityInfo{UniqueID: "explicit-id", StateTopic: "state/node"},
		DiscoveryTopic: "homeassistant/sensor/node/power/config",
	})

	orphans, removed := reg.RemoveDiscovery("homeassistant/sensor/node/power/config")
	if !removed || !equalStrings(orphans, []string{"state/node"}) {
		t.Fatalf("RemoveDiscovery() = %#v, %v", orphans, removed)
	}
	if devices := reg.Snapshot(); len(devices) != 0 {
		t.Fatalf("snapshot after removal = %#v, want empty", devices)
	}
}

func TestRemoveDeviceReturnsOrphanTopics(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "node"}, Entity: EntityInfo{UniqueID: "a", StateTopic: "shared", AvailabilityTopic: "node/status"}, DiscoveryTopic: "homeassistant/sensor/node/a/config"})
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "node"}, Entity: EntityInfo{UniqueID: "b", StateTopic: "shared", AvailabilityTopic: "node/status"}, DiscoveryTopic: "homeassistant/sensor/node/b/config"})
	if topics, removed := reg.RemoveDevice("missing"); removed || len(topics) != 0 {
		t.Fatalf("unknown removal = %v, %v", topics, removed)
	}
	topics, removed := reg.RemoveDevice("node")
	if !removed || len(topics) != 2 || topics[0] != "node/status" || topics[1] != "shared" {
		t.Fatalf("removed topics = %v, removed = %v", topics, removed)
	}
	if len(reg.Snapshot()) != 0 || len(reg.Topics()) != 0 {
		t.Fatal("device or topics remained after removal")
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func TestStatePayloadValidityIsTracked(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Entity: EntityInfo{
		UniqueID: "soc", StateTopic: "state/soc", ValueTemplate: `{{ value_json.soc }}`,
	}})

	reg.UpdateState("state/soc", []byte(`{"missing":42}`), false, time.Now().UTC())
	entity := reg.Snapshot()[0].Entities[0]
	if entity.PayloadValid || entity.PayloadError == "" {
		t.Fatalf("invalid template payload = %#v", entity)
	}

	reg.UpdateState("state/soc", []byte(`{"soc":42}`), false, time.Now().UTC())
	entity = reg.Snapshot()[0].Entities[0]
	if !entity.PayloadValid || entity.PayloadError != "" {
		t.Fatalf("valid template payload = %#v", entity)
	}
}

func TestUpdateTopicWithQoSReturnsOnlyChangedEntities(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{
		Device: DeviceInfo{ID: "node"},
		Entity: EntityInfo{UniqueID: "node_temp", StateTopic: "state/node/temp", AvailabilityTopic: "status/node", PayloadAvailable: "online"},
	})
	at := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)

	changes := reg.UpdateTopicWithQoS("state/node/temp", []byte("21"), false, 0, at)
	if len(changes) != 1 {
		t.Fatalf("first state publish changes = %#v, want 1 entry", changes)
	}
	change := changes[0]
	if change.DeviceID != "node" || change.UniqueID != "node_temp" || change.StateTopic != "state/node/temp" ||
		change.Value != "21" || !change.HasValue || !change.LastSeen.Equal(at) {
		t.Fatalf("unexpected state change: %#v", change)
	}

	// Republishing the identical value must not be reported as a change.
	if changes := reg.UpdateTopicWithQoS("state/node/temp", []byte("21"), false, 0, at.Add(time.Second)); len(changes) != 0 {
		t.Fatalf("duplicate value changes = %#v, want none", changes)
	}

	availabilityAt := at.Add(time.Minute)
	changes = reg.UpdateTopicWithQoS("status/node", []byte("online"), true, 0, availabilityAt)
	if len(changes) != 1 {
		t.Fatalf("availability publish changes = %#v, want 1 entry", changes)
	}
	change = changes[0]
	if change.DeviceID != "node" || change.UniqueID != "node_temp" || change.StateTopic != "state/node/temp" ||
		!change.Available || !change.HasAvailability || change.Value != "21" {
		t.Fatalf("unexpected availability change: %#v", change)
	}
}

func TestUpdateTopicWithQoSSkipsUnknownTopic(t *testing.T) {
	reg := New()
	if changes := reg.UpdateTopicWithQoS("state/unknown", []byte("1"), false, 0, time.Now().UTC()); len(changes) != 0 {
		t.Fatalf("changes for unknown topic = %#v, want none", changes)
	}
}

func TestTopicSamplesCarryTheLastStatePayload(t *testing.T) {
	reg := New()
	// Two entities share one state topic, a third only brings an availability
	// topic - the latter never records a lastMessage.
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "shelly"}, Entity: EntityInfo{UniqueID: "power", StateTopic: "shelly/netz_meanwell/power"}})
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "shelly"}, Entity: EntityInfo{UniqueID: "voltage", StateTopic: "shelly/netz_meanwell/power"}})
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "shelly"}, Entity: EntityInfo{UniqueID: "relay", StateTopic: "shelly/relay/state", AvailabilityTopic: "shelly/status"}})
	reg.UpdateState("shelly/netz_meanwell/power", []byte(`{"id":0,"apower":12.5}`), false, time.Now())

	samples := reg.TopicSamples()
	byTopic := make(map[string]TopicSample, len(samples))
	topics := make([]string, 0, len(samples))
	for _, sample := range samples {
		byTopic[sample.Topic] = sample
		topics = append(topics, sample.Topic)
	}
	if !equalStrings(topics, []string{"shelly/netz_meanwell/power", "shelly/relay/state", "shelly/status"}) {
		t.Fatalf("samples are not sorted by topic: %v", topics)
	}
	if got := byTopic["shelly/netz_meanwell/power"].Payload; got != `{"id":0,"apower":12.5}` {
		t.Fatalf("payload = %q", got)
	}
	if byTopic["shelly/netz_meanwell/power"].At == nil {
		t.Fatal("sample has no timestamp")
	}
	for _, topic := range []string{"shelly/relay/state", "shelly/status"} {
		if got := byTopic[topic].Payload; got != "" {
			t.Fatalf("%s should have no payload yet, got %q", topic, got)
		}
	}
}

// A device that heartbeats its availability topic every cycle (battery_soc
// publishes .../status/online after every /state and /tuning message) must not
// lose the state-topic sample: the availability message is the most recent one
// on the entity, but the sample for the state topic still has to carry the last
// state payload.
func TestTopicSamplesKeepStatePayloadAfterAvailabilityHeartbeat(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{
		Device: DeviceInfo{ID: "battery_soc", Name: "Batterie-Ladezustand"},
		Entity: EntityInfo{
			UniqueID:            "battery_soc_open_suggestions_pack",
			StateTopic:          "outstation/battery_soc/tuning",
			AvailabilityTopic:   "outstation/battery_soc/status/online",
			PayloadAvailable:    "1",
			PayloadNotAvailable: "0",
		},
	})
	reg.UpdateState("outstation/battery_soc/tuning", []byte(`{"units":{"pack":{"suggestions":[]}}}`), true, time.Now())
	// The availability heartbeat lands after the state message, on the same entity.
	reg.UpdateAvailability("outstation/battery_soc/status/online", []byte("1"), true, time.Now())

	samples := reg.TopicSamplesFor([]string{"outstation/battery_soc/tuning"})
	if len(samples) != 1 {
		t.Fatalf("expected one sample, got %d", len(samples))
	}
	if got := samples[0].Payload; got != `{"units":{"pack":{"suggestions":[]}}}` {
		t.Fatalf("state payload lost after availability heartbeat: %q", got)
	}
	if samples[0].At == nil {
		t.Fatal("sample has no timestamp")
	}
}

func TestTopicSamplesCoverExactlyTheKnownTopics(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "node"}, Entity: EntityInfo{UniqueID: "temp", StateTopic: "state/node/temp", AvailabilityTopic: "status/node"}})

	topics := make([]string, 0)
	for _, sample := range reg.TopicSamples() {
		topics = append(topics, sample.Topic)
	}
	if !equalStrings(topics, reg.Topics()) {
		t.Fatalf("TopicSamples() = %v, Topics() = %v", topics, reg.Topics())
	}
}

// TestCountsMatchesSnapshot bindet Counts() an Snapshot(): die
// Diagnose-Zusammenfassung darf nicht andere Zahlen nennen als die
// Vollansicht, sonst wandert bei jeder Registry-Aenderung eine stille
// Abweichung ein.
func TestCountsMatchesSnapshot(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{
		Device: DeviceInfo{ID: "node", Name: "Node"},
		Entity: EntityInfo{UniqueID: "node_temp", ObjectID: "temperature", StateTopic: "state/node/temp"},
	})
	reg.UpsertEntity(Discovery{
		Device: DeviceInfo{ID: "node", Name: "Node"},
		Entity: EntityInfo{UniqueID: "node_hum", ObjectID: "humidity", StateTopic: "state/node/hum"},
	})
	reg.UpsertEntity(Discovery{
		Device: DeviceInfo{ID: "other", Name: "Other"},
		Entity: EntityInfo{UniqueID: "other_temp", ObjectID: "temperature", StateTopic: "state/other/temp"},
	})

	devices, entities := reg.Counts()
	snapshot := reg.Snapshot()
	wantEntities := 0
	for _, device := range snapshot {
		wantEntities += len(device.Entities)
	}
	if devices != len(snapshot) || entities != wantEntities {
		t.Fatalf("Counts() = (%d, %d), snapshot has (%d, %d)", devices, entities, len(snapshot), wantEntities)
	}
	if devices != 2 || entities != 3 {
		t.Fatalf("Counts() = (%d, %d), want (2, 3)", devices, entities)
	}
}

func TestCountsIsEmptyOnAFreshRegistry(t *testing.T) {
	devices, entities := New().Counts()
	if devices != 0 || entities != 0 {
		t.Fatalf("Counts() on an empty registry = (%d, %d), want (0, 0)", devices, entities)
	}
}

func TestTopicSamplesReportTheOwningDevice(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{
		Device: DeviceInfo{ID: "netz_meanwell", Name: "MeanWell-Ladegeraet"},
		Entity: EntityInfo{UniqueID: "netz_meanwell_power", StateTopic: "shelly/netz_meanwell/status"},
	})
	// Zweites Geraet, das nie sendet - der Name muss trotzdem herauskommen.
	reg.UpsertEntity(Discovery{
		Device: DeviceInfo{ID: "bms_bank_b", Name: "Shelly Uni Bank B"},
		Entity: EntityInfo{UniqueID: "bank_b_voltage", StateTopic: "bms/bank_b/voltage"},
	})
	// Drittes Geraet ohne Namen: dann muss die ID einspringen, sonst landet das
	// Topic in der Gruppe "Sonstige", obwohl das Geraet bekannt ist.
	reg.UpsertEntity(Discovery{
		Device: DeviceInfo{ID: "namenlos"},
		Entity: EntityInfo{UniqueID: "namenlos_wert", StateTopic: "namenlos/wert"},
	})
	reg.UpdateState("shelly/netz_meanwell/status", []byte(`{"id":0,"apower":12.5}`), false, time.Now())

	byTopic := make(map[string]TopicSample)
	for _, sample := range reg.TopicSamples() {
		byTopic[sample.Topic] = sample
	}

	if got := byTopic["shelly/netz_meanwell/status"].Device; got != "MeanWell-Ladegeraet" {
		t.Fatalf("Geraetename am Topic mit Payload = %q", got)
	}
	if got := byTopic["shelly/netz_meanwell/status"].DeviceID; got != "netz_meanwell" {
		t.Fatalf("Geraete-ID am Topic mit Payload = %q", got)
	}
	if got := byTopic["bms/bank_b/voltage"].Device; got != "Shelly Uni Bank B" {
		t.Fatalf("Geraetename am Topic ohne Nachricht = %q", got)
	}
	if got := byTopic["bms/bank_b/voltage"].Payload; got != "" {
		t.Fatalf("Topic ohne Nachricht darf keinen Payload melden: %q", got)
	}
	if got := byTopic["namenlos/wert"].Device; got != "namenlos" {
		t.Fatalf("namenloses Geraet muss auf die ID zurueckfallen, war %q", got)
	}
}

// ValueViews traegt genau die sieben Felder, die sich zwischen zwei
// Layoutwechseln aendern koennen. Alles andere (Name, Einheit, Icon) steht
// schon im gerenderten HTML und gehoert nicht in jedes SSE-Ereignis.
func TestValueViewsCarriesOnlyTheChangingFields(t *testing.T) {
	devices := []DeviceView{{
		ID: "dev-a",
		Entities: []EntityView{
			{UniqueID: "e1", Value: "42", HasValue: true, Available: true, HasAvailability: true, Name: "Leistung", UnitOfMeasurement: "W"},
			{UniqueID: "e2", Stale: true, Pending: true, LastCommandResult: "timeout"},
		},
	}}

	values := ValueViews(devices)

	if len(values) != 2 {
		t.Fatalf("erwartet 2 Eintraege, bekommen %d", len(values))
	}
	if got := values["e1"]; got.Value != "42" || !got.HasValue || !got.Available || !got.HasAvailability {
		t.Errorf("e1 falsch uebernommen: %+v", got)
	}
	if got := values["e2"]; !got.Stale || !got.Pending || got.LastCommandResult != "timeout" {
		t.Errorf("e2 falsch uebernommen: %+v", got)
	}

	data, err := json.Marshal(values["e1"])
	if err != nil {
		t.Fatalf("Marshal fehlgeschlagen: %v", err)
	}
	for _, forbidden := range []string{"Leistung", "unit_of_measurement", "discovery_json", "last_message"} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("ValueView traegt %q mit - der Rumpf soll klein bleiben: %s", forbidden, data)
		}
	}
}

func TestValueViewsSkipsEntitiesWithoutID(t *testing.T) {
	values := ValueViews([]DeviceView{{ID: "dev-a", Entities: []EntityView{{UniqueID: ""}, {UniqueID: "e1"}}}})
	if _, ok := values[""]; ok {
		t.Error("Entitaet ohne UniqueID landete unter dem leeren Schluessel")
	}
	if len(values) != 1 {
		t.Errorf("erwartet 1 Eintrag, bekommen %d", len(values))
	}
}

// Der Fingerabdruck ist der Waechter, der entscheidet, ob der Browser sein
// Fragment tauschen muss. Sein Wert liegt genau darin, dass er bei einer
// reinen Wertaenderung stehenbleibt - sonst taeuschte er bei jedem MQTT-
// Messwert einen Strukturwechsel vor und der Tausch waere zurueck.
func TestStructureFingerprintIgnoresValues(t *testing.T) {
	before := []DeviceView{{ID: "dev-a", Entities: []EntityView{
		{UniqueID: "e1", Name: "Leistung", Component: "sensor", DeviceClass: "power", UnitOfMeasurement: "W", Value: "42", HasValue: true, Available: true},
	}}}
	after := []DeviceView{{ID: "dev-a", Entities: []EntityView{
		{UniqueID: "e1", Name: "Leistung", Component: "sensor", DeviceClass: "power", UnitOfMeasurement: "W", Value: "1337", HasValue: true, Available: false, Stale: true, Pending: true, LastCommandResult: "timeout"},
	}}}

	if StructureFingerprint(before) != StructureFingerprint(after) {
		t.Error("eine reine Wertaenderung hat den Fingerabdruck veraendert")
	}
}

// Jede dieser Aenderungen laesst die Templates anders rendern: eine neue
// Zeile, ein anderer Titel, ein anderes Icon, eine andere Einheit, ein
// Schaltknopf statt eines Werts. Bleibt der Fingerabdruck dabei stehen,
// erscheint die Aenderung im Browser nie.
func TestStructureFingerprintReactsToStructure(t *testing.T) {
	base := []DeviceView{{ID: "dev-a", Entities: []EntityView{
		{UniqueID: "e1", Name: "Leistung", Component: "sensor", DeviceClass: "power", UnitOfMeasurement: "W"},
	}}}
	reference := StructureFingerprint(base)

	cases := map[string][]DeviceView{
		"neue Entitaet": {{ID: "dev-a", Entities: []EntityView{
			{UniqueID: "e1", Name: "Leistung", Component: "sensor", DeviceClass: "power", UnitOfMeasurement: "W"},
			{UniqueID: "e2", Name: "Spannung", Component: "sensor"},
		}}},
		"neues Geraet": {
			{ID: "dev-a", Entities: []EntityView{{UniqueID: "e1", Name: "Leistung", Component: "sensor", DeviceClass: "power", UnitOfMeasurement: "W"}}},
			{ID: "dev-b"},
		},
		"umbenannt": {{ID: "dev-a", Entities: []EntityView{
			{UniqueID: "e1", Name: "Wirkleistung", Component: "sensor", DeviceClass: "power", UnitOfMeasurement: "W"},
		}}},
		"andere Komponente": {{ID: "dev-a", Entities: []EntityView{
			{UniqueID: "e1", Name: "Leistung", Component: "switch", DeviceClass: "power", UnitOfMeasurement: "W"},
		}}},
		"andere device_class": {{ID: "dev-a", Entities: []EntityView{
			{UniqueID: "e1", Name: "Leistung", Component: "sensor", DeviceClass: "energy", UnitOfMeasurement: "W"},
		}}},
		"andere Einheit": {{ID: "dev-a", Entities: []EntityView{
			{UniqueID: "e1", Name: "Leistung", Component: "sensor", DeviceClass: "power", UnitOfMeasurement: "kW"},
		}}},
		"jetzt schaltbar": {{ID: "dev-a", Entities: []EntityView{
			{UniqueID: "e1", Name: "Leistung", Component: "sensor", DeviceClass: "power", UnitOfMeasurement: "W", Commandable: true},
		}}},
	}
	for name, devices := range cases {
		t.Run(name, func(t *testing.T) {
			if StructureFingerprint(devices) == reference {
				t.Error("Strukturaenderung ohne Wechsel des Fingerabdrucks")
			}
		})
	}
}

// Ohne Trennzeichen zwischen den Feldern ergaeben "ab"+"" und "a"+"b"
// denselben Hash - zwei verschiedene Strukturen saehen gleich aus.
func TestStructureFingerprintSeparatesFields(t *testing.T) {
	joined := []DeviceView{{ID: "dev-a", Entities: []EntityView{{UniqueID: "e1", Name: "ab", Component: ""}}}}
	split := []DeviceView{{ID: "dev-a", Entities: []EntityView{{UniqueID: "e1", Name: "a", Component: "b"}}}}

	if StructureFingerprint(joined) == StructureFingerprint(split) {
		t.Error("Felder werden ohne Trennzeichen aneinandergehaengt")
	}
}

// Der leere Fall muss einen Wert liefern, keinen leeren String: der Client
// vergleicht ihn mit data-structure, und ein leerer Wert wuerde dort als
// "kein Fingerabdruck da" gelesen und ewig den Tausch erzwingen.
func TestStructureFingerprintOfEmptyIsStableAndNotBlank(t *testing.T) {
	empty := StructureFingerprint(nil)
	if empty == "" {
		t.Fatal("leerer Fingerabdruck fuer eine leere Registry")
	}
	if empty != StructureFingerprint([]DeviceView{}) {
		t.Error("nil und leere Liste liefern verschiedene Fingerabdruecke")
	}
}

func TestTopicSamplesForReturnsKnownFalseForUnknownTopic(t *testing.T) {
	reg := New()
	samples := reg.TopicSamplesFor([]string{"outstation/nichts/state"})
	if len(samples) != 1 || samples[0].Known {
		t.Fatalf("expected one unknown sample, got %+v", samples)
	}
}

func TestTopicSamplesForPreservesRequestOrder(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "node"}, Entity: EntityInfo{UniqueID: "b", StateTopic: "outstation/b/state"}})
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "node"}, Entity: EntityInfo{UniqueID: "a", StateTopic: "outstation/a/state"}})
	topics := []string{"outstation/b/state", "outstation/a/state"}
	samples := reg.TopicSamplesFor(topics)
	if len(samples) != 2 || samples[0].Topic != topics[0] || samples[1].Topic != topics[1] {
		t.Fatalf("expected request order preserved, got %+v", samples)
	}
}

func TestRestoreStateAtSeedsLastMessageFromCachedPayload(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "dev"}, Entity: EntityInfo{UniqueID: "uid", StateTopic: "outstation/x/state"}})
	lastSeen := time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC)
	lastTopicAt := time.Date(2026, 8, 1, 1, 1, 0, 0, time.UTC)
	reg.RestoreStateAt("dev", "uid", "42", true, false, lastSeen, lastTopicAt, `{"raw":true}`)
	samples := reg.TopicSamplesFor([]string{"outstation/x/state"})
	if samples[0].Payload != `{"raw":true}` || samples[0].Source != "runtime-cache" {
		t.Fatalf("expected cached raw payload with runtime-cache source, got %+v", samples[0])
	}
}

func TestRestoreStateAtDoesNotOverwriteALiveMessage(t *testing.T) {
	reg := New()
	reg.UpsertEntity(Discovery{Device: DeviceInfo{ID: "dev"}, Entity: EntityInfo{UniqueID: "uid", StateTopic: "outstation/x/state"}})
	lastSeen := time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC)
	lastTopicAt := time.Date(2026, 8, 1, 1, 1, 0, 0, time.UTC)
	reg.UpdateTopicWithQoS("outstation/x/state", []byte(`{"live":true}`), false, 0, lastTopicAt)
	reg.RestoreStateAt("dev", "uid", "42", true, false, lastSeen, lastTopicAt, `{"stale":true}`)
	samples := reg.TopicSamplesFor([]string{"outstation/x/state"})
	if samples[0].Payload != `{"live":true}` || samples[0].Source != "live" {
		t.Fatalf("restore must not clobber an already-live message, got %+v", samples[0])
	}
}
