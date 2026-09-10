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
	"github.com/Developer-Simon/energy-node-dashboard/internal/mqttclient"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func testBase() mqttclient.Config {
	return mqttclient.Config{
		Host:              "127.0.0.1",
		Port:              "1883",
		Username:          "energynode_client",
		Password:          "geheim",
		ClientID:          "energy-node-dashboard",
		CleanSession:      true,
		KeepaliveSeconds:  30,
		ConnectTimeoutSec: 10,
		DiscoveryPrefix:   "homeassistant",
		Source:            "config",
	}
}

func TestResolveMQTTConfigUsesConfigWithoutStoredSettings(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	cfg, source, err := ResolveMQTTConfig(store, nil, testBase())
	if err != nil {
		t.Fatalf("ResolveMQTTConfig: %v", err)
	}
	if source != "config" || cfg.Source != "config" {
		t.Fatalf("source = %q / %q, erwartet \"config\"", source, cfg.Source)
	}
	if cfg.Host != "127.0.0.1" || cfg.Username != "energynode_client" || cfg.Password != "geheim" {
		t.Fatalf("Basis nicht uebernommen: %+v", cfg)
	}
}

func TestResolveMQTTConfigStoredSettingsWinCompletely(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	stored := settings.DefaultMQTT()
	stored.Enabled = true
	stored.Host = "broker.local"
	stored.Port = 8883
	stored.Username = "aus-settings"
	if err := store.SaveMQTT(stored); err != nil {
		t.Fatalf("SaveMQTT: %v", err)
	}
	cfg, source, err := ResolveMQTTConfig(store, nil, testBase())
	if err != nil {
		t.Fatalf("ResolveMQTTConfig: %v", err)
	}
	if source != "settings" || cfg.Host != "broker.local" || cfg.Username != "aus-settings" {
		t.Fatalf("settings gewinnen nicht vollstaendig: %+v (source %q)", cfg, source)
	}
}

func newMQTTTestRouter(t *testing.T, store *settings.Store, credentials *mqttclient.CredentialStore, reconfigurer MQTTReconfigurer, statusProvider mqttclient.StatusProvider) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), store, nil, nil, nil, nil, nil, RouterDependencies{
		Auth: manager, MQTT: statusProvider, MQTTReconfigure: reconfigurer, MQTTCredentials: credentials,
	})
	return router, manager
}

func loginAsAdmin(t *testing.T, router http.Handler) (*http.Cookie, string) {
	t.Helper()
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRequest.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, loginRequest)
	if recorder.Code != http.StatusOK {
		t.Fatalf("admin login status %d: %s", recorder.Code, recorder.Body.String())
	}
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	return recorder.Result().Cookies()[0], session.CSRFToken
}

func loginAsGuest(t *testing.T, router http.Handler) *http.Cookie {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("guest login status %d: %s", recorder.Code, recorder.Body.String())
	}
	return recorder.Result().Cookies()[0]
}

func TestMQTTConfigPutRequiresRoleHTTPSAndCSRF(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	router, _ := newMQTTTestRouter(t, store, nil, nil, nil)
	body := `{"enabled":true,"host":"broker","port":1883,"client_id":"dashboard","tls":false,"tls_insecure":false,"keepalive_seconds":30,"clean_session":true,"discovery_prefix":"homeassistant","connect_timeout_seconds":10}`

	// No X-Forwarded-Proto here: the guest cookie was issued over plain HTTP
	// (sessionCookieName picks the cookie by the *current* request's
	// scheme), so marking this request "secure" would just make
	// authMiddleware look for the wrong cookie name and 401 before ever
	// reaching the role check this test wants to exercise.
	guestCookie := loginAsGuest(t, router)
	guestRequest := httptest.NewRequest(http.MethodPut, "/api/v1/mqtt", strings.NewReader(body))
	guestRequest.AddCookie(guestCookie)
	guestRecorder := httptest.NewRecorder()
	router.ServeHTTP(guestRecorder, guestRequest)
	if guestRecorder.Code != http.StatusForbidden {
		t.Fatalf("guest PUT status %d, want 403: %s", guestRecorder.Code, guestRecorder.Body.String())
	}

	adminCookie, csrfToken := loginAsAdmin(t, router)

	// The bootstrap admin also holds system_actions, and authMiddleware
	// refuses to even recognise that cookie on a request that isn't marked
	// secure (sessionCookieName picks a different cookie name for insecure
	// requests) - so this never reaches handleMQTTConfig's own HTTPS check;
	// it 401s one layer up. Either way, HTTP is blocked end to end, which is
	// what this assertion actually cares about.
	insecureRequest := httptest.NewRequest(http.MethodPut, "/api/v1/mqtt", strings.NewReader(body))
	insecureRequest.AddCookie(adminCookie)
	insecureRequest.Header.Set("X-CSRF-Token", csrfToken)
	insecureRecorder := httptest.NewRecorder()
	router.ServeHTTP(insecureRecorder, insecureRequest)
	if insecureRecorder.Code == http.StatusOK {
		t.Fatalf("insecure admin PUT status %d, want it rejected", insecureRecorder.Code)
	}

	noCSRFRequest := httptest.NewRequest(http.MethodPut, "/api/v1/mqtt", strings.NewReader(body))
	noCSRFRequest.AddCookie(adminCookie)
	noCSRFRequest.Header.Set("X-Forwarded-Proto", "https")
	noCSRFRecorder := httptest.NewRecorder()
	router.ServeHTTP(noCSRFRecorder, noCSRFRequest)
	if noCSRFRecorder.Code != http.StatusForbidden {
		t.Fatalf("missing-CSRF admin PUT status %d, want 403: %s", noCSRFRecorder.Code, noCSRFRecorder.Body.String())
	}

	okRequest := httptest.NewRequest(http.MethodPut, "/api/v1/mqtt", strings.NewReader(body))
	okRequest.AddCookie(adminCookie)
	okRequest.Header.Set("X-Forwarded-Proto", "https")
	okRequest.Header.Set("X-CSRF-Token", csrfToken)
	okRecorder := httptest.NewRecorder()
	router.ServeHTTP(okRecorder, okRequest)
	if okRecorder.Code != http.StatusOK {
		t.Fatalf("valid admin PUT status %d, want 200: %s", okRecorder.Code, okRecorder.Body.String())
	}
	if stored, err := store.LoadMQTT(); err != nil || stored.Host != "broker" {
		t.Fatalf("mqtt.json was not persisted: %#v (err=%v)", stored, err)
	}
}

func TestMQTTConfigGetNeverLeaksPassword(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	credentials := mqttclient.NewCredentialStore(t.TempDir())
	if err := credentials.Save(mqttclient.Credentials{Password: "top-secret"}); err != nil {
		t.Fatal(err)
	}
	router, _ := newMQTTTestRouter(t, store, credentials, nil, nil)
	guestCookie := loginAsGuest(t, router)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/mqtt", nil)
	request.AddCookie(guestCookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status %d: %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "top-secret") {
		t.Fatal("GET /api/v1/mqtt leaked the broker password")
	}
	var response mqttConfigResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.PasswordConfigured {
		t.Fatal("password_configured = false despite a saved password")
	}
}

func TestMQTTCredentialsRoundTripNeverEchoesPassword(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	credentials := mqttclient.NewCredentialStore(t.TempDir())
	router, _ := newMQTTTestRouter(t, store, credentials, nil, nil)
	adminCookie, csrfToken := loginAsAdmin(t, router)

	setRequest := httptest.NewRequest(http.MethodPost, "/api/v1/mqtt/credentials", strings.NewReader(`{"password":"s3cret"}`))
	setRequest.AddCookie(adminCookie)
	setRequest.Header.Set("X-Forwarded-Proto", "https")
	setRequest.Header.Set("X-CSRF-Token", csrfToken)
	setRecorder := httptest.NewRecorder()
	router.ServeHTTP(setRecorder, setRequest)
	if setRecorder.Code != http.StatusOK || strings.Contains(setRecorder.Body.String(), "s3cret") {
		t.Fatalf("set credentials status %d, body %q", setRecorder.Code, setRecorder.Body.String())
	}
	if creds, err := credentials.Load(); err != nil || creds.Password != "s3cret" {
		t.Fatalf("credentials not persisted: %#v (err=%v)", creds, err)
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/mqtt/credentials", nil)
	deleteRequest.AddCookie(adminCookie)
	deleteRequest.Header.Set("X-Forwarded-Proto", "https")
	deleteRequest.Header.Set("X-CSRF-Token", csrfToken)
	deleteRecorder := httptest.NewRecorder()
	router.ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete credentials status %d: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	if _, err := credentials.Load(); err == nil {
		t.Fatal("credentials still present after DELETE")
	}
}

func TestMQTTTestEndpointReportsFailureWithoutTouchingProductionConnection(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	router, _ := newMQTTTestRouter(t, store, nil, nil, nil)
	adminCookie, csrfToken := loginAsAdmin(t, router)

	// Port 1 on loopback is reserved and nothing listens there, so this
	// fails fast (connection refused) instead of waiting out a timeout.
	request := httptest.NewRequest(http.MethodPost, "/api/v1/mqtt/test", strings.NewReader(`{"host":"127.0.0.1","port":1,"client_id":"dashboard","connect_timeout_seconds":2}`))
	request.AddCookie(adminCookie)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-CSRF-Token", csrfToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("test endpoint status %d: %s", recorder.Code, recorder.Body.String())
	}
	var result mqttclient.TestResult
	if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.OK {
		t.Fatal("expected the connection test against a closed port to fail")
	}
	if result.ErrorCode == "" {
		t.Fatal("expected a non-empty error_code")
	}
}

type fakeMQTTReconfigurer struct {
	gotConfig mqttclient.Config
	err       error
}

func (f *fakeMQTTReconfigurer) Reconfigure(cfg mqttclient.Config) error {
	f.gotConfig = cfg
	return f.err
}

func TestMQTTReconnectRequiresRoleAndAppliesResolvedConfig(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	if err := store.SaveMQTT(settings.MQTTConfig{
		Enabled: true, Host: "settings-broker", Port: 1883, ClientID: "dashboard",
		KeepaliveSeconds: 30, CleanSession: true, DiscoveryPrefix: "homeassistant", ConnectTimeoutSec: 10,
	}); err != nil {
		t.Fatal(err)
	}
	reconfigurer := &fakeMQTTReconfigurer{}
	statusProvider := &fakeMQTTStatusProvider{status: mqttclient.Status{Connected: true, BrokerAddress: "settings-broker:1883"}}
	router, _ := newMQTTTestRouter(t, store, nil, reconfigurer, statusProvider)

	guestCookie := loginAsGuest(t, router)
	guestRequest := httptest.NewRequest(http.MethodPost, "/api/v1/mqtt/reconnect", nil)
	guestRequest.AddCookie(guestCookie)
	guestRecorder := httptest.NewRecorder()
	router.ServeHTTP(guestRecorder, guestRequest)
	if guestRecorder.Code != http.StatusForbidden {
		t.Fatalf("guest reconnect status %d, want 403: %s", guestRecorder.Code, guestRecorder.Body.String())
	}
	if reconfigurer.gotConfig.Host != "" {
		t.Fatal("Reconfigure was called despite the guest being forbidden")
	}

	adminCookie, csrfToken := loginAsAdmin(t, router)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/mqtt/reconnect", nil)
	request.AddCookie(adminCookie)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-CSRF-Token", csrfToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("admin reconnect status %d: %s", recorder.Code, recorder.Body.String())
	}
	if reconfigurer.gotConfig.Host != "settings-broker" {
		t.Fatalf("Reconfigure got %#v, want the stored settings-broker config", reconfigurer.gotConfig)
	}
}

func TestMQTTStatusReturnsProviderStatus(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	statusProvider := &fakeMQTTStatusProvider{status: mqttclient.Status{Connected: true, BrokerAddress: "broker:1883", Source: "settings"}}
	router, _ := newMQTTTestRouter(t, store, nil, nil, statusProvider)
	guestCookie := loginAsGuest(t, router)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/mqtt/status", nil)
	request.AddCookie(guestCookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status GET code %d: %s", recorder.Code, recorder.Body.String())
	}
	var status mqttclient.Status
	if err := json.NewDecoder(recorder.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if !status.Connected || status.BrokerAddress != "broker:1883" {
		t.Fatalf("got status %#v, want the fake provider's status echoed back", status)
	}
}

func TestResolveMQTTConfigCarriesAvailabilityTopic(t *testing.T) {
	base := testBase()
	base.AvailabilityTopic = "outstation/energy_node/status/online"

	// Zweig "config" (nichts gespeichert)
	cfg, _, err := ResolveMQTTConfig(settings.NewStore(t.TempDir()), nil, base)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AvailabilityTopic != base.AvailabilityTopic {
		t.Fatalf("config branch dropped AvailabilityTopic: %q", cfg.AvailabilityTopic)
	}

	// Zweig "settings"
	store := settings.NewStore(t.TempDir())
	stored := settings.DefaultMQTT()
	stored.Enabled = true
	stored.Host = "broker.local"
	if err := store.SaveMQTT(stored); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = ResolveMQTTConfig(store, nil, base)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AvailabilityTopic != base.AvailabilityTopic {
		t.Fatalf("settings branch dropped AvailabilityTopic: %q", cfg.AvailabilityTopic)
	}
}

func TestMQTTConfigGetIncludesPublishEnergyDevice(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	router, _ := newMQTTTestRouter(t, store, nil, nil, nil)
	guestCookie := loginAsGuest(t, router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mqtt", nil)
	req.AddCookie(guestCookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/mqtt = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got, ok := body["publish_energy_device"]
	if !ok {
		t.Fatalf("response has no publish_energy_device key: %s", rec.Body.String())
	}
	if got != true {
		t.Fatalf("publish_energy_device = %v, want true (the default)", got)
	}
}

func TestMQTTEnergyDevicePUTTogglesIndependentlyOfEnabled(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	router, _ := newMQTTTestRouter(t, store, nil, nil, nil)
	adminCookie, csrf := loginAsAdmin(t, router)

	put := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/mqtt/energy-device", strings.NewReader(body))
		req.AddCookie(adminCookie)
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-CSRF-Token", csrf)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := put(`{"publish_energy_device":false}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT false = %d: %s", rec.Code, rec.Body.String())
	}
	stored, err := store.LoadMQTT()
	if err != nil {
		t.Fatal(err)
	}
	if stored.PublishEnergyDevice {
		t.Fatal("PublishEnergyDevice = true after PUT false")
	}
	if stored.Enabled {
		t.Fatal("toggling the energy device must not enable the dashboard-managed connection")
	}

	if rec := put(`{"publish_energy_device":true}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT true = %d: %s", rec.Code, rec.Body.String())
	}
	stored, err = store.LoadMQTT()
	if err != nil {
		t.Fatal(err)
	}
	if !stored.PublishEnergyDevice {
		t.Fatal("PublishEnergyDevice = false after PUT true")
	}
	if stored.Enabled {
		t.Fatal("still must not be enabled")
	}
}

type fakeNodeSim struct {
	lastActive bool
	called     bool
}

func (f *fakeNodeSim) PublishNodeSimulation(active bool) { f.lastActive = active; f.called = true }

func newNodeSettingsTestRouter(t *testing.T, store *settings.Store, sim NodeSettingsPublisher) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), store, nil, nil, nil, nil, nil, RouterDependencies{
		Auth: manager, NodeSimulation: sim,
	})
	return router, manager
}

func TestMQTTNodeSettingsPublishesSimulation(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	sim := &fakeNodeSim{}
	router, _ := newNodeSettingsTestRouter(t, store, sim)
	adminCookie, csrf := loginAsAdmin(t, router)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mqtt/node-settings", strings.NewReader(`{"simulation_active":true}`))
	req.AddCookie(adminCookie)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-CSRF-Token", csrf)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !sim.called || !sim.lastActive {
		t.Fatalf("PublishNodeSimulation(true) not called (called=%v active=%v)", sim.called, sim.lastActive)
	}
	stored, _ := store.LoadMQTT()
	if !stored.SimulationActive {
		t.Fatal("SimulationActive not persisted")
	}
}

func TestMQTTNodeSettingsSavesMetrics(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	router, _ := newNodeSettingsTestRouter(t, store, &fakeNodeSim{})
	adminCookie, csrf := loginAsAdmin(t, router)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mqtt/node-settings", strings.NewReader(`{"metrics":{"cpu_temp":false}}`))
	req.AddCookie(adminCookie)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-CSRF-Token", csrf)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	stored, _ := store.LoadMQTT()
	if stored.MetricEnabled("cpu_temp") {
		t.Fatal("cpu_temp still enabled")
	}
}

func TestMQTTEnergyDevicePUTRejectsInsecureRequest(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	router, _ := newMQTTTestRouter(t, store, nil, nil, nil)
	guestCookie := loginAsGuest(t, router)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/mqtt/energy-device", strings.NewReader(`{"publish_energy_device":true}`))
	req.AddCookie(guestCookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("guest PUT over plain HTTP unexpectedly succeeded: %d", rec.Code)
	}
}
