package httpapi

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/companion"
	"github.com/Developer-Simon/energy-node-dashboard/internal/companionpair"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

const (
	companionTestFingerprint = "abababababababababababababababababababababababababababababababab"
	companionTestNonce       = "1111111111111111111111111111111111111111111111111111111111111111"
)

// newCompanionTestRouter builds the panel router with a manager and a hub. The
// hub is the verifier of the manager, as in production.
func newCompanionTestRouter(t *testing.T) (http.Handler, *auth.Manager, *companion.Hub) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	hub := companion.NewHub()
	manager.SetHAVerifier(hub)
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{
		Auth:                 manager,
		Companion:            hub,
		CompanionFingerprint: companionTestFingerprint,
	})
	return router, manager, hub
}

// companionCall sends one request, with the bearer token when token is not empty.
func companionCall(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// wantCompanionError checks the status and the error code of a response.
func wantCompanionError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status %d, want %d: %s", recorder.Code, status, recorder.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != code {
		t.Fatalf("error code %q, want %q", body.Code, code)
	}
}

// pairCompanion runs the pairing over the gate and returns the token.
func pairCompanion(t *testing.T, gate http.Handler, manager *auth.Manager) string {
	t.Helper()
	code, _, err := manager.CreatePairingCode("admin")
	if err != nil {
		t.Fatal(err)
	}
	proof := companionpair.ClientProof(companionpair.Normalize(code), companionTestFingerprint, companionTestNonce)
	body := fmt.Sprintf(`{"nonce":%q,"proof":%q}`, companionTestNonce, proof)
	recorder := companionCall(gate, http.MethodPost, "/api/v1/companion/pair", body, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("pair status %d: %s", recorder.Code, recorder.Body.String())
	}
	var answer struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&answer); err != nil {
		t.Fatal(err)
	}
	return answer.Token
}

// readVerifyEvent reads the SSE stream until a verify event and returns its id.
func readVerifyEvent(t *testing.T, body io.Reader) (id, username string) {
	t.Helper()
	scanner := bufio.NewScanner(body)
	event := ""
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: ") && event == "verify":
			var request struct {
				ID       string `json:"id"`
				Username string `json:"username"`
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &request); err != nil {
				t.Fatal(err)
			}
			return request.ID, request.Username
		}
	}
	t.Fatalf("no verify event: %v", scanner.Err())
	return "", ""
}

func TestCompanionRoutesAre404OnMainPort(t *testing.T) {
	router, _, _ := newCompanionTestRouter(t)
	for _, probe := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/companion/pair"},
		{http.MethodGet, "/api/v1/companion/stream"},
		{http.MethodPost, "/api/v1/companion/verify/0123456789abcdef"},
		{http.MethodPost, "/api/v1/companion/session"},
		{http.MethodPost, "/api/v1/companion/user-changed"},
	} {
		recorder := companionCall(router, probe.method, probe.path, `{}`, "")
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s %s on the main port: status %d, want 404", probe.method, probe.path, recorder.Code)
		}
	}
}

func TestCompanionGateRejectsMissingOrWrongToken(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	gate := CompanionGate(manager, router)

	wantCompanionError(t, companionCall(gate, http.MethodGet, "/api/v1/companion/stream", "", ""), http.StatusUnauthorized, "companion_unauthorized")
	wantCompanionError(t, companionCall(gate, http.MethodPost, "/api/v1/companion/session", `{}`, "x"), http.StatusUnauthorized, "companion_unauthorized")

	token := pairCompanion(t, gate, manager)
	wantCompanionError(t, companionCall(gate, http.MethodGet, "/api/v1/companion/stream", "", "deadbeef"), http.StatusUnauthorized, "companion_unauthorized")
	wantCompanionError(t, companionCall(gate, http.MethodGet, "/api/v1/companion/stream", "", ""), http.StatusUnauthorized, "companion_unauthorized")
	if !manager.CompanionTokenValid(token) {
		t.Fatal("paired token is not valid")
	}
}

func TestPairOverGateReturnsTokenAndServerProof(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	gate := CompanionGate(manager, router)
	code, _, err := manager.CreatePairingCode("admin")
	if err != nil {
		t.Fatal(err)
	}
	proof := companionpair.ClientProof(companionpair.Normalize(code), companionTestFingerprint, companionTestNonce)
	body := fmt.Sprintf(`{"nonce":%q,"proof":%q}`, companionTestNonce, proof)
	recorder := companionCall(gate, http.MethodPost, "/api/v1/companion/pair", body, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("pair status %d: %s", recorder.Code, recorder.Body.String())
	}
	var answer struct {
		Token string `json:"token"`
		Proof string `json:"proof"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&answer); err != nil {
		t.Fatal(err)
	}
	want := companionpair.ServerProof(companionpair.Normalize(code), companionTestFingerprint, companionTestNonce, answer.Token)
	if answer.Proof != want {
		t.Fatalf("server proof = %q, want %q", answer.Proof, want)
	}
	if !manager.CompanionTokenValid(answer.Token) {
		t.Fatal("returned token is not valid")
	}
}

func TestPairWithProofForOtherFingerprintFails(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	gate := CompanionGate(manager, router)
	code, _, err := manager.CreatePairingCode("admin")
	if err != nil {
		t.Fatal(err)
	}
	wrong := companionpair.ClientProof(companionpair.Normalize(code), strings.Repeat("cd", 32), companionTestNonce)
	body := fmt.Sprintf(`{"nonce":%q,"proof":%q}`, companionTestNonce, wrong)
	wantCompanionError(t, companionCall(gate, http.MethodPost, "/api/v1/companion/pair", body, ""), http.StatusForbidden, "pairing_invalid")

	// The code is still open after one wrong proof, so the right proof pairs.
	right := companionpair.ClientProof(companionpair.Normalize(code), companionTestFingerprint, companionTestNonce)
	body = fmt.Sprintf(`{"nonce":%q,"proof":%q}`, companionTestNonce, right)
	if recorder := companionCall(gate, http.MethodPost, "/api/v1/companion/pair", body, ""); recorder.Code != http.StatusOK {
		t.Fatalf("pair after a wrong proof: status %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestCompanionSessionForDisabledAccount(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	gate := CompanionGate(manager, router)
	token := pairCompanion(t, gate, manager)
	body := `{"ha_user_id":"ha-anna","username":"anna","name":"Anna","is_admin":false}`

	recorder := companionCall(gate, http.MethodPost, "/api/v1/companion/session", body, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("session status %d: %s", recorder.Code, recorder.Body.String())
	}
	if _, err := manager.UpdateHAAccount("admin", "ha-anna", nil, true, false); err != nil {
		t.Fatalf("disable account: %v", err)
	}
	wantCompanionError(t, companionCall(gate, http.MethodPost, "/api/v1/companion/session", body, token), http.StatusForbidden, "account_disabled")
	wantCompanionError(t, companionCall(gate, http.MethodPost, "/api/v1/companion/session", `{"ha_user_id":"","username":"anna"}`, token), http.StatusBadRequest, "invalid_body")
}

func TestUnpairClosesStreamAndRejectsOldToken(t *testing.T) {
	router, manager, hub := newCompanionTestRouter(t)
	gate := CompanionGate(manager, router)
	token := pairCompanion(t, gate, manager)

	server := httptest.NewServer(gate)
	t.Cleanup(server.Close)
	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/companion/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stream status %d", response.StatusCode)
	}

	if err := manager.Unpair("admin"); err != nil {
		t.Fatal(err)
	}
	hub.Close()
	// The stream ends, so ReadAll returns; the client timeout guards a hang.
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatalf("stream did not end cleanly: %v", err)
	}
	wantCompanionError(t, companionCall(gate, http.MethodGet, "/api/v1/companion/stream", "", token), http.StatusUnauthorized, "companion_unauthorized")
}

func TestVerifyRouteResolvesPendingRequest(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	gate := CompanionGate(manager, router)
	token := pairCompanion(t, gate, manager)

	server := httptest.NewServer(gate)
	t.Cleanup(server.Close)
	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/companion/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 10 * time.Second}
	stream, err := client.Do(request)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer stream.Body.Close()

	// A panel login of an unknown name asks Home Assistant over the stream.
	login := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		login <- usersCall(router, http.MethodPost, "/api/v1/auth/login", `{"username":"anna","password":"anna-pass"}`, nil, "", true)
	}()
	id, username := readVerifyEvent(t, stream.Body)
	if username != "anna" {
		t.Fatalf("verify username = %q", username)
	}

	answer := `{"ok":true,"ha_user_id":"ha-anna","username":"anna","name":"Anna","is_admin":false}`
	if recorder := companionCall(gate, http.MethodPost, "/api/v1/companion/verify/"+id, answer, token); recorder.Code != http.StatusNoContent {
		t.Fatalf("verify status %d: %s", recorder.Code, recorder.Body.String())
	}
	select {
	case recorder := <-login:
		if recorder.Code != http.StatusOK {
			t.Fatalf("login status %d: %s", recorder.Code, recorder.Body.String())
		}
		var session struct {
			Source string `json:"source"`
		}
		if err := json.NewDecoder(recorder.Body).Decode(&session); err != nil {
			t.Fatal(err)
		}
		if session.Source != auth.SourceHomeAssistant {
			t.Fatalf("login source = %q, want %q", session.Source, auth.SourceHomeAssistant)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("login did not finish after the verify answer")
	}
	found := false
	for _, account := range manager.HAAccounts() {
		found = found || account.HAUserID == "ha-anna"
	}
	if !found {
		t.Fatal("home assistant account was not stored")
	}
	wantCompanionError(t, companionCall(gate, http.MethodPost, "/api/v1/companion/verify/"+id, answer, token), http.StatusNotFound, "verify_unknown")
}

func TestUserChangedRouteDropsSessions(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	gate := CompanionGate(manager, router)
	token := pairCompanion(t, gate, manager)
	identity := `"ha_user_id":"ha-anna","username":"anna","name":"Anna","is_admin":false`
	sessionToken := companionSessionToken(t, gate, token, "{"+identity+"}")

	if recorder := authSessionCall(gate, token, sessionToken, "https"); recorder.Code != http.StatusOK {
		t.Fatalf("session before the change: status %d: %s", recorder.Code, recorder.Body.String())
	}
	change := companionCall(gate, http.MethodPost, "/api/v1/companion/user-changed", "{"+identity+`,"active":false,"removed":false}`, token)
	if change.Code != http.StatusNoContent {
		t.Fatalf("user-changed status %d: %s", change.Code, change.Body.String())
	}
	wantCompanionError(t, authSessionCall(gate, token, sessionToken, "https"), http.StatusUnauthorized, "authentication_required")
}

// companionSessionToken starts a companion session over the gate and returns its token.
func companionSessionToken(t *testing.T, gate http.Handler, pairToken, body string) string {
	t.Helper()
	recorder := companionCall(gate, http.MethodPost, "/api/v1/companion/session", body, pairToken)
	if recorder.Code != http.StatusOK {
		t.Fatalf("session status %d: %s", recorder.Code, recorder.Body.String())
	}
	var answer struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&answer); err != nil {
		t.Fatal(err)
	}
	return answer.Token
}

// authSessionCall reads GET /api/v1/auth/session with the panel cookie of the
// session token. pairToken is the bearer token for the gate ("" on the main
// port). proto is the X-Forwarded-Proto value ("" sends none); the cookie name
// follows the security of the request, as the panel reads it.
func authSessionCall(handler http.Handler, pairToken, sessionToken, proto string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	if proto != "" {
		request.Header.Set("X-Forwarded-Proto", proto)
	}
	if pairToken != "" {
		request.Header.Set("Authorization", "Bearer "+pairToken)
	}
	request.AddCookie(&http.Cookie{Name: sessionCookieName(proto == "https"), Value: sessionToken})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestCompanionGateTreatsMissingForwardedProtoAsInsecure(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	token := pairCompanion(t, CompanionGate(manager, router), manager)
	var seen *http.Request
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = r })
	gate := CompanionGate(manager, inner)
	for _, proto := range []string{"", "http", "http, https"} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/companion/stream", nil)
		request.TLS = &tls.ConnectionState{}
		request.Header.Set("Authorization", "Bearer "+token)
		if proto != "" {
			request.Header.Set("X-Forwarded-Proto", proto)
		}
		gate.ServeHTTP(httptest.NewRecorder(), request)
		if seen == nil || seen.TLS != nil {
			t.Fatalf("X-Forwarded-Proto %q: clone keeps TLS, want insecure", proto)
		}
	}
	for _, proto := range []string{"https", "https, http"} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/companion/stream", nil)
		request.TLS = &tls.ConnectionState{}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-Forwarded-Proto", proto)
		gate.ServeHTTP(httptest.NewRecorder(), request)
		if seen == nil || seen.TLS == nil {
			t.Fatalf("X-Forwarded-Proto %q: clone lost TLS", proto)
		}
	}
}

func TestCompanionGateTreatsForwardedHTTPAsInsecure(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	gate := CompanionGate(manager, router)
	if err := manager.SetHARoles("admin", []string{auth.RoleUserManagement}, nil); err != nil {
		t.Fatal(err)
	}
	token := pairCompanion(t, gate, manager)
	sessionToken := companionSessionToken(t, gate, token, `{"ha_user_id":"ha-root","username":"root","name":"Root","is_admin":true}`)

	for _, tc := range []struct {
		proto string
		want  bool
	}{{"http", false}, {"https", true}} {
		recorder := authSessionCall(gate, token, sessionToken, tc.proto)
		if recorder.Code != http.StatusOK {
			t.Fatalf("X-Forwarded-Proto %s: status %d: %s", tc.proto, recorder.Code, recorder.Body.String())
		}
		var body struct {
			UserManagement bool     `json:"user_management"`
			Withheld       []string `json:"withheld"`
		}
		if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.UserManagement != tc.want {
			t.Fatalf("X-Forwarded-Proto %s: user_management = %v, want %v", tc.proto, body.UserManagement, tc.want)
		}
		withheld := false
		for _, key := range body.Withheld {
			withheld = withheld || key == auth.RoleUserManagement
		}
		if withheld == tc.want {
			t.Fatalf("X-Forwarded-Proto %s: withheld = %v, user_management withheld want %v", tc.proto, body.Withheld, !tc.want)
		}
	}
}

func TestCompanionSessionIsRejectedOutsideCompanionPort(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	gate := CompanionGate(manager, router)
	token := pairCompanion(t, gate, manager)
	sessionToken := companionSessionToken(t, gate, token, `{"ha_user_id":"ha-anna","username":"anna","name":"Anna","is_admin":false}`)

	wantCompanionError(t, authSessionCall(router, "", sessionToken, "https"), http.StatusUnauthorized, "authentication_required")

	recorder := authSessionCall(gate, token, sessionToken, "https")
	if recorder.Code != http.StatusOK {
		t.Fatalf("over the gate: status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Via    string `json:"via"`
		Source string `json:"source"`
		Name   string `json:"display_name"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Via != auth.ViaCompanion || body.Source != auth.SourceHomeAssistant || body.Name != "Anna" {
		t.Fatalf("session answer = %+v, want via companion, source homeassistant, display_name Anna", body)
	}
}

func TestLoginReportsHAUnavailable(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	gate := CompanionGate(manager, router)
	pairCompanion(t, gate, manager)

	// Paired, but no companion stream is open, so Home Assistant cannot answer.
	recorder := usersCall(router, http.MethodPost, "/api/v1/auth/login", `{"username":"anna","password":"anna-pass"}`, nil, "", true)
	wantCompanionError(t, recorder, http.StatusServiceUnavailable, "ha_unavailable")
}
