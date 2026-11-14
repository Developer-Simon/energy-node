package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
)

// companionHTTPAdminSession pairs the companion for the HA admin "root" with
// user_management and returns the gate, the pairing token and the session token.
func companionHTTPAdminSession(t *testing.T, router http.Handler, manager *auth.Manager) (http.Handler, string, string) {
	t.Helper()
	gate := CompanionGate(manager, router)
	if err := manager.SetHARoles("admin", []string{auth.RoleUserManagement}, nil); err != nil {
		t.Fatal(err)
	}
	token := pairCompanion(t, gate, manager)
	sessionToken := companionSessionToken(t, gate, token, `{"ha_user_id":"ha-root","username":"root","name":"Root","is_admin":true}`)
	return gate, token, sessionToken
}

func TestCompanionHTTPOptInGrantsHTTPSRoles(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	gate, token, sessionToken := companionHTTPAdminSession(t, router, manager)
	if err := manager.SetCompanionHTTP("admin", true); err != nil {
		t.Fatal(err)
	}

	recorder := authSessionCall(gate, token, sessionToken, "http")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		UserManagement bool     `json:"user_management"`
		Withheld       []string `json:"withheld"`
		Trusted        bool     `json:"trusted"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.UserManagement {
		t.Fatal("user_management must be granted over HTTP with the opt-in")
	}
	if len(body.Withheld) != 0 {
		t.Fatalf("withheld = %v, want none", body.Withheld)
	}
	if !body.Trusted {
		t.Fatal("trusted must be true over HTTP with the opt-in")
	}
}

func TestCompanionHTTPOptInPassesRequireHTTPS(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	gate, token, sessionToken := companionHTTPAdminSession(t, router, manager)

	call := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-Forwarded-Proto", "http")
		request.AddCookie(&http.Cookie{Name: sessionCookieName(false), Value: sessionToken})
		recorder := httptest.NewRecorder()
		gate.ServeHTTP(recorder, request)
		return recorder
	}

	wantUsersError(t, call(), http.StatusForbidden, "secure_connection_required")

	if err := manager.SetCompanionHTTP("admin", true); err != nil {
		t.Fatal(err)
	}
	if recorder := call(); recorder.Code != http.StatusOK {
		t.Fatalf("with the opt-in: status %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestCompanionHTTPOptInIgnoresPasswordSessions(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	if err := manager.SetCompanionHTTP("admin", true); err != nil {
		t.Fatal(err)
	}
	cookie, _ := sessionFor(t, router, "admin", "secret")

	recorder := usersCall(router, http.MethodGet, "/api/v1/users", "", &http.Cookie{Name: sessionCookieName(false), Value: cookie.Value}, "", false)
	wantUsersError(t, recorder, http.StatusForbidden, "secure_connection_required")
}

func TestRequestTrustedFallsBackWithoutContext(t *testing.T) {
	insecure := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	insecure.Header.Set("X-Forwarded-Proto", "http")
	if requestTrusted(insecure) {
		t.Fatal("HTTP request without context value must not be trusted")
	}

	secure := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	secure.Header.Set("X-Forwarded-Proto", "https")
	if !requestTrusted(secure) {
		t.Fatal("HTTPS request without context value must be trusted")
	}

	trusted := insecure.WithContext(auth.WithTrusted(context.Background(), true))
	if !requestTrusted(trusted) {
		t.Fatal("context value true must win over an HTTP request")
	}
}

// companionHTTPListing reads companion_http and companion_http_editable from
// GET /api/v1/users.
func companionHTTPListing(t *testing.T, recorder *httptest.ResponseRecorder) (bool, bool) {
	t.Helper()
	wantUsersStatus(t, recorder, http.StatusOK)
	var listing struct {
		CompanionHTTP         *bool `json:"companion_http"`
		CompanionHTTPEditable *bool `json:"companion_http_editable"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if listing.CompanionHTTP == nil || listing.CompanionHTTPEditable == nil {
		t.Fatalf("listing lacks the companion HTTP fields: %s", recorder.Body.String())
	}
	return *listing.CompanionHTTP, *listing.CompanionHTTPEditable
}

// TestCompanionHTTPEndpoint: the admin switches the companion HTTP opt-in over
// HTTPS. A missing enabled field is rejected, and the listing reports the switch
// and that it is editable over HTTPS.
func TestCompanionHTTPEndpoint(t *testing.T) {
	router, _ := newRestrictedTestRouter(t, nil)
	adminCookie, adminCSRF := sessionFor(t, router, "admin", usersAdminPassword)

	wantUsersError(t, usersCall(router, http.MethodPut, "/api/v1/users/companion-http", `{}`, adminCookie, adminCSRF, true), http.StatusBadRequest, "invalid_body")

	recorder := usersCall(router, http.MethodPut, "/api/v1/users/companion-http", `{"enabled":true}`, adminCookie, adminCSRF, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	var body struct {
		CompanionHTTP *bool `json:"companion_http"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.CompanionHTTP == nil || !*body.CompanionHTTP {
		t.Fatalf("PUT response companion_http = %v, want true: %s", body.CompanionHTTP, recorder.Body.String())
	}

	on, editable := companionHTTPListing(t, usersCall(router, http.MethodGet, "/api/v1/users", "", adminCookie, "", true))
	if !on || !editable {
		t.Fatalf("listing companion_http = %v, editable = %v, want true, true", on, editable)
	}
}

// TestCompanionHTTPEndpointRefusesTrustedHTTP: a companion session with the
// opt-in is trusted over HTTP, but it may not change the opt-in itself. The
// PUT answers 403 and the switch stays on. The listing over HTTP reports it as
// not editable.
func TestCompanionHTTPEndpointRefusesTrustedHTTP(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	gate, token, sessionToken := companionHTTPAdminSession(t, router, manager)
	if err := manager.SetCompanionHTTP("admin", true); err != nil {
		t.Fatal(err)
	}

	sessionRecorder := companionCall(gate, http.MethodPost, "/api/v1/companion/session", `{"ha_user_id":"ha-root","username":"root","name":"Root","is_admin":true}`, token)
	if sessionRecorder.Code != http.StatusOK {
		t.Fatalf("session status %d: %s", sessionRecorder.Code, sessionRecorder.Body.String())
	}
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(sessionRecorder.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}

	gateCall := func(method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-Forwarded-Proto", "http")
		request.Header.Set("X-CSRF-Token", session.CSRFToken)
		request.AddCookie(&http.Cookie{Name: sessionCookieName(false), Value: sessionToken})
		recorder := httptest.NewRecorder()
		gate.ServeHTTP(recorder, request)
		return recorder
	}

	wantUsersError(t, gateCall(http.MethodPut, "/api/v1/users/companion-http", `{"enabled":false}`), http.StatusForbidden, "secure_connection_required")
	if !manager.CompanionHTTPAllowed() {
		t.Fatal("a trusted HTTP companion session switched its own opt-in off")
	}

	on, editable := companionHTTPListing(t, gateCall(http.MethodGet, "/api/v1/users", ""))
	if !on || editable {
		t.Fatalf("listing over HTTP: companion_http = %v, editable = %v, want true, false", on, editable)
	}
}
