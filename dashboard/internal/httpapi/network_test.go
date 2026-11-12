package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/netmgr"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// networkRunner answers the helper verbs of the netmgr client. Each verb has
// its own output or error; calls records every helper invocation.
type networkRunner struct {
	status  string
	scan    error
	connect error
	forget  error
	calls   []string
}

func (r *networkRunner) Output(_ context.Context, _ string, args ...string) (string, error) {
	verb := args[2]
	r.calls = append(r.calls, verb)
	switch verb {
	case "network-status":
		return r.status, nil
	case "wifi-scan":
		return "", r.scan
	case "wifi-connect":
		return "", r.connect
	case "wifi-forget":
		return "", r.forget
	}
	return "", os.ErrInvalid
}

func (r *networkRunner) called(verb string) bool {
	for _, call := range r.calls {
		if call == verb {
			return true
		}
	}
	return false
}

// networkExitError returns the *exec.ExitError a helper exit code produces.
func networkExitError(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit "+strconv.Itoa(code)).Run()
	if err == nil {
		t.Fatalf("exit %d produced no error", code)
	}
	return err
}

func networkFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "netmgr", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func newNetworkTestRouter(t *testing.T, runner *networkRunner, stageDir string) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	client := netmgr.NewClient(runner, "/usr/local/bin/energy-node-dashboard-system-action", stageDir)
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{
		Auth:    manager,
		Network: client,
	})
	return router, manager
}

// networkRequest sends a request to the network routes. The session value
// goes under the cookie name of the request's scheme, as a browser would send
// it, so a plain HTTP request is authenticated and then fails the HTTPS gate.
func networkRequest(router http.Handler, method, path, body string, cookie *http.Cookie, csrf string, https bool) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if cookie != nil {
		request.AddCookie(&http.Cookie{Name: sessionCookieName(https), Value: cookie.Value})
	}
	if https {
		request.Header.Set("X-Forwarded-Proto", "https")
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func networkErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v: %s", err, recorder.Body.String())
	}
	return body.Code
}

func expectNetworkStatus(t *testing.T, recorder *httptest.ResponseRecorder, want int, code string) {
	t.Helper()
	if recorder.Code != want {
		t.Fatalf("status %d, want %d: %s", recorder.Code, want, recorder.Body.String())
	}
	if code != "" {
		if got := networkErrorCode(t, recorder); got != code {
			t.Fatalf("error code %q, want %q", got, code)
		}
	}
}

func TestNetworkStatusRequiresNetworkRole(t *testing.T) {
	runner := &networkRunner{status: networkFixture(t, "both.txt")}
	router, _ := newNetworkTestRouter(t, runner, t.TempDir())

	guestCookie := loginAsGuest(t, router)
	recorder := networkRequest(router, http.MethodGet, "/api/v1/network/status", "", guestCookie, "", true)
	expectNetworkStatus(t, recorder, http.StatusForbidden, "network_forbidden")
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", recorder.Header().Get("Cache-Control"))
	}
	if runner.called("network-status") {
		t.Fatal("network-status ran for a user without the network role")
	}
}

func TestNetworkStatusReturnsStatusWithoutPassword(t *testing.T) {
	runner := &networkRunner{status: networkFixture(t, "both.txt")}
	router, _ := newNetworkTestRouter(t, runner, t.TempDir())
	adminCookie, _ := loginAsAdmin(t, router)

	recorder := networkRequest(router, http.MethodGet, "/api/v1/network/status", "", adminCookie, "", true)
	expectNetworkStatus(t, recorder, http.StatusOK, "")
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", recorder.Header().Get("Cache-Control"))
	}
	if strings.Contains(strings.ToLower(recorder.Body.String()), "password") {
		t.Fatalf("status body names a password: %s", recorder.Body.String())
	}
	var status netmgr.Status
	if err := json.NewDecoder(recorder.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if !status.Managed || len(status.Networks) == 0 {
		t.Fatalf("status = %+v, want managed with networks", status)
	}
}

// TestNetworkWriteRoutesRequireHTTPS calls the handlers with the admin in the
// context. The account logs in over HTTPS only, so the router cannot serve a
// plain HTTP admin session; the gate order is what this test checks.
func TestNetworkWriteRoutesRequireHTTPS(t *testing.T) {
	runner := &networkRunner{status: networkFixture(t, "both.txt")}
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	client := netmgr.NewClient(runner, "/usr/local/bin/energy-node-dashboard-system-action", t.TempDir())
	var admin auth.User
	for _, user := range manager.Users() {
		if user.Username == "admin" {
			admin = user
		}
	}
	cases := []struct {
		path    string
		body    string
		handler http.HandlerFunc
	}{
		{"scan", "", handleNetworkScan(manager, client)},
		{"connect", `{"ssid":"Nachbar","password":"geheimes-wlan"}`, handleNetworkConnect(manager, client)},
		{"forget", `{"profile_uuid":"6f0d6b6e-3c1a-4c4e-9a52-1d2f3e4a5b6c"}`, handleNetworkForget(manager, client)},
	}
	for _, tc := range cases {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/network/wifi/"+tc.path, strings.NewReader(tc.body))
		request = request.WithContext(auth.WithUser(request.Context(), admin))
		recorder := httptest.NewRecorder()
		tc.handler(recorder, request)
		expectNetworkStatus(t, recorder, http.StatusForbidden, "secure_connection_required")
		if len(runner.calls) != 0 {
			t.Fatalf("%s: helper ran without HTTPS: %v", tc.path, runner.calls)
		}
	}
}

func TestNetworkWriteRoutesRequireCSRF(t *testing.T) {
	cases := []struct {
		path string
		body string
	}{
		{"/api/v1/network/wifi/scan", ""},
		{"/api/v1/network/wifi/connect", `{"ssid":"Nachbar","password":"geheimes-wlan"}`},
		{"/api/v1/network/wifi/forget", `{"profile_uuid":"6f0d6b6e-3c1a-4c4e-9a52-1d2f3e4a5b6c"}`},
	}
	for _, tc := range cases {
		runner := &networkRunner{status: networkFixture(t, "both.txt")}
		router, _ := newNetworkTestRouter(t, runner, t.TempDir())
		adminCookie, _ := loginAsAdmin(t, router)

		recorder := networkRequest(router, http.MethodPost, tc.path, tc.body, adminCookie, "", true)
		expectNetworkStatus(t, recorder, http.StatusForbidden, "csrf_failed")
		if len(runner.calls) != 0 {
			t.Fatalf("%s: helper ran without CSRF token: %v", tc.path, runner.calls)
		}
	}
}

func TestNetworkWriteRoutesRequireNetworkRole(t *testing.T) {
	runner := &networkRunner{status: networkFixture(t, "both.txt")}
	router, _ := newNetworkTestRouter(t, runner, t.TempDir())
	guestCookie := loginAsGuest(t, router)

	recorder := networkRequest(router, http.MethodPost, "/api/v1/network/wifi/connect", `{"ssid":"Nachbar","password":"geheimes-wlan"}`, guestCookie, "", true)
	expectNetworkStatus(t, recorder, http.StatusForbidden, "network_forbidden")
	if len(runner.calls) != 0 {
		t.Fatalf("helper ran for a guest: %v", runner.calls)
	}
}

func TestNetworkScanUnmanagedReturnsConflict(t *testing.T) {
	runner := &networkRunner{status: networkFixture(t, "unmanaged.txt")}
	router, _ := newNetworkTestRouter(t, runner, t.TempDir())
	adminCookie, csrf := loginAsAdmin(t, router)

	recorder := networkRequest(router, http.MethodPost, "/api/v1/network/wifi/scan", "", adminCookie, csrf, true)
	expectNetworkStatus(t, recorder, http.StatusConflict, "network_unmanaged")
	if runner.called("wifi-scan") {
		t.Fatal("wifi-scan ran on an unmanaged network")
	}
}

func TestNetworkScanHelperExitUnmanagedReturnsConflict(t *testing.T) {
	runner := &networkRunner{status: networkFixture(t, "both.txt"), scan: networkExitError(t, 69)}
	router, _ := newNetworkTestRouter(t, runner, t.TempDir())
	adminCookie, csrf := loginAsAdmin(t, router)

	recorder := networkRequest(router, http.MethodPost, "/api/v1/network/wifi/scan", "", adminCookie, csrf, true)
	expectNetworkStatus(t, recorder, http.StatusConflict, "network_unmanaged")
}

func TestNetworkScanStartsAndReturnsAccepted(t *testing.T) {
	runner := &networkRunner{status: networkFixture(t, "both.txt")}
	router, _ := newNetworkTestRouter(t, runner, t.TempDir())
	adminCookie, csrf := loginAsAdmin(t, router)

	recorder := networkRequest(router, http.MethodPost, "/api/v1/network/wifi/scan", "", adminCookie, csrf, true)
	expectNetworkStatus(t, recorder, http.StatusAccepted, "")
	if !runner.called("wifi-scan") {
		t.Fatal("wifi-scan was not started")
	}
}

func TestNetworkConnectBusyReturnsConflict(t *testing.T) {
	runner := &networkRunner{status: networkFixture(t, "both.txt"), connect: networkExitError(t, 75)}
	router, _ := newNetworkTestRouter(t, runner, t.TempDir())
	adminCookie, csrf := loginAsAdmin(t, router)

	recorder := networkRequest(router, http.MethodPost, "/api/v1/network/wifi/connect", `{"ssid":"Nachbar","password":"geheimes-wlan"}`, adminCookie, csrf, true)
	expectNetworkStatus(t, recorder, http.StatusConflict, "network_busy")
}

func TestNetworkConnectDoesNotEchoPassword(t *testing.T) {
	stageDir := t.TempDir()
	runner := &networkRunner{status: networkFixture(t, "both.txt")}
	router, _ := newNetworkTestRouter(t, runner, stageDir)
	adminCookie, csrf := loginAsAdmin(t, router)

	recorder := networkRequest(router, http.MethodPost, "/api/v1/network/wifi/connect", `{"ssid":"Nachbar","password":"geheimes-wlan"}`, adminCookie, csrf, true)
	expectNetworkStatus(t, recorder, http.StatusAccepted, "")
	if strings.Contains(recorder.Body.String(), "geheimes-wlan") {
		t.Fatalf("connect response echoes the password: %s", recorder.Body.String())
	}
	if !runner.called("wifi-connect") {
		t.Fatal("wifi-connect was not started")
	}
	staged, err := os.ReadFile(filepath.Join(stageDir, "network-connect.staged.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(staged), `"security":"WPA2"`) {
		t.Fatalf("staged request %s does not carry the scanned security of Nachbar", staged)
	}
}

func TestNetworkConnectRejectsInvalidRequest(t *testing.T) {
	runner := &networkRunner{status: networkFixture(t, "both.txt")}
	router, _ := newNetworkTestRouter(t, runner, t.TempDir())
	adminCookie, csrf := loginAsAdmin(t, router)

	recorder := networkRequest(router, http.MethodPost, "/api/v1/network/wifi/connect", `{"ssid":"","password":"geheimes-wlan"}`, adminCookie, csrf, true)
	expectNetworkStatus(t, recorder, http.StatusBadRequest, "network_rejected")
	if runner.called("wifi-connect") {
		t.Fatal("wifi-connect ran for an invalid request")
	}
}

func TestNetworkForgetReturnsProfile(t *testing.T) {
	const uuid = "6f0d6b6e-3c1a-4c4e-9a52-1d2f3e4a5b6c"
	runner := &networkRunner{status: networkFixture(t, "both.txt")}
	router, _ := newNetworkTestRouter(t, runner, t.TempDir())
	adminCookie, csrf := loginAsAdmin(t, router)

	recorder := networkRequest(router, http.MethodPost, "/api/v1/network/wifi/forget", `{"profile_uuid":"`+uuid+`"}`, adminCookie, csrf, true)
	expectNetworkStatus(t, recorder, http.StatusOK, "")
	var body struct {
		ProfileUUID string `json:"profile_uuid"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.ProfileUUID != uuid {
		t.Fatalf("profile_uuid = %q, want %q", body.ProfileUUID, uuid)
	}
	if !runner.called("wifi-forget") {
		t.Fatal("wifi-forget was not started")
	}
}

func TestNetworkForgetRejectsNonUUID(t *testing.T) {
	runner := &networkRunner{status: networkFixture(t, "both.txt")}
	router, _ := newNetworkTestRouter(t, runner, t.TempDir())
	adminCookie, csrf := loginAsAdmin(t, router)

	recorder := networkRequest(router, http.MethodPost, "/api/v1/network/wifi/forget", `{"profile_uuid":"uuid-1"}`, adminCookie, csrf, true)
	expectNetworkStatus(t, recorder, http.StatusBadRequest, "network_rejected")
	if runner.called("wifi-forget") {
		t.Fatal("wifi-forget ran for a non-UUID")
	}
}
