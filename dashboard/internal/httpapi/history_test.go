// dashboard/internal/httpapi/history_test.go
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func TestHistoryEntitiesReturnsOnlyConfiguredEntities(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "shelly"},
		Entity: registry.EntityInfo{
			UniqueID: "shelly_power", Component: "sensor", ObjectID: "power",
			Name: "Leistung", StateTopic: "shelly/power", UnitOfMeasurement: "W",
		},
	})
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "shelly"},
		Entity: registry.EntityInfo{
			UniqueID: "shelly_voltage", Component: "sensor", ObjectID: "voltage",
			Name: "Spannung", StateTopic: "shelly/voltage", UnitOfMeasurement: "V",
		},
	})
	reg.UpdateTopic("shelly/power", []byte("640.5"), false, time.Now())
	reg.UpdateTopic("shelly/voltage", []byte("231"), false, time.Now())

	store := settings.NewStore(t.TempDir())
	value := settings.Default()
	value.HistoryExtraEntities = []string{"shelly_power"}
	if err := store.SaveSettings(value); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	handleHistoryEntities(reg, store)(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/history/entities", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}

	var body struct {
		At      string `json:"at"`
		Samples []struct {
			EntityID string   `json:"entity_id"`
			Value    *float64 `json:"value"`
			Unit     string   `json:"unit"`
			Stale    bool     `json:"stale"`
		} `json:"samples"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.At == "" {
		t.Fatal("response is missing the sample timestamp")
	}
	if len(body.Samples) != 1 {
		t.Fatalf("got %d samples, want exactly the one configured entity", len(body.Samples))
	}
	if body.Samples[0].EntityID != "shelly_power" {
		t.Fatalf("entity_id = %q, want shelly_power", body.Samples[0].EntityID)
	}
	if body.Samples[0].Value == nil || *body.Samples[0].Value != 640.5 {
		t.Fatalf("value = %v, want 640.5", body.Samples[0].Value)
	}
	if body.Samples[0].Unit != "W" {
		t.Fatalf("unit = %q, want W", body.Samples[0].Unit)
	}
}

func TestHistoryEntitiesReturnsEmptyListWithoutConfiguration(t *testing.T) {
	reg := registry.New()
	store := settings.NewStore(t.TempDir())

	recorder := httptest.NewRecorder()
	handleHistoryEntities(reg, store)(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/history/entities", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		Samples []json.RawMessage `json:"samples"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Samples) != 0 {
		t.Fatalf("got %d samples, want none", len(body.Samples))
	}
}

func TestHistoryEntitiesSkipsUnknownAndUnparsableEntities(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "shelly"},
		Entity: registry.EntityInfo{
			UniqueID: "shelly_mode", Component: "sensor", ObjectID: "mode",
			Name: "Betriebsart", StateTopic: "shelly/mode",
		},
	})
	reg.UpdateTopic("shelly/mode", []byte("automatik"), false, time.Now())

	store := settings.NewStore(t.TempDir())
	value := settings.Default()
	value.HistoryExtraEntities = []string{"shelly_mode", "gibt_es_nicht"}
	if err := store.SaveSettings(value); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	handleHistoryEntities(reg, store)(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/history/entities", nil))
	var body struct {
		Samples []struct {
			EntityID string   `json:"entity_id"`
			Value    *float64 `json:"value"`
		} `json:"samples"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// Eine unbekannte Entitaet faellt weg; eine bekannte mit nicht
	// numerischem Wert erscheint mit value: null statt als 0 - eine 0 waere
	// eine Messwertbehauptung, die es nicht gibt.
	if len(body.Samples) != 1 {
		t.Fatalf("got %d samples, want 1", len(body.Samples))
	}
	if body.Samples[0].EntityID != "shelly_mode" || body.Samples[0].Value != nil {
		t.Fatalf("got %+v, want shelly_mode with a null value", body.Samples[0])
	}
}

func TestHistoryEntitiesRejectsNonGET(t *testing.T) {
	recorder := httptest.NewRecorder()
	handleHistoryEntities(registry.New(), settings.NewStore(t.TempDir()))(
		recorder, httptest.NewRequest(http.MethodPost, "/api/v1/history/entities", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
}
