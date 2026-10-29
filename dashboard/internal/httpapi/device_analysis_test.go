package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

const deviceAnalysisDiscovery = `{"unique_id":"shelly_relay","name":"Relay","state_topic":"state/shelly/relay","availability_topic":"state/shelly/status","command_topic":"shelly/relay/0/set","device":{"name":"Shelly","manufacturer":"Shelly","model":"Plug"}}`

// deviceAnalysisMismatchDiscovery nennt eine andere unique_id als die Entitaet,
// damit die DiscoveryMismatch-Regel anschlaegt.
const deviceAnalysisMismatchDiscovery = `{"unique_id":"other","name":"Relay","state_topic":"state/shelly/relay","availability_topic":"state/shelly/status","command_topic":"shelly/relay/0/set","device":{"name":"Shelly","manufacturer":"Shelly","model":"Plug"}}`

func deviceAnalysisRegistry(t *testing.T, discovery string) *registry.Registry {
	t.Helper()
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device:            registry.DeviceInfo{ID: "shelly", Name: "Shelly", Manufacturer: "Shelly", Model: "Plug"},
		Entity:            registry.EntityInfo{UniqueID: "shelly_relay", Component: "switch", ObjectID: "relay", Name: "Relay", StateTopic: "state/shelly/relay", AvailabilityTopic: "state/shelly/status", CommandTopic: "shelly/relay/0/set", PayloadOn: "ON", PayloadOff: "OFF"},
		RawJSON:           discovery,
		DiscoveryTopic:    "homeassistant/switch/shelly/relay/config",
		DiscoveryRetained: true,
	})
	// Ablauf: online mit Zustand (setzt lastSeen), dann offline. Die Nachricht
	// nach dem Offline-Wechsel aktualisiert last_message, nicht aber lastSeen,
	// daher bleibt nur die Offline-Warnung und keine NoStateUpdate-Warnung.
	// Verfuegbarkeit und Zustand teilen sich den last_message-Slot, deshalb
	// steht der Zustand zuletzt.
	now := time.Now().UTC()
	reg.UpdateAvailability("state/shelly/status", []byte("online"), false, now)
	reg.UpdateTopicWithQoS("state/shelly/relay", []byte("42"), false, 1, now)
	reg.UpdateAvailability("state/shelly/status", []byte("offline"), false, now)
	reg.UpdateTopicWithQoS("state/shelly/relay", []byte("42"), false, 1, now)
	return reg
}

func deviceAnalysisGET(t *testing.T, router http.Handler, path string, user *auth.User, ifNoneMatch string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if user != nil {
		request = request.WithContext(auth.WithUser(context.Background(), *user))
	}
	if ifNoneMatch != "" {
		request.Header.Set("If-None-Match", ifNoneMatch)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeDeviceAnalysisBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func warningRuleIDs(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, ok := body["warnings"].([]any)
	if !ok {
		t.Fatalf("warnings missing or not a list: %#v", body["warnings"])
	}
	var ids []string
	for _, item := range raw {
		ids = append(ids, item.(map[string]any)["rule_id"].(string))
	}
	return ids
}

func analysisEntity(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	entities, ok := body["entities"].([]any)
	if !ok || len(entities) == 0 {
		t.Fatalf("entities missing: %#v", body["entities"])
	}
	return entities[0].(map[string]any)
}

func deviceAnalysisRouter(t *testing.T, reg *registry.Registry) http.Handler {
	t.Helper()
	publisher := &recordingCommandPublisher{}
	return NewRouterWithTinyTuyaAndPublisher(reg, config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, publisher)
}

// deviceAnalysisFixture liefert einen Router mit Gerät, Nachricht, Schreibaktion
// und zwei Warnungen (Offline ist betrieblich, DiscoveryMismatch nicht).
func deviceAnalysisFixture(t *testing.T) http.Handler {
	t.Helper()
	reg := deviceAnalysisRegistry(t, deviceAnalysisMismatchDiscovery)
	router := deviceAnalysisRouter(t, reg)
	command := httptest.NewRecorder()
	router.ServeHTTP(command, httptest.NewRequest(http.MethodPost, "/api/v1/entities/shelly_relay/command", nil))
	if command.Code != http.StatusOK {
		t.Fatalf("command status %d: %s", command.Code, command.Body.String())
	}
	return router
}

var viewOnlyUser = &auth.User{Username: "viewer", Roles: []string{auth.RoleViewDevices}}
var analysisUser = &auth.User{Username: "analyst", Roles: []string{auth.RoleViewDevices, auth.RoleViewDeviceAnalysis}}

func TestDeviceDetailWithoutAnalysisRight(t *testing.T) {
	router := deviceAnalysisFixture(t)
	body := decodeDeviceAnalysisBody(t, deviceAnalysisGET(t, router, "/api/v1/devices/shelly", viewOnlyUser, ""))

	if _, ok := body["discovery_json"]; ok {
		t.Fatalf("device discovery_json leaked: %#v", body["discovery_json"])
	}
	entity := analysisEntity(t, body)
	if _, ok := entity["discovery_json"]; ok {
		t.Fatalf("entity discovery_json leaked: %#v", entity["discovery_json"])
	}
	lastMessage, ok := entity["last_message"].(map[string]any)
	if !ok {
		t.Fatalf("last_message missing: %#v", entity["last_message"])
	}
	if lastMessage["at"] == nil || lastMessage["payload"] != "" || lastMessage["topic"] != "" {
		t.Fatalf("last_message not reduced to at: %#v", lastMessage)
	}
	if actions, ok := body["command_actions"].([]any); !ok || len(actions) != 0 {
		t.Fatalf("command_actions = %#v, want empty list", body["command_actions"])
	}
	if ids := warningRuleIDs(t, body); len(ids) != 1 || ids[0] != "Offline" {
		t.Fatalf("warnings = %v, want only Offline", ids)
	}
	if entity["state_topic"] != "state/shelly/relay" {
		t.Fatalf("state_topic = %#v, want it kept", entity["state_topic"])
	}
}

func TestDeviceDetailWithAnalysisRight(t *testing.T) {
	router := deviceAnalysisFixture(t)
	body := decodeDeviceAnalysisBody(t, deviceAnalysisGET(t, router, "/api/v1/devices/shelly", analysisUser, ""))

	if _, ok := body["discovery_json"]; !ok {
		t.Fatal("device discovery_json missing with analysis right")
	}
	entity := analysisEntity(t, body)
	if entity["discovery_json"] == nil || entity["discovery_json"] == "" {
		t.Fatalf("entity discovery_json missing: %#v", entity["discovery_json"])
	}
	lastMessage, ok := entity["last_message"].(map[string]any)
	if !ok || lastMessage["payload"] != "42" || lastMessage["topic"] != "state/shelly/relay" {
		t.Fatalf("last_message = %#v", entity["last_message"])
	}
	if actions, ok := body["command_actions"].([]any); !ok || len(actions) != 1 {
		t.Fatalf("command_actions = %#v, want one entry", body["command_actions"])
	}
	ids := warningRuleIDs(t, body)
	if !hasRuleID(ids, "Offline") || !hasRuleID(ids, "DiscoveryMismatch") {
		t.Fatalf("warnings = %v, want Offline and DiscoveryMismatch", ids)
	}
}

func TestDeviceDetailWithoutUserIsUnfiltered(t *testing.T) {
	router := deviceAnalysisFixture(t)
	body := decodeDeviceAnalysisBody(t, deviceAnalysisGET(t, router, "/api/v1/devices/shelly", nil, ""))

	if _, ok := body["discovery_json"]; !ok {
		t.Fatal("device discovery_json missing without user")
	}
	if actions, ok := body["command_actions"].([]any); !ok || len(actions) != 1 {
		t.Fatalf("command_actions = %#v, want one entry", body["command_actions"])
	}
	if ids := warningRuleIDs(t, body); !hasRuleID(ids, "DiscoveryMismatch") {
		t.Fatalf("warnings = %v, want DiscoveryMismatch", ids)
	}
}

func TestDeviceListWithoutAnalysisRight(t *testing.T) {
	router := deviceAnalysisRouter(t, deviceAnalysisRegistry(t, deviceAnalysisMismatchDiscovery))

	restricted := deviceAnalysisGET(t, router, "/api/v1/devices", viewOnlyUser, "")
	full := deviceAnalysisGET(t, router, "/api/v1/devices", analysisUser, "")

	restrictedBody := restricted.Body.String()
	if restricted.Code != http.StatusOK || strings.Contains(restrictedBody, `"discovery_json"`) {
		t.Fatalf("restricted list status %d leaked discovery_json: %s", restricted.Code, restrictedBody)
	}
	if strings.Contains(restrictedBody, `"payload":"42"`) || strings.Contains(restrictedBody, `"topic":"state/shelly/relay"`) {
		t.Fatalf("restricted list last_message not reduced: %s", restrictedBody)
	}
	if full.Code != http.StatusOK || !strings.Contains(full.Body.String(), `"discovery_json"`) || !strings.Contains(full.Body.String(), `"payload":"42"`) {
		t.Fatalf("full list incomplete: %s", full.Body.String())
	}

	restrictedETag := restricted.Header().Get("ETag")
	fullETag := full.Header().Get("ETag")
	if restrictedETag == "" || fullETag == "" || restrictedETag == fullETag {
		t.Fatalf("ETags must differ, got restricted %q and full %q", restrictedETag, fullETag)
	}

	revalidated := deviceAnalysisGET(t, router, "/api/v1/devices", viewOnlyUser, fullETag)
	if revalidated.Code != http.StatusOK {
		t.Fatalf("restricted user got status %d for the full ETag, want 200", revalidated.Code)
	}
	if got := deviceAnalysisGET(t, router, "/api/v1/devices", viewOnlyUser, restrictedETag); got.Code != http.StatusNotModified {
		t.Fatalf("restricted user with own ETag got status %d, want 304", got.Code)
	}
	if got := deviceAnalysisGET(t, router, "/api/v1/devices", analysisUser, fullETag); got.Code != http.StatusNotModified {
		t.Fatalf("full user with own ETag got status %d, want 304", got.Code)
	}
}

func TestStripDeviceAnalysisKeepsSnapshot(t *testing.T) {
	reg := deviceAnalysisRegistry(t, deviceAnalysisDiscovery)
	snapshot := reg.Snapshot()
	if len(snapshot) != 1 || len(snapshot[0].Entities) != 1 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	original := snapshot[0]
	originalEntity := original.Entities[0]

	stripped := stripDeviceAnalysis(original)
	if stripped.DiscoveryJSON != nil || stripped.Entities[0].DiscoveryJSON != "" {
		t.Fatalf("stripped copy still has discovery: %#v", stripped)
	}
	if stripped.Entities[0].LastMessage == nil || stripped.Entities[0].LastMessage.Payload != "" {
		t.Fatalf("stripped last_message = %#v", stripped.Entities[0].LastMessage)
	}

	if len(original.DiscoveryJSON) == 0 || original.DiscoveryJSON[0] == "" {
		t.Fatal("original device discovery was modified")
	}
	if original.Entities[0].DiscoveryJSON != originalEntity.DiscoveryJSON || original.Entities[0].DiscoveryJSON == "" {
		t.Fatal("original entity discovery was modified")
	}
	if original.Entities[0].LastMessage == nil || original.Entities[0].LastMessage.Payload != "42" {
		t.Fatalf("original last_message was modified: %#v", original.Entities[0].LastMessage)
	}
	if reread := reg.Snapshot(); reread[0].Entities[0].LastMessage.Payload != "42" || reread[0].Entities[0].DiscoveryJSON == "" {
		t.Fatal("registry snapshot changed after stripping")
	}
}

func hasRuleID(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
