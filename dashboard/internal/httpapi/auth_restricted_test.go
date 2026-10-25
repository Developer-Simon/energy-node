package httpapi

import (
	"encoding/json"
	"fmt"
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

const restrictedTestPassword = "http-login-pass-1"

type restrictedSessionBody struct {
	Restricted    bool     `json:"restricted"`
	SystemActions bool     `json:"system_actions"`
	Withheld      []string `json:"withheld"`
	Roles         []string `json:"roles"`
}

func newRestrictedTestRouter(t *testing.T, redeploy http.Handler) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: manager, Redeploy: redeploy})
	return router, manager
}

func createRestrictedTestAccount(t *testing.T, manager *auth.Manager, username string, roles []string) {
	t.Helper()
	if _, err := manager.CreateUser("admin", username, restrictedTestPassword, roles); err != nil {
		t.Fatalf("create %s: %v", username, err)
	}
}

// loginOverHTTP logs in without X-Forwarded-Proto and returns the cookie the
// server set; that cookie carries the unsecure name.
func loginOverHTTP(t *testing.T, router http.Handler, username string) *http.Cookie {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, restrictedTestPassword)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP login of %s status %d: %s", username, recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName(false) {
		t.Fatalf("HTTP login cookies = %+v, want one %s cookie", cookies, sessionCookieName(false))
	}
	return cookies[0]
}

// TestHTTPLoginWithHTTPLoginRoleGetsRestrictedSession: a richtiges Passwort
// über HTTP mit der Berechtigung http_login ergibt eine eingeschränkte
// Sitzung. system_actions fällt weg und steht in withheld.
func TestHTTPLoginWithHTTPLoginRoleGetsRestrictedSession(t *testing.T) {
	router, manager := newRestrictedTestRouter(t, nil)
	createRestrictedTestAccount(t, manager, "henry", []string{auth.RoleHTTPLogin, auth.RoleSystemActions})

	body := fmt.Sprintf(`{"username":"henry","password":%q}`, restrictedTestPassword)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP login status %d: %s", recorder.Code, recorder.Body.String())
	}
	var session restrictedSessionBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if !session.Restricted {
		t.Fatalf("HTTP session is not restricted: %s", recorder.Body.String())
	}
	if session.SystemActions {
		t.Fatalf("system_actions is still effective over HTTP: %s", recorder.Body.String())
	}
	if !containsString(session.Withheld, auth.RoleSystemActions) {
		t.Fatalf("withheld = %v, want %s", session.Withheld, auth.RoleSystemActions)
	}
}

// TestHTTPLoginWithoutRoleIsForbidden: das Passwort stimmt, aber das Konto hat
// http_login nicht. Das ist 403 mit secure_login_required, nicht 401.
func TestHTTPLoginWithoutRoleIsForbidden(t *testing.T) {
	router, manager := newRestrictedTestRouter(t, nil)
	createRestrictedTestAccount(t, manager, "ingrid", []string{auth.RoleViewDevices})

	body := fmt.Sprintf(`{"username":"ingrid","password":%q}`, restrictedTestPassword)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"secure_login_required"`) {
		t.Fatalf("status %d, want 403 secure_login_required: %s", recorder.Code, recorder.Body.String())
	}
}

// TestRestrictedSessionCannotReachRedeployOverHTTPS: die HTTP-Sitzung eines
// Kontos mit system_actions wird über HTTPS an /redeploy/ geschickt. Die
// Sitzung ist eingeschränkt, also fehlt system_actions im Kontext.
func TestRestrictedSessionCannotReachRedeployOverHTTPS(t *testing.T) {
	reached := false
	redeploy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	router, manager := newRestrictedTestRouter(t, redeploy)
	createRestrictedTestAccount(t, manager, "henry", []string{auth.RoleHTTPLogin, auth.RoleSystemActions})
	httpCookie := loginOverHTTP(t, router, "henry")

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		request := httptest.NewRequest(method, "/redeploy/", nil)
		request.AddCookie(&http.Cookie{Name: secureSessionCookie, Value: httpCookie.Value})
		request.Header.Set("X-Forwarded-Proto", "https")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"system_actions_forbidden"`) {
			t.Fatalf("restricted %s /redeploy/ over HTTPS status %d: %s", method, recorder.Code, recorder.Body.String())
		}
	}
	if reached {
		t.Fatal("a restricted session reached the redeploy handler over HTTPS")
	}
}

// TestHTTPSessionWithSystemActionsIsNoLongerRejected: die Sitzung eines Kontos
// mit system_actions über HTTP wird nicht mehr mit 403 abgewiesen, sondern
// als eingeschränkte Sitzung beantwortet.
func TestHTTPSessionWithSystemActionsIsNoLongerRejected(t *testing.T) {
	router, manager := newRestrictedTestRouter(t, nil)
	createRestrictedTestAccount(t, manager, "henry", []string{auth.RoleHTTPLogin, auth.RoleSystemActions})
	httpCookie := loginOverHTTP(t, router, "henry")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	request.AddCookie(httpCookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP session status %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var session restrictedSessionBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if !session.Restricted || session.SystemActions {
		t.Fatalf("HTTP session = %+v, want restricted without system_actions", session)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
