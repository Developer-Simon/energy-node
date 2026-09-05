package httpapi

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/diagnostics"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func eventsServer(t *testing.T, reg *registry.Registry) *httptest.Server {
	t.Helper()
	// Kurzes Lebenszeichen, damit ein blockierender Lesevorgang seine
	// Frist ueberhaupt pruefen kann - der Poll-Takt bleibt der Default.
	previous := registryStreamKeepAlive
	registryStreamKeepAlive = 100 * time.Millisecond
	t.Cleanup(func() { registryStreamKeepAlive = previous })

	// 1-Sekunden-Poll, damit eine Aenderung nach dem Verbinden zuegig
	// ankommt statt erst nach dem 3-Sekunden-Default.
	store := settings.NewStore(t.TempDir())
	value := settings.Default()
	value.LiveUpdateIntervalSeconds = 1
	if err := store.SaveSettings(value); err != nil {
		t.Fatal(err)
	}

	resolver := energy.NewResolver(nil)
	engine := diagnostics.NewEngine(reg, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/events", handleEvents(reg, store, resolver, engine))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// openEventStream oeffnet den SSE-Strom und liefert einen Reader auf den
// Rumpf.
func openEventStream(t *testing.T, server *httptest.Server) (*bufio.Reader, func()) {
	t.Helper()
	response, err := http.Get(server.URL + "/api/v1/events")
	if err != nil {
		t.Fatalf("Strom nicht erreichbar: %v", err)
	}
	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	return bufio.NewReader(response.Body), func() { response.Body.Close() }
}

// readRegistryEvent liest bis zum naechsten "event: registry" und gibt die
// zugehoerige data:-Zeile zurueck.
func readRegistryEvent(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	sawRegistry := false
	// Bequem ueber dem Standard-Poll-Takt (live_update_interval_seconds).
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("Strom abgebrochen: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "event: registry":
			sawRegistry = true
		case sawRegistry && strings.HasPrefix(line, "data: "):
			return strings.TrimPrefix(line, "data: ")
		}
	}
	t.Fatal("kein registry-Ereignis im Strom")
	return ""
}

func testRegistryWithPower(t *testing.T) *registry.Registry {
	t.Helper()
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "shelly"},
		Entity: registry.EntityInfo{
			UniqueID: "shelly_power", Component: "sensor", ObjectID: "power",
			Name: "Leistung", StateTopic: "shelly/power", UnitOfMeasurement: "W",
		},
	})
	return reg
}

func TestEventStreamSendetRumpfBeimVerbinden(t *testing.T) {
	reg := testRegistryWithPower(t)
	reg.UpdateTopic("shelly/power", []byte("42"), false, time.Now())
	server := eventsServer(t, reg)

	reader, closeStream := openEventStream(t, server)
	defer closeStream()

	data := readRegistryEvent(t, reader)
	if !strings.Contains(data, `"entities"`) || !strings.Contains(data, `"version"`) {
		t.Errorf("erstes Ereignis traegt nicht den vollen Rumpf: %s", data)
	}
}

func TestEventStreamSendetNeuenRumpfBeiRegistryAenderung(t *testing.T) {
	reg := testRegistryWithPower(t)
	server := eventsServer(t, reg)

	reader, closeStream := openEventStream(t, server)
	defer closeStream()
	first := readRegistryEvent(t, reader) // Rumpf beim Verbinden

	reg.UpdateTopic("shelly/power", []byte("99"), false, time.Now())

	second := readRegistryEvent(t, reader)
	if second == first {
		t.Errorf("Folge-Ereignis ist mit dem ersten identisch: %s", second)
	}
	if !strings.Contains(second, `"99"`) {
		t.Errorf("Folge-Ereignis traegt den neuen Wert nicht: %s", second)
	}
}

// Ohne regelmaessiges Lebenszeichen schliessen Zwischenstellen (caddy,
// tailscale) einen stillen SSE-Strom - und der Server merkt einen
// weggebrochenen Client erst beim naechsten echten Schreibversuch.
func TestEventStreamSendetLebenszeichen(t *testing.T) {
	reg := registry.New()
	server := eventsServer(t, reg) // verkuerzt registryStreamKeepAlive

	reader, closeStream := openEventStream(t, server)
	defer closeStream()
	readRegistryEvent(t, reader) // Rumpf beim Verbinden

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("Strom abgebrochen: %v", err)
		}
		if strings.HasPrefix(line, ":") {
			return
		}
	}
	t.Fatal("kein Lebenszeichen (Kommentarzeile) im Strom")
}

// Der erste Rumpf auf einer frischen Verbindung traegt alle Werte; nachdem
// sich einer bewegt hat, kommt ein Delta mit nur diesem Wert.
func TestRegistryStreamFirstFullThenDelta(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "shelly"},
		Entity: registry.EntityInfo{UniqueID: "e1", Component: "sensor", ObjectID: "power", Name: "Power", StateTopic: "shelly/power"},
	})
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "shelly"},
		Entity: registry.EntityInfo{UniqueID: "e2", Component: "sensor", ObjectID: "energy", Name: "Energy", StateTopic: "shelly/energy"},
	})
	reg.UpdateTopic("shelly/power", []byte("1"), true, time.Now())
	reg.UpdateTopic("shelly/energy", []byte("10"), true, time.Now())

	server := eventsServer(t, reg)
	reader, closeStream := openEventStream(t, server)
	defer closeStream()

	first := readRegistryEvent(t, reader)
	if strings.Contains(first, `"entities_delta"`) {
		t.Errorf("erster Rumpf ist ein Delta: %s", first)
	}
	var firstBody struct {
		Entities map[string]registry.ValueView `json:"entities"`
	}
	if err := json.Unmarshal([]byte(first), &firstBody); err != nil {
		t.Fatalf("erster Rumpf kein JSON: %v", err)
	}
	if len(firstBody.Entities) != 2 {
		t.Errorf("erster Rumpf traegt %d Entitaeten, erwartet 2", len(firstBody.Entities))
	}

	reg.UpdateTopic("shelly/power", []byte("2"), true, time.Now())

	second := readRegistryEvent(t, reader)
	var secondBody struct {
		EntitiesDelta bool                          `json:"entities_delta"`
		Entities      map[string]registry.ValueView `json:"entities"`
	}
	if err := json.Unmarshal([]byte(second), &secondBody); err != nil {
		t.Fatalf("zweiter Rumpf kein JSON: %v", err)
	}
	if !secondBody.EntitiesDelta {
		t.Errorf("zweiter Rumpf ist kein Delta: %s", second)
	}
	if len(secondBody.Entities) != 1 || secondBody.Entities["e1"].Value != "2" {
		t.Errorf("Delta = %v, erwartet nur e1=2", secondBody.Entities)
	}
}
