package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
