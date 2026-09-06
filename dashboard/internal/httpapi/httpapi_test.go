package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/mqttclient"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/runtimecache"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/shellypresets"
	"github.com/Developer-Simon/energy-node-dashboard/internal/storagehealth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/tinytuya"
)

type fakeTinyTuyaProbe struct{}

type recordingTinyTuyaProbe struct {
	request tinytuya.CloudRequest
}

type failingTinyTuyaProbe struct{}

func (failingTinyTuyaProbe) Devices(context.Context, tinytuya.CloudRequest) ([]tinytuya.Device, error) {
	return nil, errors.New("Tuya Cloud: Code 28841002: IoT Core service subscription has expired")
}

func (failingTinyTuyaProbe) Status(context.Context, tinytuya.StatusRequest) (tinytuya.StatusResult, error) {
	return tinytuya.StatusResult{}, errors.New("status failed")
}

type recordingCommandPublisher struct {
	topic   string
	payload string
	err     error
}

type fakeStorageHealthProvider struct {
	report storagehealth.Report
	err    error
}

type fakeMQTTStatusProvider struct {
	status mqttclient.Status
}

type fakeDeviceReloader struct {
	err   error
	calls int
}

func (provider fakeStorageHealthProvider) Check(context.Context) (storagehealth.Report, error) {
	return provider.report, provider.err
}

func (provider fakeMQTTStatusProvider) Status() mqttclient.Status {
	return provider.status
}

func (reloader *fakeDeviceReloader) Reload() error {
	reloader.calls++
	return reloader.err
}

func (publisher *recordingCommandPublisher) Publish(topic, payload string) error {
	publisher.topic = topic
	publisher.payload = payload
	return publisher.err
}

func (probe *recordingTinyTuyaProbe) Devices(_ context.Context, request tinytuya.CloudRequest) ([]tinytuya.Device, error) {
	probe.request = request
	return []tinytuya.Device{{DeviceID: "device-1"}}, nil
}

func (probe *recordingTinyTuyaProbe) Status(context.Context, tinytuya.StatusRequest) (tinytuya.StatusResult, error) {
	return tinytuya.StatusResult{}, nil
}

func (fakeTinyTuyaProbe) Devices(context.Context, tinytuya.CloudRequest) ([]tinytuya.Device, error) {
	return []tinytuya.Device{{DeviceID: "device-1", Name: "Testgerät", LocalKey: "local-key", IP: "192.0.2.10", Version: 3.3}}, nil
}

func (fakeTinyTuyaProbe) Status(context.Context, tinytuya.StatusRequest) (tinytuya.StatusResult, error) {
	return tinytuya.StatusResult{
		DPS: []tinytuya.DPS{{ID: "1", Type: "bool", Value: true}},
		Raw: map[string]any{"dps": map[string]any{"1": true}},
	}, nil
}

func TestTopicsEndpointReturnsKnownTopics(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "battery"},
		Entity: registry.EntityInfo{UniqueID: "battery-soc", StateTopic: "outstation/battery/state", AvailabilityTopic: "outstation/battery/status"},
	})
	reg.UpdateState("outstation/battery/state", []byte(`{"soc":50}`), false, time.Now())

	router := NewRouter(reg, config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/topics", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	var topics []string
	if err := json.NewDecoder(recorder.Body).Decode(&topics); err != nil {
		t.Fatal(err)
	}
	if len(topics) != 2 || topics[0] != "outstation/battery/state" || topics[1] != "outstation/battery/status" {
		t.Fatalf("unexpected topics: %#v", topics)
	}
}

func TestDevicesEndpointUsesRegistryETag(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node"},
		Entity: registry.EntityInfo{UniqueID: "node-temp", StateTopic: "state/node/temp"},
	})
	router := NewRouter(reg, nil, nil)
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil))
	if first.Code != http.StatusOK || first.Header().Get("ETag") == "" {
		t.Fatalf("first devices response status=%d etag=%q", first.Code, first.Header().Get("ETag"))
	}
	secondRequest := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	secondRequest.Header.Set("If-None-Match", first.Header().Get("ETag"))
	second := httptest.NewRecorder()
	router.ServeHTTP(second, secondRequest)
	if second.Code != http.StatusNotModified {
		t.Fatalf("conditional devices response status=%d, want %d", second.Code, http.StatusNotModified)
	}
}

func TestDiagnosticsRulesAndHealthEndpoints(t *testing.T) {
	router := NewRouter(registry.New(), nil, nil)

	rules := httptest.NewRecorder()
	router.ServeHTTP(rules, httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/rules", nil))
	if rules.Code != http.StatusOK || !strings.Contains(rules.Body.String(), "DiscoveryMismatch") || !strings.Contains(rules.Body.String(), "DiscoveryRetained") {
		t.Fatalf("rules response %d: %s", rules.Code, rules.Body.String())
	}

	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/health", nil))
	if health.Code != http.StatusOK || health.Body.String() != "[]\n" {
		t.Fatalf("health response %d: %s", health.Code, health.Body.String())
	}

	post := httptest.NewRecorder()
	router.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/v1/diagnostics/rules", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST rules status %d, want 405", post.Code)
	}
}

func TestRuntimeCacheStatusIsExposedByHealthAndAPI(t *testing.T) {
	cache := runtimecache.NewStore(t.TempDir())
	if err := cache.Load(); err != nil {
		t.Fatal(err)
	}
	router := NewRouterWithTinyTuyaAndPublisherAndRuntimeCache(registry.New(), nil, nil, nil, nil, nil, cache)

	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status %d", health.Code)
	}
	var healthBody struct {
		Status       string              `json:"status"`
		RuntimeCache runtimecache.Status `json:"runtime_cache"`
	}
	if err := json.NewDecoder(health.Body).Decode(&healthBody); err != nil {
		t.Fatal(err)
	}
	if healthBody.Status != "ok" || !healthBody.RuntimeCache.Loaded {
		t.Fatalf("health body = %#v", healthBody)
	}

	status := httptest.NewRecorder()
	router.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/v1/runtime-cache", nil))
	if status.Code != http.StatusOK {
		t.Fatalf("runtime cache status %d", status.Code)
	}
	var statusBody runtimecache.Status
	if err := json.NewDecoder(status.Body).Decode(&statusBody); err != nil {
		t.Fatal(err)
	}
	if !statusBody.Loaded {
		t.Fatalf("runtime cache body = %#v", statusBody)
	}
}

func TestEntityCommandEndpointPublishesTogglePayload(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "shelly"},
		Entity: registry.EntityInfo{UniqueID: "shelly_relay", Component: "switch", StateTopic: "state/shelly/relay", CommandTopic: "shelly/relay/0/set", PayloadOn: "ON", PayloadOff: "OFF"},
	})
	publisher := &recordingCommandPublisher{}
	router := NewRouterWithTinyTuyaAndPublisher(reg, nil, nil, nil, nil, publisher)

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest("POST", "/api/v1/entities/shelly_relay/command", nil))
	if first.Code != 200 || publisher.topic != "shelly/relay/0/set" || publisher.payload != "ON" {
		t.Fatalf("first command status %d, topic %q, payload %q", first.Code, publisher.topic, publisher.payload)
	}

	reg.UpdateState("state/shelly/relay", []byte("ON"), false, time.Now())
	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest("POST", "/api/v1/entities/shelly_relay/command", nil))
	if second.Code != 200 || publisher.payload != "OFF" {
		t.Fatalf("second command status %d, payload %q", second.Code, publisher.payload)
	}
	reg.UpdateState("state/shelly/relay", []byte("OFF"), false, time.Now())
	explicit := httptest.NewRecorder()
	router.ServeHTTP(explicit, httptest.NewRequest(http.MethodPost, "/api/v1/entities/shelly_relay/command", strings.NewReader(`{"payload":"ON"}`)))
	if explicit.Code != http.StatusOK || publisher.payload != "ON" {
		t.Fatalf("explicit command status %d, payload %q", explicit.Code, publisher.payload)
	}
	detail := httptest.NewRecorder()
	router.ServeHTTP(detail, httptest.NewRequest("GET", "/api/v1/devices/shelly", nil))
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"command_actions"`) || strings.Count(detail.Body.String(), `"result":"published"`) != 3 {
		t.Fatalf("command history missing from detail response %d: %s", detail.Code, detail.Body.String())
	}
}

func TestEntityCommandEndpointReturnsPendingStatusAndRejectsConcurrentCommand(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "shelly"},
		Entity: registry.EntityInfo{UniqueID: "shelly_relay", Component: "switch", StateTopic: "state/shelly/relay", CommandTopic: "shelly/relay/0/set", PayloadOn: "ON", PayloadOff: "OFF"},
	})
	publisher := &recordingCommandPublisher{}
	router := NewRouterWithTinyTuyaAndPublisher(reg, nil, nil, nil, nil, publisher)

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/api/v1/entities/shelly_relay/command", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first command status %d", first.Code)
	}
	var firstBody map[string]string
	if err := json.NewDecoder(first.Body).Decode(&firstBody); err != nil {
		t.Fatal(err)
	}
	if firstBody["status"] != "pending" || firstBody["pending_deadline"] == "" {
		t.Fatalf("first command body = %#v, want status=pending with a pending_deadline", firstBody)
	}

	// The entity must not yet report the commanded value as confirmed.
	entity := reg.Snapshot()[0].Entities[0]
	if entity.HasValue || !entity.Pending {
		t.Fatalf("entity after publish = %#v, want no confirmed value yet and pending=true", entity)
	}

	// A second write while the first is still pending must be rejected
	// (P1.3: protection against re-triggering during a pending write).
	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/api/v1/entities/shelly_relay/command", nil))
	if second.Code != http.StatusConflict {
		t.Fatalf("second command status %d, want %d", second.Code, http.StatusConflict)
	}

	reg.UpdateState("state/shelly/relay", []byte("ON"), false, time.Now())
	entity = reg.Snapshot()[0].Entities[0]
	if entity.Pending || entity.Value != "ON" || entity.LastCommandResult != "success" {
		t.Fatalf("entity after confirmation = %#v", entity)
	}

	// Now that the first command resolved, a new one is accepted again.
	third := httptest.NewRecorder()
	router.ServeHTTP(third, httptest.NewRequest(http.MethodPost, "/api/v1/entities/shelly_relay/command", nil))
	if third.Code != http.StatusOK {
		t.Fatalf("third command status %d", third.Code)
	}
}

func TestEntityCommandTimeoutFallsBackWithoutClaimingSuccess(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "shelly"},
		Entity: registry.EntityInfo{UniqueID: "shelly_relay", Component: "switch", StateTopic: "state/shelly/relay", CommandTopic: "shelly/relay/0/set", PayloadOn: "ON", PayloadOff: "OFF"},
	})
	publisher := &recordingCommandPublisher{}
	router := NewRouterWithTinyTuyaAndPublisher(reg, nil, nil, nil, nil, publisher)

	sent := httptest.NewRecorder()
	router.ServeHTTP(sent, httptest.NewRequest(http.MethodPost, "/api/v1/entities/shelly_relay/command", nil))
	if sent.Code != http.StatusOK {
		t.Fatalf("command status %d", sent.Code)
	}

	// Simulate the periodic sweep (see cmd/dashboard/main.go) firing after
	// the pending deadline without any MQTT confirmation ever arriving.
	if changes := reg.ExpirePendingCommands(time.Now().Add(entityCommandPendingTimeout + time.Second)); len(changes) != 1 {
		t.Fatalf("expected one timed-out entity, got %d", len(changes))
	}

	entity := reg.Snapshot()[0].Entities[0]
	if entity.Pending || entity.HasValue || entity.LastCommandResult != "timeout" {
		t.Fatalf("entity after timeout = %#v, want pending=false, no confirmed value, last_command_result=timeout", entity)
	}

	// The entity is no longer blocked, so a fresh command is accepted.
	retry := httptest.NewRecorder()
	router.ServeHTTP(retry, httptest.NewRequest(http.MethodPost, "/api/v1/entities/shelly_relay/command", nil))
	if retry.Code != http.StatusOK {
		t.Fatalf("retry command status %d", retry.Code)
	}
}

func TestEntityCommandEndpointPublishesValidatedNumberPayload(t *testing.T) {
	minimum, maximum, step := 10.0, 30.0, 5.0
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node"},
		Entity: registry.EntityInfo{UniqueID: "node_interval", Component: "number", CommandTopic: "node/interval/set", MinValue: &minimum, MaxValue: &maximum, Step: &step},
	})
	publisher := &recordingCommandPublisher{}
	router := NewRouterWithTinyTuyaAndPublisher(reg, nil, nil, nil, nil, publisher)

	success := httptest.NewRecorder()
	router.ServeHTTP(success, httptest.NewRequest(http.MethodPost, "/api/v1/entities/node_interval/command", strings.NewReader(`{"value":25}`)))
	if success.Code != http.StatusOK || publisher.topic != "node/interval/set" || publisher.payload != "25" {
		t.Fatalf("number command status %d, topic %q, payload %q", success.Code, publisher.topic, publisher.payload)
	}

	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/api/v1/entities/node_interval/command", strings.NewReader(`{"value":27}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid number command status %d, want %d", invalid.Code, http.StatusBadRequest)
	}
}

func TestEntityCommandHistoryRecordsPublishErrorsAndIsBounded(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "shelly"},
		Entity: registry.EntityInfo{UniqueID: "shelly_relay", Component: "switch", StateTopic: "state/shelly/relay", CommandTopic: "shelly/relay/0/set", PayloadOn: "ON", PayloadOff: "OFF"},
	})

	failingPublisher := &recordingCommandPublisher{err: errors.New("broker unavailable")}
	router := NewRouterWithTinyTuyaAndPublisher(reg, nil, nil, nil, nil, failingPublisher)
	failure := httptest.NewRecorder()
	router.ServeHTTP(failure, httptest.NewRequest(http.MethodPost, "/api/v1/entities/shelly_relay/command", nil))
	if failure.Code != http.StatusBadGateway {
		t.Fatalf("publish failure status %d, want %d", failure.Code, http.StatusBadGateway)
	}
	detail := httptest.NewRecorder()
	router.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/api/v1/devices/shelly", nil))
	if !strings.Contains(detail.Body.String(), `"result":"error"`) || !strings.Contains(detail.Body.String(), "broker unavailable") {
		t.Fatalf("publish failure missing from history: %s", detail.Body.String())
	}

	successfulPublisher := &recordingCommandPublisher{}
	router = NewRouterWithTinyTuyaAndPublisher(reg, nil, nil, nil, nil, successfulPublisher)
	for index := 0; index < maxCommandActionsPerDevice+3; index++ {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/entities/shelly_relay/command", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("command %d status %d", index, recorder.Code)
		}
		// Confirm the command via a live MQTT state message so the next
		// iteration doesn't hit the pending-command conflict (409).
		reg.UpdateState("state/shelly/relay", []byte(successfulPublisher.payload), false, time.Now())
	}
	detail = httptest.NewRecorder()
	router.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/api/v1/devices/shelly", nil))
	var response deviceDetailResponse
	if err := json.NewDecoder(detail.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response.CommandActions) != maxCommandActionsPerDevice {
		t.Fatalf("got %d command actions, want %d", len(response.CommandActions), maxCommandActionsPerDevice)
	}
}

func TestEnergyEndpointAggregatesKnownPowerEntities(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "inverter"},
		Entity: registry.EntityInfo{UniqueID: "pv_total_power", ObjectID: "total_power", Name: "PV Gesamtleistung", StateTopic: "state/pv", UnitOfMeasurement: "W"},
	})
	reg.UpdateState("state/pv", []byte("420"), false, time.Now().UTC())

	recorder := httptest.NewRecorder()
	router := NewRouter(reg, nil, nil)
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/energy", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Values map[string]float64 `json:"values"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Values["pv"] != 420 {
		t.Fatalf("PV value = %v, want 420", response.Values["pv"])
	}
}

func TestStorageHealthEndpointReturnsReportAndChecksMethod(t *testing.T) {
	provider := fakeStorageHealthProvider{report: storagehealth.Report{
		Available: true,
		Device:    "/dev/mmcblk0",
		Medium:    "MMC/eMMC",
		Source:    "Linux-Sysfs",
	}}
	router := NewRouterWithStorageHealth(registry.New(), nil, nil, nil, nil, nil, provider)

	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, httptest.NewRequest("GET", "/api/v1/health/storage", nil))
	if getRecorder.Code != 200 {
		t.Fatalf("GET status %d: %s", getRecorder.Code, getRecorder.Body.String())
	}
	var report storagehealth.Report
	if err := json.NewDecoder(getRecorder.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if !report.Available || report.Device != "/dev/mmcblk0" {
		t.Fatalf("unexpected storage health report: %#v", report)
	}

	postRecorder := httptest.NewRecorder()
	router.ServeHTTP(postRecorder, httptest.NewRequest("POST", "/api/v1/health/storage", nil))
	if postRecorder.Code != 405 {
		t.Fatalf("POST status %d, want 405", postRecorder.Code)
	}

	healthRecorder := httptest.NewRecorder()
	router.ServeHTTP(healthRecorder, httptest.NewRequest("GET", "/api/v1/health", nil))
	if healthRecorder.Code != 200 || !strings.Contains(healthRecorder.Body.String(), `"storage"`) || !strings.Contains(healthRecorder.Body.String(), `"medium":"MMC/eMMC"`) {
		t.Fatalf("health summary does not contain storage status: %d %s", healthRecorder.Code, healthRecorder.Body.String())
	}
}

func TestStorageHealthEndpointReportsUnsupportedMedium(t *testing.T) {
	provider := fakeStorageHealthProvider{report: storagehealth.Report{
		Reason: "Das Medium stellt keine verwertbaren Gesundheitsdaten bereit.",
	}}
	router := NewRouterWithStorageHealth(registry.New(), nil, nil, nil, nil, nil, provider)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/health/storage", nil))
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `"available":false`) || !strings.Contains(recorder.Body.String(), "keine verwertbaren Gesundheitsdaten") {
		t.Fatalf("unexpected unsupported response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestDiscoveryEndpointReturnsSortedRegistryAndErrors(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device:          registry.DeviceInfo{ID: "node"},
		Entity:          registry.EntityInfo{UniqueID: "node_temp", StateTopic: "state/node/temp"},
		DiscoveryTopic:  "homeassistant/sensor/node/temp/config",
		DiscoverySource: "mqtt",
		DiscoveryQoS:    1,
	})
	reg.RecordDiscoveryError("homeassistant/sensor/node/bad/config", "{", "invalid character", true, 1, "mqtt", time.Now().UTC())

	recorder := httptest.NewRecorder()
	NewRouter(reg, nil, nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/discovery", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "node_temp") || !strings.Contains(recorder.Body.String(), "discovery_errors") {
		t.Fatalf("discovery response %d: %s", recorder.Code, recorder.Body.String())
	}

	post := httptest.NewRecorder()
	NewRouter(reg, nil, nil).ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/v1/discovery", nil))
	if post.Code != http.StatusMethodNotAllowed || !strings.Contains(post.Body.String(), `"code":"method_not_allowed"`) {
		t.Fatalf("POST discovery response %d: %s", post.Code, post.Body.String())
	}
}

func TestDeviceDetailReloadAndHealthUseRuntimeDependencies(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_temp", ObjectID: "temperature", StateTopic: "state/node/temp"},
	})
	startedAt := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	now := startedAt.Add(90 * time.Second)
	reloader := &fakeDeviceReloader{}
	router := NewRouterWithDependencies(reg, nil, nil, nil, nil, nil, nil, nil, RouterDependencies{
		MQTT:            fakeMQTTStatusProvider{status: mqttclient.Status{Connected: true}},
		Reloader:        reloader,
		StartedAt:       startedAt,
		Now:             func() time.Time { return now },
		Version:         "v0.1.7-dev-new-charts.42",
		ServicesVersion: "v0.1.12",
	})

	detail := httptest.NewRecorder()
	router.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/api/v1/devices/node", nil))
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"id":"node"`) || !strings.Contains(detail.Body.String(), `"warnings"`) {
		t.Fatalf("device detail response %d: %s", detail.Code, detail.Body.String())
	}

	reload := httptest.NewRecorder()
	router.ServeHTTP(reload, httptest.NewRequest(http.MethodPost, "/api/v1/devices/node/reload", nil))
	if reload.Code != http.StatusOK || reloader.calls != 1 || !strings.Contains(reload.Body.String(), `"scope":"all_devices"`) {
		t.Fatalf("reload response %d, calls=%d: %s", reload.Code, reloader.calls, reload.Body.String())
	}

	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/v1/devices/missing", nil))
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), `"code":"device_not_found"`) {
		t.Fatalf("missing device response %d: %s", missing.Code, missing.Body.String())
	}

	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), `"uptime_seconds":90`) || !strings.Contains(health.Body.String(), `"connected":true`) || !strings.Contains(health.Body.String(), `"version":"v0.1.7-dev-new-charts.42"`) || !strings.Contains(health.Body.String(), `"services_version":"v0.1.12"`) {
		t.Fatalf("health response %d: %s", health.Code, health.Body.String())
	}
}

func TestHealthVersionDefaultsToDev(t *testing.T) {
	reg := registry.New()
	router := NewRouterWithDependencies(reg, nil, nil, nil, nil, nil, nil, nil, RouterDependencies{})

	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), `"version":"dev"`) {
		t.Fatalf("health response %d: %s", health.Code, health.Body.String())
	}
}

func TestDeviceReloadReportsReloadFailure(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "node"}, Entity: registry.EntityInfo{UniqueID: "temp"}})
	reloader := &fakeDeviceReloader{err: errors.New("broker unavailable")}
	router := NewRouterWithDependencies(reg, nil, nil, nil, nil, nil, nil, nil, RouterDependencies{Reloader: reloader})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/devices/node/reload", nil))
	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), `"code":"reload_failed"`) {
		t.Fatalf("reload failure response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestEnergyRolesEndpointUpdatesAggregation(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "meter"},
		Entity: registry.EntityInfo{UniqueID: "custom_power", Name: "Leistung", StateTopic: "state/custom", UnitOfMeasurement: "W"},
	})
	reg.UpdateState("state/custom", []byte("125"), false, time.Now().UTC())
	store := settings.NewStore(t.TempDir())
	router := NewRouter(reg, nil, store)

	request := httptest.NewRequest("PUT", "/api/v1/energy/roles", strings.NewReader(`{"assignments":{"custom_power":{"role":"load","invert":true}}}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("roles status %d: %s", recorder.Code, recorder.Body.String())
	}

	energyRecorder := httptest.NewRecorder()
	router.ServeHTTP(energyRecorder, httptest.NewRequest("GET", "/api/v1/energy", nil))
	var response struct {
		Values map[string]float64 `json:"values"`
	}
	if err := json.NewDecoder(energyRecorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Values["load"] != -125 {
		t.Fatalf("load value = %v, want -125", response.Values["load"])
	}
}

func TestEnergyEndpointIncludesBalance(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "inverter"},
		Entity: registry.EntityInfo{UniqueID: "pv_total_power", ObjectID: "total_power", Name: "PV", StateTopic: "state/pv", UnitOfMeasurement: "W"},
	})
	reg.UpdateState("state/pv", []byte("620"), false, time.Now().UTC())

	recorder := httptest.NewRecorder()
	NewRouter(reg, nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/energy", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Balance struct {
			LoadTotal float64 `json:"load_total"`
		} `json:"balance"`
		Interpretation struct {
			GapMode string `json:"gap_mode"`
		} `json:"interpretation"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Interpretation.GapMode != "unknown_consumer" {
		t.Fatalf("gap_mode = %q, want unknown_consumer default", response.Interpretation.GapMode)
	}
	// No "load" role assigned, so auto mode falls back to the calculated
	// load - with nothing else in the snapshot that equals the pv value.
	if response.Balance.LoadTotal != 620 {
		t.Fatalf("balance.load_total = %v, want 620", response.Balance.LoadTotal)
	}
}

func TestEnergyRolesPutWithoutInterpretationKeepsTheStoredOne(t *testing.T) {
	reg := registry.New()
	store := settings.NewStore(t.TempDir())
	router := NewRouter(reg, nil, store)

	interpretationRequest := httptest.NewRequest("PUT", "/api/v1/energy/interpretation", strings.NewReader(`{"gap_mode":"diagnostic","load_mode":"measured","gap_tolerance_mode":"absolute","gap_tolerance_w":10,"gap_tolerance_percent":2}`))
	interpretationRequest.Header.Set("Content-Type", "application/json")
	interpretationRecorder := httptest.NewRecorder()
	router.ServeHTTP(interpretationRecorder, interpretationRequest)
	if interpretationRecorder.Code != 200 {
		t.Fatalf("interpretation put status %d: %s", interpretationRecorder.Code, interpretationRecorder.Body.String())
	}

	rolesRequest := httptest.NewRequest("PUT", "/api/v1/energy/roles", strings.NewReader(`{"assignments":{"custom_power":{"role":"load"}}}`))
	rolesRequest.Header.Set("Content-Type", "application/json")
	rolesRecorder := httptest.NewRecorder()
	router.ServeHTTP(rolesRecorder, rolesRequest)
	if rolesRecorder.Code != 200 {
		t.Fatalf("roles put status %d: %s", rolesRecorder.Code, rolesRecorder.Body.String())
	}
	var rolesResponse settings.EnergyConfig
	if err := json.NewDecoder(rolesRecorder.Body).Decode(&rolesResponse); err != nil {
		t.Fatal(err)
	}
	if rolesResponse.Interpretation.GapMode != energy.GapModeDiagnostic || rolesResponse.Interpretation.LoadMode != energy.LoadModeMeasured {
		t.Fatalf("roles PUT reset the stored interpretation: %#v", rolesResponse.Interpretation)
	}

	stored, err := store.LoadEnergy()
	if err != nil {
		t.Fatal(err)
	}
	if stored.Interpretation.GapMode != energy.GapModeDiagnostic {
		t.Fatalf("stored interpretation = %#v, want diagnostic to survive the roles PUT", stored.Interpretation)
	}
}

func TestEnergyInterpretationEndpointRoundTrips(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	router := NewRouter(registry.New(), nil, store)

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest("GET", "/api/v1/energy/interpretation", nil))
	if get.Code != 200 || !strings.Contains(get.Body.String(), `"unknown_consumer"`) {
		t.Fatalf("GET interpretation = %d: %s", get.Code, get.Body.String())
	}

	put := httptest.NewRequest("PUT", "/api/v1/energy/interpretation", strings.NewReader(`{"gap_mode":"diagnostic","load_mode":"calculated","gap_tolerance_mode":"percent","gap_tolerance_w":25,"gap_tolerance_percent":3}`))
	put.Header.Set("Content-Type", "application/json")
	putRecorder := httptest.NewRecorder()
	router.ServeHTTP(putRecorder, put)
	if putRecorder.Code != 200 {
		t.Fatalf("PUT interpretation = %d: %s", putRecorder.Code, putRecorder.Body.String())
	}

	getAfter := httptest.NewRecorder()
	router.ServeHTTP(getAfter, httptest.NewRequest("GET", "/api/v1/energy/interpretation", nil))
	var value energy.Interpretation
	if err := json.NewDecoder(getAfter.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	if value.GapMode != energy.GapModeDiagnostic || value.LoadMode != energy.LoadModeCalculated || value.GapTolerancePercent != 3 {
		t.Fatalf("interpretation after PUT = %#v", value)
	}

	invalid := httptest.NewRequest("PUT", "/api/v1/energy/interpretation", strings.NewReader(`{"gap_mode":"bogus"}`))
	invalid.Header.Set("Content-Type", "application/json")
	invalidRecorder := httptest.NewRecorder()
	router.ServeHTTP(invalidRecorder, invalid)
	if invalidRecorder.Code != 400 {
		t.Fatalf("PUT with unknown gap_mode = %d, want 400: %s", invalidRecorder.Code, invalidRecorder.Body.String())
	}
}

func TestStaticHistoryRecorderAssetIsServed(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewRouter(registry.New(), nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/static/js/history-recorder.js", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, marker := range []string{"HistoryStore.writeRaw", "dashboardHistorizer", "DOMContentLoaded", "setInterval(collect, config.intervalSeconds * 1000)", "dashboard-history-updated"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("history-recorder asset does not contain %q", marker)
		}
	}
}

func TestStaticHistoryAssetIsServed(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewRouter(registry.New(), nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/static/js/history.js", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, marker := range []string{"historyPanel", "dashboard-history-updated", "window.dashboardHistorizer.config"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("history asset does not contain %q", marker)
		}
	}
}

func TestTinyTuyaEndpointsUseProbeAndDoNotEchoCloudSecret(t *testing.T) {
	router := NewRouterWithTinyTuya(registry.New(), nil, nil, fakeTinyTuyaProbe{}, nil)
	devices := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/v1/tiny-tuya/devices", strings.NewReader(`{"region":"eu","access_id":"id","access_secret":"secret-value"}`))
	router.ServeHTTP(devices, request)
	if devices.Code != 200 || strings.Contains(devices.Body.String(), "secret-value") {
		t.Fatalf("devices status %d or secret leaked: %s", devices.Code, devices.Body.String())
	}
	status := httptest.NewRecorder()
	router.ServeHTTP(status, httptest.NewRequest("POST", "/api/v1/tiny-tuya/status", strings.NewReader(`{"device_id":"device-1","local_key":"local-key","ip":"192.0.2.10","version":3.3}`)))
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"id":"1"`) {
		t.Fatalf("status response %d: %s", status.Code, status.Body.String())
	}
}

func TestTinyTuyaCloudErrorIncludesToolOutput(t *testing.T) {
	router := NewRouterWithTinyTuya(registry.New(), nil, nil, failingTinyTuyaProbe{}, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/api/v1/tiny-tuya/devices", strings.NewReader(`{"region":"eu","access_id":"id","access_secret":"secret"}`)))
	if recorder.Code != 502 || !strings.Contains(recorder.Body.String(), `"tool_output":"Tuya Cloud: Code 28841002`) {
		t.Fatalf("diagnostic response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestTinyTuyaCredentialsAreSavedRedactedAndReused(t *testing.T) {
	probe := &recordingTinyTuyaProbe{}
	store := tinytuya.NewCredentialStore(t.TempDir())
	router := NewRouterWithTinyTuya(registry.New(), nil, nil, probe, store)

	save := httptest.NewRecorder()
	router.ServeHTTP(save, httptest.NewRequest("POST", "/api/v1/tiny-tuya/credentials", strings.NewReader(`{"region":"eu","access_id":"access-id","access_secret":"secret-value"}`)))
	if save.Code != 200 || strings.Contains(save.Body.String(), "secret-value") || strings.Contains(save.Body.String(), "access-id") {
		t.Fatalf("save response %d leaked credentials: %s", save.Code, save.Body.String())
	}

	metadata := httptest.NewRecorder()
	router.ServeHTTP(metadata, httptest.NewRequest("GET", "/api/v1/tiny-tuya/credentials", nil))
	if metadata.Code != 200 || !strings.Contains(metadata.Body.String(), `"configured":true`) || strings.Contains(metadata.Body.String(), "secret-value") || strings.Contains(metadata.Body.String(), "access-id") {
		t.Fatalf("metadata response %d leaked credentials: %s", metadata.Code, metadata.Body.String())
	}

	devices := httptest.NewRecorder()
	router.ServeHTTP(devices, httptest.NewRequest("POST", "/api/v1/tiny-tuya/devices", strings.NewReader(`{}`)))
	if devices.Code != 200 || probe.request.AccessSecret != "secret-value" || probe.request.AccessID != "access-id" || probe.request.Region != "eu" {
		t.Fatalf("stored credentials were not reused: status %d, request %#v", devices.Code, probe.request)
	}
}

func TestTinyTuyaDevicesReusesStoredSecretWhenAccessIDStillPresent(t *testing.T) {
	probe := &recordingTinyTuyaProbe{}
	store := tinytuya.NewCredentialStore(t.TempDir())
	router := NewRouterWithTinyTuya(registry.New(), nil, nil, probe, store)

	save := httptest.NewRecorder()
	router.ServeHTTP(save, httptest.NewRequest("POST", "/api/v1/tiny-tuya/credentials", strings.NewReader(`{"region":"eu","access_id":"access-id","access_secret":"secret-value"}`)))
	if save.Code != 200 {
		t.Fatalf("save response %d: %s", save.Code, save.Body.String())
	}

	// The Access ID field is still populated (autofill or a leftover from an
	// earlier query) but the secret was cleared - the stored secret must be used.
	devices := httptest.NewRecorder()
	router.ServeHTTP(devices, httptest.NewRequest("POST", "/api/v1/tiny-tuya/devices", strings.NewReader(`{"region":"eu","access_id":"access-id"}`)))
	if devices.Code != 200 || probe.request.AccessSecret != "secret-value" || probe.request.AccessID != "access-id" {
		t.Fatalf("stored secret was not reused: status %d, request %#v", devices.Code, probe.request)
	}

	// A genuinely different Access ID without a secret is still rejected.
	rejected := httptest.NewRecorder()
	router.ServeHTTP(rejected, httptest.NewRequest("POST", "/api/v1/tiny-tuya/devices", strings.NewReader(`{"region":"eu","access_id":"other-id"}`)))
	if rejected.Code != 400 {
		t.Fatalf("mismatched Access ID should be rejected, got %d: %s", rejected.Code, rejected.Body.String())
	}
}

func TestTinyTuyaConfigureSavesValidatedDevice(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tuya_devices.json"), []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tuya_devices.schema.json"), []byte(`{"type":"array","minItems":1,"items":{"type":"object","required":["id","name","device_id","local_key","ip","datapoints"],"additionalProperties":false,"properties":{"id":{"type":"string","minLength":1},"name":{"type":"string","minLength":1},"device_id":{"type":"string","minLength":1},"local_key":{"type":"string","minLength":1},"ip":{"type":"string","minLength":1},"version":{"type":"number","minimum":3},"device_type":{"type":"string","minLength":1},"datapoints":{"type":"object","required":["switch"],"properties":{"switch":{"type":"string","minLength":1}}}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	manager := config.NewManager(dir)
	router := NewRouterWithTinyTuya(registry.New(), manager, nil, fakeTinyTuyaProbe{}, nil)
	request := httptest.NewRequest("POST", "/api/v1/tiny-tuya/configure", strings.NewReader(`{"id":"tuya_test","name":"Testgerät","device_id":"device-1","local_key":"local-key","ip":"192.0.2.10","version":3.3,"device_type":"valve","datapoints":{"switch":"1"}}`))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("configure status %d: %s", recorder.Code, recorder.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "tuya_devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "local-key") || !strings.Contains(string(data), "tuya_test") {
		t.Fatalf("saved device is incomplete: %s", data)
	}
	if strings.Contains(recorder.Body.String(), "local-key") {
		t.Fatalf("response exposed local key: %s", recorder.Body.String())
	}
}

func TestConfigurationSchemaEndpoint(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "devices.json"), []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devices.schema.json"), []byte(`{"type":"array","items":{"type":"object"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(registry.New(), config.NewManager(dir), settings.NewStore(t.TempDir()))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/configurations/devices/schema", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"type":"array"`) {
		t.Fatalf("schema was not returned: %s", recorder.Body.String())
	}
}

func TestShellyPresetsEndpointReturnsPresetsAndChecksMethod(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shelly_presets.schema.json"), []byte(`{"type":"array","items":{"type":"object","required":["id","name","properties"],"properties":{"id":{"type":"string"},"name":{"type":"string"},"properties":{"type":"object"}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shelly_presets.json"), []byte(`[{"id":"shelly_1_gen1","name":"Shelly 1 (Gen1)","properties":{"generation":1,"switch_channels":1}}]`), 0600); err != nil {
		t.Fatal(err)
	}
	store := shellypresets.NewStore(filepath.Join(dir, "shelly_presets.json"))
	router := NewRouterWithDependencies(registry.New(), nil, nil, nil, nil, nil, nil, nil, RouterDependencies{ShellyPresets: store})

	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, httptest.NewRequest("GET", "/api/v1/shelly/presets", nil))
	if getRecorder.Code != 200 || !strings.Contains(getRecorder.Body.String(), `"id":"shelly_1_gen1"`) {
		t.Fatalf("GET status %d: %s", getRecorder.Code, getRecorder.Body.String())
	}

	postRecorder := httptest.NewRecorder()
	router.ServeHTTP(postRecorder, httptest.NewRequest("POST", "/api/v1/shelly/presets", nil))
	if postRecorder.Code != 405 {
		t.Fatalf("POST status %d, want 405", postRecorder.Code)
	}
}

func TestShellyPresetsEndpointReturnsServiceUnavailableWithoutStore(t *testing.T) {
	router := NewRouterWithDependencies(registry.New(), nil, nil, nil, nil, nil, nil, nil, RouterDependencies{})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/shelly/presets", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("got status %d, want 503: %s", recorder.Code, recorder.Body.String())
	}
}

func TestConfigurationReloadEndpointInvokesServiceReload(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "devices.json"), []byte(`[{"id":"one"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devices.schema.json"), []byte(`{"type":"array"}`), 0600); err != nil {
		t.Fatal(err)
	}
	manager := config.NewManager(dir)
	reloaded := false
	manager.SetReloadFunc(func(name string, data []byte) error {
		if name != "devices" || string(data) != `[{"id":"one"}]` {
			t.Fatalf("unexpected reload callback: %s %s", name, data)
		}
		reloaded = true
		return nil
	})

	recorder := httptest.NewRecorder()
	NewRouter(registry.New(), manager, settings.NewStore(t.TempDir())).ServeHTTP(
		recorder,
		httptest.NewRequest("POST", "/api/v1/configurations/devices/reload", nil),
	)
	if recorder.Code != 200 || !reloaded {
		t.Fatalf("reload status %d, callback invoked=%v: %s", recorder.Code, reloaded, recorder.Body.String())
	}
}

func TestLayoutRevisionEndpoints(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{ID: "first", Name: "First", Order: 0, Groups: []settings.Group{}}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{ID: "second", Name: "Second", Order: 0, Groups: []settings.Group{}}}}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(registry.New(), config.NewManager(t.TempDir()), store)
	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest("GET", "/api/v1/layout/revisions", nil))
	if list.Code != 200 {
		t.Fatalf("list status %d: %s", list.Code, list.Body.String())
	}
	var revisions []settings.Revision
	if err := json.NewDecoder(list.Body).Decode(&revisions); err != nil || len(revisions) != 1 {
		t.Fatalf("revisions %#v, err %v", revisions, err)
	}
	read := httptest.NewRecorder()
	router.ServeHTTP(read, httptest.NewRequest("GET", "/api/v1/layout/revisions/"+revisions[0].Name, nil))
	if read.Code != 200 || !strings.Contains(read.Body.String(), "first") {
		t.Fatalf("read status %d: %s", read.Code, read.Body.String())
	}
	restore := httptest.NewRecorder()
	restoreRequest := httptest.NewRequest("POST", "/api/v1/layout/restore", strings.NewReader(`{"revision":"`+revisions[0].Name+`"}`))
	restoreRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(restore, restoreRequest)
	if restore.Code != 200 || !strings.Contains(restore.Body.String(), "first") {
		t.Fatalf("restore status %d: %s", restore.Code, restore.Body.String())
	}
}

func TestDeviceMapEndpoints(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	router := NewRouter(registry.New(), config.NewManager(t.TempDir()), store)

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest("GET", "/api/v1/device/map", nil))
	if get.Code != 200 {
		t.Fatalf("get status %d: %s", get.Code, get.Body.String())
	}

	put := httptest.NewRecorder()
	body := `{"version":1,"nodes":[{"device_id":"node-a","x":12.5,"y":-3}],"view":{"snap_to_grid":true,"show_grid":true,"grid_size":20,"edge_style":"elbow"}}`
	putRequest := httptest.NewRequest("PUT", "/api/v1/device/map", strings.NewReader(body))
	putRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(put, putRequest)
	if put.Code != 200 {
		t.Fatalf("put status %d: %s", put.Code, put.Body.String())
	}

	get2 := httptest.NewRecorder()
	router.ServeHTTP(get2, httptest.NewRequest("GET", "/api/v1/device/map", nil))
	var deviceMap settings.DeviceMap
	if err := json.NewDecoder(get2.Body).Decode(&deviceMap); err != nil {
		t.Fatal(err)
	}
	if len(deviceMap.Nodes) != 1 || deviceMap.Nodes[0].DeviceID != "node-a" || deviceMap.Nodes[0].X != 12.5 {
		t.Fatalf("device-map after PUT = %#v", deviceMap)
	}
	wantView := settings.DeviceMapView{SnapToGrid: true, ShowGrid: true, GridSize: 20, EdgeStyle: "elbow"}
	if deviceMap.View != wantView {
		t.Fatalf("device-map view after PUT = %#v, want %#v", deviceMap.View, wantView)
	}
}

func TestDeviceMapRelationsEndpoints(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "child", Name: "Child", ViaDevice: "parent"}, Entity: registry.EntityInfo{UniqueID: "child_entity"}})
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "parent", Name: "Parent"}, Entity: registry.EntityInfo{UniqueID: "parent_entity"}})
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "other", Name: "Other"}, Entity: registry.EntityInfo{UniqueID: "other_entity"}})
	router := NewRouter(reg, config.NewManager(t.TempDir()), store)

	postRelation := func(childID, parentID string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest("POST", "/api/v1/device/map/relations", strings.NewReader(
			`{"child_id":"`+childID+`","parent_id":"`+parentID+`"}`,
		))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		return recorder
	}

	created := postRelation("other", "parent")
	if created.Code != http.StatusCreated {
		t.Fatalf("create relation status %d: %s", created.Code, created.Body.String())
	}
	var override settings.RelationOverride
	if err := json.NewDecoder(created.Body).Decode(&override); err != nil || override.ID == "" {
		t.Fatalf("decode created relation: %v, body %s", err, created.Body.String())
	}

	parentView, ok := reg.Get("parent")
	if !ok {
		t.Fatal("parent device missing from registry")
	}
	foundOverrideChild := false
	for _, relation := range parentView.Relations {
		if relation.Kind == "child" && relation.ID == "other" {
			foundOverrideChild = true
		}
	}
	if !foundOverrideChild {
		t.Fatalf("registry was not updated live after POST relations: %#v", parentView.Relations)
	}

	if cycle := postRelation("parent", "other"); cycle.Code != http.StatusBadRequest {
		t.Fatalf("cyclic relation status %d: %s, want 400", cycle.Code, cycle.Body.String())
	}

	if unknown := postRelation("does-not-exist", "parent"); unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown device relation status %d: %s, want 404", unknown.Code, unknown.Body.String())
	}

	if selfRef := postRelation("parent", "parent"); selfRef.Code != http.StatusBadRequest {
		t.Fatalf("self-referencing relation status %d: %s, want 400", selfRef.Code, selfRef.Body.String())
	}

	deleteRecorder := httptest.NewRecorder()
	router.ServeHTTP(deleteRecorder, httptest.NewRequest("DELETE", "/api/v1/device/map/relations/"+override.ID, nil))
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete relation status %d: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}

	getAfterDelete := httptest.NewRecorder()
	router.ServeHTTP(getAfterDelete, httptest.NewRequest("GET", "/api/v1/device/map", nil))
	var deviceMap settings.DeviceMap
	if err := json.NewDecoder(getAfterDelete.Body).Decode(&deviceMap); err != nil {
		t.Fatal(err)
	}
	if len(deviceMap.Edges) != 0 {
		t.Fatalf("edges after delete = %#v, want none", deviceMap.Edges)
	}

	parentAfterDelete, _ := reg.Get("parent")
	for _, relation := range parentAfterDelete.Relations {
		if relation.Kind == "child" && relation.ID == "other" {
			t.Fatal("registry still reports the deleted override relation")
		}
	}
}

func TestDeviceMapRevisionEndpoints(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	if err := store.SaveDeviceMap(settings.DeviceMap{Nodes: []settings.DeviceMapNode{{DeviceID: "first", X: 1, Y: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDeviceMap(settings.DeviceMap{Nodes: []settings.DeviceMapNode{{DeviceID: "second", X: 2, Y: 2}}}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(registry.New(), config.NewManager(t.TempDir()), store)
	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest("GET", "/api/v1/device/map/revisions", nil))
	if list.Code != 200 {
		t.Fatalf("list status %d: %s", list.Code, list.Body.String())
	}
	var revisions []settings.Revision
	if err := json.NewDecoder(list.Body).Decode(&revisions); err != nil || len(revisions) != 1 {
		t.Fatalf("revisions %#v, err %v", revisions, err)
	}
	restore := httptest.NewRecorder()
	restoreRequest := httptest.NewRequest("POST", "/api/v1/device/map/restore", strings.NewReader(`{"revision":"`+revisions[0].Name+`"}`))
	restoreRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(restore, restoreRequest)
	if restore.Code != 200 || !strings.Contains(restore.Body.String(), "first") {
		t.Fatalf("restore status %d: %s", restore.Code, restore.Body.String())
	}
}

func TestLiveUpdateIntervalFollowsSettingsStore(t *testing.T) {
	if got, want := liveUpdateInterval(nil), time.Duration(settings.Default().LiveUpdateIntervalSeconds)*time.Second; got != want {
		t.Fatalf("nil store interval = %v, want %v", got, want)
	}

	store := settings.NewStore(t.TempDir())
	if got, want := liveUpdateInterval(store), 3*time.Second; got != want {
		t.Fatalf("fresh store interval = %v, want %v", got, want)
	}

	value, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	value.LiveUpdateIntervalSeconds = 10
	if err := store.SaveSettings(value); err != nil {
		t.Fatal(err)
	}
	if got, want := liveUpdateInterval(store), 10*time.Second; got != want {
		t.Fatalf("configured store interval = %v, want %v", got, want)
	}
}

func TestTopicSamplesEndpointReturnsLastPayloadPerTopic(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "battery"},
		Entity: registry.EntityInfo{UniqueID: "battery-soc", StateTopic: "outstation/battery/state", AvailabilityTopic: "outstation/battery/status"},
	})
	reg.UpdateState("outstation/battery/state", []byte(`{"soc":50}`), false, time.Now())

	router := NewRouter(reg, config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/topics/samples", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	var samples []registry.TopicSample
	if err := json.NewDecoder(recorder.Body).Decode(&samples); err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 {
		t.Fatalf("unexpected samples: %#v", samples)
	}
	if samples[0].Topic != "outstation/battery/state" || samples[0].Payload != `{"soc":50}` {
		t.Fatalf("state sample = %#v", samples[0])
	}
	if samples[1].Topic != "outstation/battery/status" || samples[1].Payload != "" {
		t.Fatalf("availability sample = %#v", samples[1])
	}

	// The plain /api/v1/topics route must stay untouched by the new one.
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/topics", nil))
	if recorder.Code != 200 {
		t.Fatalf("/api/v1/topics broke: status %d", recorder.Code)
	}
}

func TestTopicSamplesEndpointRejectsOtherMethods(t *testing.T) {
	router := NewRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/api/v1/topics/samples", nil))
	if recorder.Code != 405 {
		t.Fatalf("got status %d, want 405", recorder.Code)
	}
}

func TestTopicSamplesEndpointFiltersWithTopicParameter(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "battery"},
		Entity: registry.EntityInfo{UniqueID: "battery-soc", StateTopic: "outstation/battery/state"},
	})
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "inverter"},
		Entity: registry.EntityInfo{UniqueID: "inv-power", StateTopic: "outstation/inverter/power"},
	})
	reg.UpdateState("outstation/battery/state", []byte(`{"soc":50}`), false, time.Now())

	router := NewRouter(reg, config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/topics/samples?topic=outstation/battery/state", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	var samples []registry.TopicSample
	if err := json.NewDecoder(recorder.Body).Decode(&samples); err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 {
		t.Fatalf("expected exactly one sample, got %d: %#v", len(samples), samples)
	}
	if samples[0].Topic != "outstation/battery/state" || samples[0].Payload != `{"soc":50}` {
		t.Fatalf("sample = %#v", samples[0])
	}
}

func TestAutomationRulesPutRequiresAuth(t *testing.T) {
	// Setup: create temp directories and seed config
	configDir := t.TempDir()
	authDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "automation_rules.json"), []byte(`{"version":1,"settings":{},"rules":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "automation_rules.schema.json"), []byte(`{"type":"object"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Create auth manager
	authManager, err := auth.NewManager(filepath.Join(authDir, "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}

	// Build router with auth manager (use NewAuthenticatedRouter to include authMiddleware)
	configManager := config.NewManager(configDir)
	router := NewAuthenticatedRouter(registry.New(), configManager, settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: authManager})

	// Test: PUT without session cookie should return 401
	req := httptest.NewRequest(http.MethodPut, "/api/v1/configurations/automation_rules", strings.NewReader(`{"version":1,"settings":{},"rules":[]}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAutomationRulesPutAllowsGuestWithCSRF(t *testing.T) {
	// Setup: create temp directories and seed config
	configDir := t.TempDir()
	authDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "automation_rules.json"), []byte(`{"version":1,"settings":{},"rules":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "automation_rules.schema.json"), []byte(`{"type":"object"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Create auth manager and guest session
	authManager, err := auth.NewManager(filepath.Join(authDir, "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	guestSession, err := authManager.ContinueAsGuest()
	if err != nil {
		t.Fatal(err)
	}

	// Build router with auth manager (use NewAuthenticatedRouter to include authMiddleware)
	configManager := config.NewManager(configDir)
	router := NewAuthenticatedRouter(registry.New(), configManager, settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: authManager})

	// Test: PUT with guest session + correct CSRF header should succeed (200)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/configurations/automation_rules", strings.NewReader(`{"version":1,"settings":{},"rules":[]}`))
	req.AddCookie(&http.Cookie{Name: "energy_node_guest_session", Value: guestSession.CookieValue()})
	req.Header.Set("X-CSRF-Token", guestSession.CSRFToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

type recordingPublisher struct {
	topic   string
	payload string
	err     error
	calls   int
}

func (p *recordingPublisher) Publish(topic, payload string) error {
	p.calls++
	p.topic = topic
	p.payload = payload
	return p.err
}

// postAutomationTest builds the same authenticated router the tests for
// PUT /api/v1/configurations/automation_rules use (guest session, cookie,
// X-CSRF-Token from /api/v1/auth/guest) and POSTs body to
// /api/v1/automations/test with a valid CSRF header.
func postAutomationTest(t *testing.T, publisher CommandPublisher, body string) *httptest.ResponseRecorder {
	t.Helper()
	authDir := t.TempDir()
	authManager, err := auth.NewManager(filepath.Join(authDir, "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	guestSession, err := authManager.ContinueAsGuest()
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, publisher, nil, nil, RouterDependencies{Auth: authManager})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/automations/test", strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "energy_node_guest_session", Value: guestSession.CookieValue()})
	req.Header.Set("X-CSRF-Token", guestSession.CSRFToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

// postAutomationTestWithoutCSRF is postAutomationTest without the
// X-CSRF-Token header, to exercise the CSRF rejection path.
func postAutomationTestWithoutCSRF(t *testing.T, publisher CommandPublisher, body string) *httptest.ResponseRecorder {
	t.Helper()
	authDir := t.TempDir()
	authManager, err := auth.NewManager(filepath.Join(authDir, "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	guestSession, err := authManager.ContinueAsGuest()
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, publisher, nil, nil, RouterDependencies{Auth: authManager})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/automations/test", strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "energy_node_guest_session", Value: guestSession.CookieValue()})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

// getAuthSessionAsGuest follows the same guest-session setup and fetches
// GET /api/v1/auth/session, returning the raw JSON body.
func getAuthSessionAsGuest(t *testing.T) []byte {
	t.Helper()
	authDir := t.TempDir()
	authManager, err := auth.NewManager(filepath.Join(authDir, "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	guestSession, err := authManager.ContinueAsGuest()
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: authManager})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: "energy_node_guest_session", Value: guestSession.CookieValue()})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	return recorder.Body.Bytes()
}

func TestAutomationTestRejectsGet(t *testing.T) {
	publisher := &recordingPublisher{}
	handler := handleAutomationTest(publisher, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/automations/test", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("erwartet 405, bekommen %d", recorder.Code)
	}
	if publisher.calls != 0 {
		t.Fatalf("es darf nichts publiziert werden, es gab aber %d Aufrufe", publisher.calls)
	}
}

func TestAutomationTestRequiresAuthentication(t *testing.T) {
	publisher := &recordingPublisher{}
	handler := handleAutomationTest(publisher, nil)
	recorder := httptest.NewRecorder()
	body := strings.NewReader(`{"rule_id":"r1","action_index":0}`)
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/automations/test", body))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("erwartet 401, bekommen %d", recorder.Code)
	}
	if publisher.calls != 0 {
		t.Fatalf("es darf nichts publiziert werden, es gab aber %d Aufrufe", publisher.calls)
	}
}

func TestAutomationTestPublishesToTheFixedTopic(t *testing.T) {
	publisher := &recordingPublisher{}
	response := postAutomationTest(t, publisher, `{"rule_id":"r1","action_index":2}`)
	if response.Code != http.StatusOK {
		t.Fatalf("erwartet 200, bekommen %d (%s)", response.Code, response.Body.String())
	}
	if publisher.topic != "outstation/automation/test/set" {
		t.Fatalf("falsches Topic: %q", publisher.topic)
	}
	var sent struct {
		RuleID      string `json:"rule_id"`
		ActionIndex int    `json:"action_index"`
	}
	if err := json.Unmarshal([]byte(publisher.payload), &sent); err != nil {
		t.Fatalf("Payload ist kein JSON: %v", err)
	}
	if sent.RuleID != "r1" || sent.ActionIndex != 2 {
		t.Fatalf("falsche Nutzlast: %q", publisher.payload)
	}
}

func TestAutomationTestRejectsAMissingActionIndex(t *testing.T) {
	publisher := &recordingPublisher{}
	response := postAutomationTest(t, publisher, `{"rule_id":"r1"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("erwartet 400, bekommen %d", response.Code)
	}
	if publisher.calls != 0 {
		t.Fatalf("es darf nichts publiziert werden, es gab aber %d Aufrufe", publisher.calls)
	}
}

func TestAutomationTestRejectsANegativeActionIndex(t *testing.T) {
	publisher := &recordingPublisher{}
	response := postAutomationTest(t, publisher, `{"rule_id":"r1","action_index":-1}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("erwartet 400, bekommen %d", response.Code)
	}
	if publisher.calls != 0 {
		t.Fatalf("es darf nichts publiziert werden, es gab aber %d Aufrufe", publisher.calls)
	}
}

func TestAutomationTestRejectsAMissingCSRFToken(t *testing.T) {
	publisher := &recordingPublisher{}
	response := postAutomationTestWithoutCSRF(t, publisher, `{"rule_id":"r1","action_index":0}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("erwartet 403, bekommen %d", response.Code)
	}
	if publisher.calls != 0 {
		t.Fatalf("es darf nichts publiziert werden, es gab aber %d Aufrufe", publisher.calls)
	}
}

func TestAuthSessionReportsTheAutomationsRole(t *testing.T) {
	body := getAuthSessionAsGuest(t)
	var session map[string]any
	if err := json.Unmarshal(body, &session); err != nil {
		t.Fatalf("Sitzungsantwort ist kein JSON: %v", err)
	}
	if session["automations"] != true {
		t.Fatalf("Gäste haben laut auth.go:182-186 RoleAutomations, das Flag fehlt aber: %v", session)
	}
}

// TestDeviceEndpointKeepsLastMessage haelt die Zusicherung fest, auf der die
// Benachrichtigungen (notifications.js) und der Automations-Tick
// (automations.page.js) stehen: beide lesen den rohen, ungetemplateten
// MQTT-Payload aus last_message und holen ihn seit dem Serverlast-Spec ueber
// die Einzelgeraete-Route statt ueber die 448-KB-Liste. Faellt last_message
// hier weg, verstummen die Toasts stillschweigend - der catch-Block im
// Browser schluckt den Fehler.
func TestDeviceEndpointKeepsLastMessage(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "automation", Name: "Automation"},
		Entity: registry.EntityInfo{UniqueID: "automation_last_event", ObjectID: "last_event", StateTopic: "outstation/automation/last_event"},
	})
	reg.UpdateState("outstation/automation/last_event", []byte(`{"at":100,"message":"info: Test"}`), false, time.Now().UTC())

	recorder := httptest.NewRecorder()
	NewRouter(reg, nil, nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/devices/automation", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("device response %d: %s", recorder.Code, recorder.Body.String())
	}

	var device struct {
		Entities []struct {
			ObjectID    string `json:"object_id"`
			LastMessage *struct {
				Payload string `json:"payload"`
			} `json:"last_message"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &device); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, entity := range device.Entities {
		if entity.ObjectID != "last_event" {
			continue
		}
		found = true
		if entity.LastMessage == nil || entity.LastMessage.Payload != `{"at":100,"message":"info: Test"}` {
			t.Fatalf("last_message missing or altered: %#v", entity.LastMessage)
		}
	}
	if !found {
		t.Fatalf("last_event entity missing from single-device response: %s", recorder.Body.String())
	}

	missing := httptest.NewRecorder()
	NewRouter(reg, nil, nil).ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/v1/devices/gibtesnicht", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown device should 404, got %d: %s", missing.Code, missing.Body.String())
	}
}

// TestDiscoverySummaryMatchesDiscovery bindet die neue Route an die alte: sie
// darf nur weglassen, nie widersprechen. /api/v1/discovery bleibt unveraendert
// - externe Nutzer und die Discovery-Löschvorschau im Modal brauchen es weiter.
func TestDiscoverySummaryMatchesDiscovery(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device:         registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity:         registry.EntityInfo{UniqueID: "node_temp", ObjectID: "temperature", StateTopic: "state/node/temp"},
		DiscoveryTopic: "homeassistant/sensor/node/temp/config",
	})
	reg.UpsertEntity(registry.Discovery{
		Device:         registry.DeviceInfo{ID: "other", Name: "Other"},
		Entity:         registry.EntityInfo{UniqueID: "other_hum", ObjectID: "humidity", StateTopic: "state/other/hum"},
		DiscoveryTopic: "homeassistant/sensor/other/hum/config",
	})
	reg.RecordDiscoveryError("homeassistant/sensor/node/bad/config", "{", "invalid character", true, 1, "mqtt", time.Now().UTC())

	router := NewRouter(reg, nil, nil)

	full := httptest.NewRecorder()
	router.ServeHTTP(full, httptest.NewRequest(http.MethodGet, "/api/v1/discovery", nil))
	var discovery struct {
		Devices []struct {
			Entities []json.RawMessage `json:"entities"`
		} `json:"devices"`
		DiscoveryErrors []json.RawMessage   `json:"discovery_errors"`
		DuplicateIDs    map[string][]string `json:"duplicate_ids"`
	}
	if err := json.Unmarshal(full.Body.Bytes(), &discovery); err != nil {
		t.Fatal(err)
	}
	wantEntities := 0
	for _, device := range discovery.Devices {
		wantEntities += len(device.Entities)
	}

	summary := httptest.NewRecorder()
	router.ServeHTTP(summary, httptest.NewRequest(http.MethodGet, "/api/v1/discovery/summary", nil))
	if summary.Code != http.StatusOK {
		t.Fatalf("summary response %d: %s", summary.Code, summary.Body.String())
	}
	var got struct {
		DeviceCount     int                 `json:"device_count"`
		EntityCount     int                 `json:"entity_count"`
		DiscoveryErrors []json.RawMessage   `json:"discovery_errors"`
		DuplicateIDs    map[string][]string `json:"duplicate_ids"`
	}
	if err := json.Unmarshal(summary.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.DeviceCount != len(discovery.Devices) || got.EntityCount != wantEntities {
		t.Fatalf("summary counts (%d, %d) do not match discovery (%d, %d)", got.DeviceCount, got.EntityCount, len(discovery.Devices), wantEntities)
	}
	if len(got.DiscoveryErrors) != len(discovery.DiscoveryErrors) {
		t.Fatalf("summary has %d discovery errors, discovery has %d", len(got.DiscoveryErrors), len(discovery.DiscoveryErrors))
	}
	if len(got.DuplicateIDs) != len(discovery.DuplicateIDs) {
		t.Fatalf("summary has %d duplicate ids, discovery has %d", len(got.DuplicateIDs), len(discovery.DuplicateIDs))
	}
	if strings.Contains(summary.Body.String(), "discovery_json") || strings.Contains(summary.Body.String(), `"devices"`) {
		t.Fatalf("summary must not carry device trees: %s", summary.Body.String())
	}

	post := httptest.NewRecorder()
	router.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/v1/discovery/summary", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST summary response %d: %s", post.Code, post.Body.String())
	}
}

// TestDiscoverySummaryUsesRegistryETag: die Route geht ueber
// writeVersionedJSON, damit die ETag-/304-Logik unveraendert mitkommt.
func TestDiscoverySummaryUsesRegistryETag(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_temp", ObjectID: "temperature", StateTopic: "state/node/temp"},
	})
	router := NewRouter(reg, nil, nil)

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/discovery/summary", nil))
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("summary response carries no ETag: %v", first.Header())
	}

	conditional := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/summary", nil)
	conditional.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	router.ServeHTTP(second, conditional)
	if second.Code != http.StatusNotModified {
		t.Fatalf("conditional summary response %d, want 304: %s", second.Code, second.Body.String())
	}
}

// TestLayoutGetShipsCardCatalog: der Editor laedt /api/v1/layout ohnehin, ein
// zweiter Rundlauf fuer den Katalog waere unnoetig. PUT darf das Zusatzfeld
// nicht zurueckverlangen - es ist reine Ausgabe.
func TestLayoutGetShipsCardCatalog(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	// Erst speichern: LoadLayout gibt fuer ein fehlendes layout.json ein
	// leeres Layout *ohne* Normalisierung zurueck, version waere dann 0.
	if err := store.SaveLayout(settings.Layout{Version: 3, Pages: []settings.Page{{
		ID: "p", Name: "P", Order: 0,
		Groups: []settings.Group{{ID: "g", Name: "G", Items: []settings.Item{
			{ID: "band", Type: "energy_band", Span: "2", Visible: true},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(registry.New(), config.NewManager(t.TempDir()), store)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/layout", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Version   int                          `json:"version"`
		CardTypes map[string]settings.CardType `json:"card_types"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Version != 3 {
		t.Fatalf("version = %d, want 3", body.Version)
	}
	band, ok := body.CardTypes["energy_band"]
	if !ok {
		t.Fatalf("card_types enthaelt energy_band nicht: %#v", body.CardTypes)
	}
	if band.MinSpan != 2 || band.MinWidth != "34rem" || band.DefaultSpan != "2" {
		t.Fatalf("energy_band = %#v", band)
	}
	// CardCatalog() statt CardTypeNames(): seit device:compact (Task 2 des
	// Geraetekachel-Plans) enthaelt der Katalog auch Darstellungsvarianten,
	// die kein eigener Item-Typ sind und darum in CardTypeNames() fehlen.
	if len(body.CardTypes) != len(settings.CardCatalog()) {
		t.Fatalf("card_types hat %d Eintraege, Katalog %d", len(body.CardTypes), len(settings.CardCatalog()))
	}
}

func TestSettingsRevisionEndpoints(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	first := settings.Default()
	first.HealthScoreThreshold = 3
	second := settings.Default()
	second.HealthScoreThreshold = 9
	if err := store.SaveSettings(first); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSettings(second); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(registry.New(), config.NewManager(t.TempDir()), store)

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest("GET", "/api/v1/settings/revisions", nil))
	if list.Code != 200 {
		t.Fatalf("list status %d: %s", list.Code, list.Body.String())
	}
	var revisions []settings.Revision
	if err := json.NewDecoder(list.Body).Decode(&revisions); err != nil || len(revisions) != 1 {
		t.Fatalf("revisions %#v, err %v", revisions, err)
	}

	read := httptest.NewRecorder()
	router.ServeHTTP(read, httptest.NewRequest("GET", "/api/v1/settings/revisions/"+revisions[0].Name, nil))
	if read.Code != 200 || !strings.Contains(read.Body.String(), `"health_score_threshold":3`) {
		t.Fatalf("read status %d: %s", read.Code, read.Body.String())
	}

	restore := httptest.NewRecorder()
	restoreRequest := httptest.NewRequest("POST", "/api/v1/settings/restore", strings.NewReader(`{"revision":"`+revisions[0].Name+`"}`))
	restoreRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(restore, restoreRequest)
	if restore.Code != 200 || !strings.Contains(restore.Body.String(), `"health_score_threshold":3`) {
		t.Fatalf("restore status %d: %s", restore.Code, restore.Body.String())
	}

	// Ein unbekannter Revisionsname darf keine 500 werfen.
	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest("GET", "/api/v1/settings/revisions/does-not-exist.json", nil))
	if missing.Code != 404 {
		t.Fatalf("missing revision status %d, want 404: %s", missing.Code, missing.Body.String())
	}
}

func TestEnergyRevisionEndpointsDoNotShadowRolesRoute(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	if err := store.SaveEnergy(settings.EnergyConfig{Assignments: map[string]energy.Assignment{
		"sensor.pv": {Role: "pv", Scale: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveEnergy(settings.EnergyConfig{Assignments: map[string]energy.Assignment{
		"sensor.grid": {Role: "grid", Scale: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(registry.New(), config.NewManager(t.TempDir()), store)

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest("GET", "/api/v1/energy/revisions", nil))
	if list.Code != 200 {
		t.Fatalf("list status %d: %s", list.Code, list.Body.String())
	}
	var revisions []settings.Revision
	if err := json.NewDecoder(list.Body).Decode(&revisions); err != nil || len(revisions) != 1 {
		t.Fatalf("revisions %#v, err %v", revisions, err)
	}

	restore := httptest.NewRecorder()
	restoreRequest := httptest.NewRequest("POST", "/api/v1/energy/restore", strings.NewReader(`{"revision":"`+revisions[0].Name+`"}`))
	restoreRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(restore, restoreRequest)
	if restore.Code != 200 || !strings.Contains(restore.Body.String(), "sensor.pv") {
		t.Fatalf("restore status %d: %s", restore.Code, restore.Body.String())
	}

	// Der neue Praefix-Handler darf die exakte roles-Route nicht verdraengen.
	roles := httptest.NewRecorder()
	router.ServeHTTP(roles, httptest.NewRequest("GET", "/api/v1/energy/roles", nil))
	if roles.Code != 200 {
		t.Fatalf("roles status %d, want 200: %s", roles.Code, roles.Body.String())
	}
}

func TestDiagnosticsReportsConfiguredDeviceMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shelly_devices.json"), []byte(`[{"id":"batterie_spannung","name":"Shelly Uni"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shelly_devices.schema.json"), []byte(`{"type":"array"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	router := NewRouterWithDependencies(registry.New(), config.NewManager(dir), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{StartedAt: time.Now().UTC().Add(-time.Hour)})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"rule_id":"ConfiguredDeviceMissing"`) {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestAutomationHistoryReturnsRuleEvents(t *testing.T) {
	dir := t.TempDir()
	body := `{"r1":[{"at":1,"result":"fired","test":false,"actions":[]}]}`
	if err := os.WriteFile(filepath.Join(dir, "automation_history.json"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	handler := handleAutomationHistory(config.NewManager(dir))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/automations/history/r1", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var events []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &events); err != nil {
		t.Fatalf("Antwort ist kein JSON-Array: %v", err)
	}
	if len(events) != 1 || events[0]["result"] != "fired" {
		t.Fatalf("unerwarteter Inhalt: %s", recorder.Body.String())
	}
}

func TestAutomationHistoryReturnsEmptyForMissingFile(t *testing.T) {
	handler := handleAutomationHistory(config.NewManager(t.TempDir()))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/automations/history/r1", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if strings.TrimSpace(recorder.Body.String()) != "[]" {
		t.Fatalf("body = %q, want []", recorder.Body.String())
	}
}

func TestAutomationHistoryReturnsEmptyForUnknownRule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "automation_history.json"), []byte(`{"other":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	handler := handleAutomationHistory(config.NewManager(dir))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/automations/history/r1", nil))
	if strings.TrimSpace(recorder.Body.String()) != "[]" {
		t.Fatalf("body = %q, want []", recorder.Body.String())
	}
}

func TestAutomationHistoryRejectsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "automation_history.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	handler := handleAutomationHistory(config.NewManager(dir))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/automations/history/r1", nil))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", recorder.Code)
	}
}

func TestAutomationHistoryRejectsPost(t *testing.T) {
	handler := handleAutomationHistory(config.NewManager(t.TempDir()))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/automations/history/r1", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
}

func TestHealthNenntDieAustauschFaehigkeit(t *testing.T) {
	router := NewRouter(registry.New(), nil, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var payload struct {
		Features map[string]int `json:"features"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if got := payload.Features["history_exchange"]; got != exchangeProtocolVersion {
		t.Fatalf("features.history_exchange = %d, want %d", got, exchangeProtocolVersion)
	}
}
