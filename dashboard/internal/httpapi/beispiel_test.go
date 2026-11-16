package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func newBeispielTestRouter(t *testing.T, dataDir string, store *settings.Store, sim NodeSettingsPublisher) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(dataDir), store, nil, nil, nil, nil, nil, RouterDependencies{
		Auth: manager, NodeSimulation: sim, DataDir: dataDir,
	})
	return router, manager
}

// loginBeispiel meldet einen Konto-Namen an und liefert Cookie und CSRF-Token.
func loginBeispiel(t *testing.T, router http.Handler, username, password string) (*http.Cookie, string) {
	t.Helper()
	body := `{"username":"` + username + `","password":"` + password + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s status %d: %s", username, rec.Code, rec.Body.String())
	}
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	return rec.Result().Cookies()[0], session.CSRFToken
}

func postBeispielStop(router http.Handler, cookie *http.Cookie, csrf, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/beispiel/stop", strings.NewReader(body))
	req.AddCookie(cookie)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-CSRF-Token", csrf)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestBeispielStopNeedsEditConfigForRemove(t *testing.T) {
	dataDir := t.TempDir()
	store := settings.NewStore(dataDir)
	router, manager := newBeispielTestRouter(t, dataDir, store, &fakeNodeSim{})

	const password = "mqtt-only-password"
	if _, err := manager.CreateUser("admin", "mqttonly", password, []string{auth.RoleMQTTConfig}); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginBeispiel(t, router, "mqttonly", password)

	rec := postBeispielStop(router, cookie, csrf, `{"remove":true}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("remove without edit_config: status %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "edit_config_forbidden") {
		t.Fatalf("403 body does not name edit_config_forbidden: %s", rec.Body.String())
	}

	rec = postBeispielStop(router, cookie, csrf, `{"remove":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("stop without remove for mqtt_config user: status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBeispielStopRejectsGet(t *testing.T) {
	dataDir := t.TempDir()
	store := settings.NewStore(dataDir)
	router, _ := newBeispielTestRouter(t, dataDir, store, &fakeNodeSim{})
	cookie, _ := loginBeispiel(t, router, "admin", "secret")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/beispiel/stop", nil)
	req.AddCookie(cookie)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status %d, want 405: %s", rec.Code, rec.Body.String())
	}
}

func TestBeispielStopTurnsSimulationOffAndReturnsEmptyLists(t *testing.T) {
	dataDir := t.TempDir()
	store := settings.NewStore(dataDir)
	mqttCfg, err := store.LoadMQTT()
	if err != nil {
		t.Fatal(err)
	}
	mqttCfg.SimulationActive = true
	if err := store.SaveMQTT(mqttCfg); err != nil {
		t.Fatal(err)
	}
	sim := &fakeNodeSim{}
	router, _ := newBeispielTestRouter(t, dataDir, store, sim)
	cookie, csrf := loginBeispiel(t, router, "admin", "secret")

	rec := postBeispielStop(router, cookie, csrf, `{"remove":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"removed":[],"kept":[]}` {
		t.Fatalf("body %s, want empty arrays", got)
	}
	if !sim.called || sim.lastActive {
		t.Fatalf("PublishNodeSimulation(false) not called (called=%v active=%v)", sim.called, sim.lastActive)
	}
	stored, _ := store.LoadMQTT()
	if stored.SimulationActive {
		t.Fatal("SimulationActive still on after stop")
	}
}
