package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func TestSwitchStateTable(t *testing.T) {
	tests := []struct {
		name   string
		entity registry.EntityView
		want   string
	}{
		{
			name:   "on",
			entity: registry.EntityView{HasValue: true, Available: true, Value: "ON", PayloadOn: "ON", PayloadOff: "OFF"},
			want:   "on",
		},
		{
			name:   "off",
			entity: registry.EntityView{HasValue: true, Available: true, Value: "OFF", PayloadOn: "ON", PayloadOff: "OFF"},
			want:   "off",
		},
		{
			name:   "pending keeps confirmed value",
			entity: registry.EntityView{HasValue: true, Value: "OFF", PayloadOn: "ON", PayloadOff: "OFF", Pending: true, PendingValue: "ON"},
			want:   "off",
		},
		{
			name:   "stale retained value counts",
			entity: registry.EntityView{HasValue: true, Value: "OFF", PayloadOn: "ON", PayloadOff: "OFF", Stale: true},
			want:   "off",
		},
		{
			name:   "not available",
			entity: registry.EntityView{HasValue: true, HasAvailability: true, Available: false, Value: "ON", PayloadOn: "ON", PayloadOff: "OFF"},
			want:   "unknown",
		},
		{
			name:   "no value",
			entity: registry.EntityView{HasValue: false, PayloadOn: "ON", PayloadOff: "OFF"},
			want:   "unknown",
		},
		{
			name:   "foreign value with different case",
			entity: registry.EntityView{HasValue: true, Available: true, Value: "on", PayloadOn: "ON", PayloadOff: "OFF"},
			want:   "unknown",
		},
		{
			name:   "empty payloads never match an empty value",
			entity: registry.EntityView{HasValue: true, Value: ""},
			want:   "unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := switchState(tt.entity); got != tt.want {
				t.Fatalf("switchState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeviceSwitchesListsOnlyOutputSwitches(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "plug_a"},
		Entity: registry.EntityInfo{
			UniqueID: "plug_a_relay", Component: "switch", ObjectID: "relay", Name: "Relais",
			StateTopic: "plug_a/relay", CommandTopic: "plug_a/relay/set",
			PayloadOn: "ON", PayloadOff: "OFF",
		},
	})
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "plug_a"},
		Entity: registry.EntityInfo{
			UniqueID: "plug_a_child_lock", Component: "switch", ObjectID: "child_lock", Name: "Kindersicherung",
			StateTopic: "plug_a/lock", CommandTopic: "plug_a/lock/set",
			PayloadOn: "ON", PayloadOff: "OFF", EntityCategory: "config",
		},
	})
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "plug_a"},
		Entity: registry.EntityInfo{
			UniqueID: "plug_a_led", Component: "switch", ObjectID: "led", Name: "LED",
			StateTopic: "plug_a/led", CommandTopic: "plug_a/led/set",
			PayloadOn: "ON",
		},
	})
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "plug_a"},
		Entity: registry.EntityInfo{
			UniqueID: "plug_a_limit", Component: "number", ObjectID: "limit", Name: "Limit",
			StateTopic: "plug_a/limit", CommandTopic: "plug_a/limit/set",
		},
	})
	reg.UpdateTopic("plug_a/relay", []byte("ON"), false, time.Now())
	reg.UpdateTopic("plug_a/lock", []byte("ON"), false, time.Now())
	reg.UpdateTopic("plug_a/led", []byte("ON"), false, time.Now())
	reg.UpdateTopic("plug_a/limit", []byte("16"), false, time.Now())

	router := NewRouter(reg, nil, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/devices/switches", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var got []switchView
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := []switchView{{DeviceID: "plug_a", EntityID: "plug_a_relay", Name: "Relais", State: "on"}}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("switches = %+v, want %+v", got, want)
	}
}

func TestDeviceSwitchesEmptyRegistryReturnsEmptyArray(t *testing.T) {
	router := NewRouter(registry.New(), nil, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/devices/switches", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if body := recorder.Body.String(); body != "[]\n" {
		t.Fatalf("body = %q, want an empty JSON array", body)
	}
}

func TestDeviceSwitchesRejectsPost(t *testing.T) {
	router := NewRouter(registry.New(), nil, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/devices/switches", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func TestDiagnosticsRouteStillGoesToDeviceHandler(t *testing.T) {
	router := NewRouter(registry.New(), nil, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/devices/switches/diagnostics", nil))
	// No device "switches" exists, so the device handler answers with 404
	// instead of the switch list.
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}
