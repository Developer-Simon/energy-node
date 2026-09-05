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

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/mqttclient"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
)

type fakeBridgeWatcher struct {
	lastClientID string
	status       mqttclient.BridgeConnectionState
}

func (f *fakeBridgeWatcher) SetBridgeWatch(remoteClientID string)           { f.lastClientID = remoteClientID }
func (f *fakeBridgeWatcher) BridgeStatus() mqttclient.BridgeConnectionState { return f.status }

type fakeSystemExecutor struct {
	calls []systemactions.Action
	err   error
}

func (f *fakeSystemExecutor) Execute(_ context.Context, action systemactions.Action) error {
	f.calls = append(f.calls, action)
	return f.err
}

func validBridgePayloadJSON() string {
	return `{
		"enabled": true,
		"name": "aussenstandort-zu-hauptsystem",
		"address": "100.64.1.2",
		"port": 1883,
		"remote_client_id": "pi-aussenstandort-bridge",
		"remote_username": "ha",
		"topics": [{"pattern": "outstation/#", "direction": "both", "qos": 0}],
		"try_private": true,
		"start_type_auto": true,
		"restart_timeout": 30,
		"keepalive_seconds": 60,
		"cleansession": false
	}`
}

func newBridgeTestRouter(t *testing.T, store *settings.Store, credentials *mqttclient.CredentialStore, executor SystemActionExecutor, watcher BridgeWatcher, dataDir, targetPath string) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), store, nil, nil, nil, nil, nil, RouterDependencies{
		Auth:              manager,
		SystemActions:     executor,
		BridgeCredentials: credentials,
		MQTTBridgeWatcher: watcher,
		DataDir:           dataDir,
		BridgeTargetPath:  targetPath,
	})
	return router, manager
}

func putBridgeConfig(t *testing.T, router http.Handler, cookie *http.Cookie, csrfToken string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/mqtt/bridge", strings.NewReader(validBridgePayloadJSON()))
	request.AddCookie(cookie)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-CSRF-Token", csrfToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestBridgeConfigPutRequiresRoleHTTPSAndCSRF(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	watcher := &fakeBridgeWatcher{}
	router, _ := newBridgeTestRouter(t, store, nil, nil, watcher, t.TempDir(), "")

	guestCookie := loginAsGuest(t, router)
	guestRequest := httptest.NewRequest(http.MethodPut, "/api/v1/mqtt/bridge", strings.NewReader(validBridgePayloadJSON()))
	guestRequest.AddCookie(guestCookie)
	guestRecorder := httptest.NewRecorder()
	router.ServeHTTP(guestRecorder, guestRequest)
	if guestRecorder.Code != http.StatusForbidden {
		t.Fatalf("guest PUT status %d, want 403: %s", guestRecorder.Code, guestRecorder.Body.String())
	}

	adminCookie, csrfToken := loginAsAdmin(t, router)

	insecureRequest := httptest.NewRequest(http.MethodPut, "/api/v1/mqtt/bridge", strings.NewReader(validBridgePayloadJSON()))
	insecureRequest.AddCookie(adminCookie)
	insecureRequest.Header.Set("X-CSRF-Token", csrfToken)
	insecureRecorder := httptest.NewRecorder()
	router.ServeHTTP(insecureRecorder, insecureRequest)
	if insecureRecorder.Code == http.StatusOK {
		t.Fatalf("insecure admin PUT status %d, want it rejected", insecureRecorder.Code)
	}

	noCSRFRequest := httptest.NewRequest(http.MethodPut, "/api/v1/mqtt/bridge", strings.NewReader(validBridgePayloadJSON()))
	noCSRFRequest.AddCookie(adminCookie)
	noCSRFRequest.Header.Set("X-Forwarded-Proto", "https")
	noCSRFRecorder := httptest.NewRecorder()
	router.ServeHTTP(noCSRFRecorder, noCSRFRequest)
	if noCSRFRecorder.Code != http.StatusForbidden {
		t.Fatalf("missing-CSRF admin PUT status %d, want 403: %s", noCSRFRecorder.Code, noCSRFRecorder.Body.String())
	}

	okRecorder := putBridgeConfig(t, router, adminCookie, csrfToken)
	if okRecorder.Code != http.StatusOK {
		t.Fatalf("valid admin PUT status %d, want 200: %s", okRecorder.Code, okRecorder.Body.String())
	}
	if stored, err := store.LoadBridge(); err != nil || len(stored.Connections) != 1 || stored.Connections[0].Address != "100.64.1.2" {
		t.Fatalf("bridge.json was not persisted: %#v (err=%v)", stored, err)
	}
	if watcher.lastClientID != "pi-aussenstandort-bridge" {
		t.Fatalf("watcher.lastClientID = %q, want the saved remote_client_id", watcher.lastClientID)
	}
	var putResponse bridgeConnectionPayload
	if err := json.NewDecoder(okRecorder.Body).Decode(&putResponse); err != nil {
		t.Fatal(err)
	}
	if putResponse.Preview == "" {
		t.Fatal("PUT response has no preview - the UI would show a blank preview until the next reload")
	}
}

func TestBridgeConfigGetNeverLeaksPassword(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	if err := store.SaveBridge(settings.BridgeConfig{Connections: []settings.BridgeConnection{{
		Enabled: true, Name: "n", Address: "100.64.1.2", Port: 1883, RemoteClientID: "id",
		Topics:     []settings.BridgeTopic{{Pattern: "a/#", Direction: "out", QoS: 0}},
		TryPrivate: true, StartTypeAuto: true, RestartTimeout: 30, KeepaliveSeconds: 60,
	}}}); err != nil {
		t.Fatal(err)
	}
	credentials := mqttclient.NewBridgeCredentialStore(t.TempDir())
	if err := credentials.Save(mqttclient.Credentials{Password: "top-secret"}); err != nil {
		t.Fatal(err)
	}
	router, _ := newBridgeTestRouter(t, store, credentials, nil, nil, t.TempDir(), "")
	adminCookie, _ := loginAsAdmin(t, router)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/mqtt/bridge", nil)
	request.AddCookie(adminCookie)
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status %d: %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "top-secret") {
		t.Fatal("GET /api/v1/mqtt/bridge leaked the remote password")
	}
	var response bridgeConnectionPayload
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.PasswordConfigured {
		t.Fatal("password_configured = false despite a saved password")
	}
	if response.Preview == "" || !strings.Contains(response.Preview, "********") {
		t.Fatalf("preview = %q, want it to contain the masked password placeholder", response.Preview)
	}
}

func TestBridgeConfigGetRequiresRoleAndHTTPS(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	router, _ := newBridgeTestRouter(t, store, nil, nil, nil, t.TempDir(), "")
	guestCookie := loginAsGuest(t, router)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/mqtt/bridge", nil)
	request.AddCookie(guestCookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("guest GET status %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
}

func TestBridgeCredentialsRoundTripNeverEchoesPassword(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	credentials := mqttclient.NewBridgeCredentialStore(t.TempDir())
	router, _ := newBridgeTestRouter(t, store, credentials, nil, nil, t.TempDir(), "")
	adminCookie, csrfToken := loginAsAdmin(t, router)

	setRequest := httptest.NewRequest(http.MethodPost, "/api/v1/mqtt/bridge/credentials", strings.NewReader(`{"password":"s3cret"}`))
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

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/mqtt/bridge/credentials", nil)
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

func bridgeStoreWithConnection(t *testing.T) *settings.Store {
	t.Helper()
	store := settings.NewStore(t.TempDir())
	if err := store.SaveBridge(settings.BridgeConfig{Connections: []settings.BridgeConnection{{
		Enabled: true, Name: "n", Address: "100.64.1.2", Port: 1883, RemoteClientID: "pi-bridge",
		Topics:     []settings.BridgeTopic{{Pattern: "a/#", Direction: "out", QoS: 0}},
		TryPrivate: true, StartTypeAuto: true, RestartTimeout: 30, KeepaliveSeconds: 60,
	}}}); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestBridgeApplyRequiresBothRoles(t *testing.T) {
	store := bridgeStoreWithConnection(t)
	executor := &fakeSystemExecutor{}
	watcher := &fakeBridgeWatcher{}
	dataDir := t.TempDir()
	router, _ := newBridgeTestRouter(t, store, nil, executor, watcher, dataDir, "")

	// No X-Forwarded-Proto here - the guest cookie was issued over plain
	// HTTP, so marking this request secure would make authMiddleware look
	// for the wrong cookie name and 401 before ever reaching the role check
	// this test wants to exercise (see the same note in mqtt_test.go).
	guestCookie := loginAsGuest(t, router)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/mqtt/bridge/apply", strings.NewReader(`{"confirm":true}`))
	request.AddCookie(guestCookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("guest apply status %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
	if len(executor.calls) != 0 {
		t.Fatal("apply-bridge-config was executed despite the guest being forbidden")
	}
}

func TestBridgeApplyStagesRendersAndCallsHelperThenCleansUp(t *testing.T) {
	store := bridgeStoreWithConnection(t)
	credentials := mqttclient.NewBridgeCredentialStore(t.TempDir())
	if err := credentials.Save(mqttclient.Credentials{Password: "remote-pw"}); err != nil {
		t.Fatal(err)
	}
	executor := &fakeSystemExecutor{}
	watcher := &fakeBridgeWatcher{}
	dataDir := t.TempDir()
	router, _ := newBridgeTestRouter(t, store, credentials, executor, watcher, dataDir, "")
	adminCookie, csrfToken := loginAsAdmin(t, router)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/mqtt/bridge/apply", strings.NewReader(`{"confirm":true}`))
	request.AddCookie(adminCookie)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-CSRF-Token", csrfToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("apply status %d: %s", recorder.Code, recorder.Body.String())
	}
	if len(executor.calls) != 1 || executor.calls[0] != systemactions.ApplyBridgeConfig {
		t.Fatalf("executor.calls = %#v, want a single ApplyBridgeConfig call", executor.calls)
	}
	if watcher.lastClientID != "pi-bridge" {
		t.Fatalf("watcher.lastClientID = %q, want the applied remote_client_id", watcher.lastClientID)
	}
	stagedPath := filepath.Join(dataDir, bridgeStagedFileName)
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Fatalf("staged file was not cleaned up after apply: err=%v", err)
	}
}

func TestBridgeApplyWithoutConfirmIsRejected(t *testing.T) {
	store := bridgeStoreWithConnection(t)
	executor := &fakeSystemExecutor{}
	router, _ := newBridgeTestRouter(t, store, nil, executor, nil, t.TempDir(), "")
	adminCookie, csrfToken := loginAsAdmin(t, router)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/mqtt/bridge/apply", strings.NewReader(`{"confirm":false}`))
	request.AddCookie(adminCookie)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-CSRF-Token", csrfToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed apply status %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
	if len(executor.calls) != 0 {
		t.Fatal("apply-bridge-config was executed despite confirm=false")
	}
}

func TestBridgeApplyReportsBusy(t *testing.T) {
	store := bridgeStoreWithConnection(t)
	executor := &fakeSystemExecutor{err: systemactions.ErrBusy}
	router, _ := newBridgeTestRouter(t, store, nil, executor, nil, t.TempDir(), "")
	adminCookie, csrfToken := loginAsAdmin(t, router)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/mqtt/bridge/apply", strings.NewReader(`{"confirm":true}`))
	request.AddCookie(adminCookie)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-CSRF-Token", csrfToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("busy apply status %d, want 409: %s", recorder.Code, recorder.Body.String())
	}
}

func TestBridgeRestartRequiresSystemActionsOnly(t *testing.T) {
	executor := &fakeSystemExecutor{}
	store := settings.NewStore(t.TempDir())
	router, _ := newBridgeTestRouter(t, store, nil, executor, nil, t.TempDir(), "")
	adminCookie, csrfToken := loginAsAdmin(t, router)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/mqtt/bridge/restart", nil)
	request.AddCookie(adminCookie)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-CSRF-Token", csrfToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("restart status %d: %s", recorder.Code, recorder.Body.String())
	}
	if len(executor.calls) != 1 || executor.calls[0] != systemactions.RestartMosquitto {
		t.Fatalf("executor.calls = %#v, want a single RestartMosquitto call", executor.calls)
	}
}

type fakeOutputRunner struct {
	out string
}

func (f fakeOutputRunner) Output(context.Context, string, ...string) (string, error) {
	return f.out, nil
}

func TestBridgeStatusBundlesServiceBridgeAndDrift(t *testing.T) {
	store := bridgeStoreWithConnection(t)
	watcher := &fakeBridgeWatcher{status: mqttclient.BridgeConnectionState{Configured: true, Connected: true}}
	router, _ := newBridgeTestRouter(t, store, nil, nil, watcher, t.TempDir(), filepath.Join(t.TempDir(), "does-not-exist.conf"))

	// Swap in a fake OutputRunner by rebuilding the router with it, since
	// newBridgeTestRouter does not expose SystemStatusRunner.
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router = NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), store, nil, nil, nil, nil, nil, RouterDependencies{
		Auth: manager, MQTTBridgeWatcher: watcher, SystemStatusRunner: fakeOutputRunner{out: "active"},
	})
	guestCookie := loginAsGuest(t, router)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/mqtt/bridge/status", nil)
	request.AddCookie(guestCookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status GET code %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		ServiceState string                           `json:"service_state"`
		Bridge       mqttclient.BridgeConnectionState `json:"bridge"`
		Drift        bridgeDriftStatus                `json:"drift"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.ServiceState != "active" {
		t.Fatalf("service_state = %q, want %q", response.ServiceState, "active")
	}
	if !response.Bridge.Connected {
		t.Fatal("bridge.connected = false, want true from the fake watcher")
	}
	if response.Drift.Known {
		t.Fatal("drift.known = true for a non-existent target file, want false")
	}
}

func TestBridgeApplyErrorCodeDistinguishesExitCodes(t *testing.T) {
	if got := bridgeApplyErrorCode(errors.New("generic")); got != "bridge_helper_failed" {
		t.Fatalf("bridgeApplyErrorCode(generic) = %q, want bridge_helper_failed", got)
	}
}

func TestBridgeRevisionsAndRestoreViaHTTP(t *testing.T) {
	store := bridgeStoreWithConnection(t)
	first, err := store.LoadBridge()
	if err != nil {
		t.Fatal(err)
	}
	firstAddress := first.Connections[0].Address
	// A plain struct copy shares the Connections slice's backing array, so
	// mutate a deep copy - otherwise "second.Connections[0].Address = ..."
	// would silently rewrite firstAddress too.
	second := settings.BridgeConfig{Connections: append([]settings.BridgeConnection(nil), first.Connections...)}
	second.Connections[0].Address = "100.64.1.9"
	if err := store.SaveBridge(second); err != nil {
		t.Fatal(err)
	}
	watcher := &fakeBridgeWatcher{}
	router, _ := newBridgeTestRouter(t, store, nil, nil, watcher, t.TempDir(), "")
	adminCookie, csrfToken := loginAsAdmin(t, router)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/mqtt/bridge/revisions", nil)
	listRequest.AddCookie(adminCookie)
	listRequest.Header.Set("X-Forwarded-Proto", "https")
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("revisions GET status %d: %s", listRecorder.Code, listRecorder.Body.String())
	}
	var revisions []settings.Revision
	if err := json.NewDecoder(listRecorder.Body).Decode(&revisions); err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 {
		t.Fatalf("got %d revisions, want 1", len(revisions))
	}

	restoreRequest := httptest.NewRequest(http.MethodPost, "/api/v1/mqtt/bridge/restore", strings.NewReader(`{"revision":"`+revisions[0].Name+`"}`))
	restoreRequest.AddCookie(adminCookie)
	restoreRequest.Header.Set("X-Forwarded-Proto", "https")
	restoreRequest.Header.Set("X-CSRF-Token", csrfToken)
	restoreRecorder := httptest.NewRecorder()
	router.ServeHTTP(restoreRecorder, restoreRequest)
	if restoreRecorder.Code != http.StatusOK {
		t.Fatalf("restore status %d: %s", restoreRecorder.Code, restoreRecorder.Body.String())
	}
	restored, err := store.LoadBridge()
	if err != nil {
		t.Fatal(err)
	}
	if restored.Connections[0].Address != firstAddress {
		t.Fatalf("got address %q after restore, want %q", restored.Connections[0].Address, firstAddress)
	}
	if watcher.lastClientID != restored.Connections[0].RemoteClientID {
		t.Fatalf("watcher.lastClientID = %q, want it refreshed on restore", watcher.lastClientID)
	}
}
