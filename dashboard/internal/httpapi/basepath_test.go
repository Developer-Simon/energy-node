package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/basepath"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// The router keeps its 47 root-relative mux patterns; basepath.Middleware in
// front of it is what makes /node/api/v1/... reach /api/v1/... .
func TestRouterServesAPIBehindAForwardedPrefix(t *testing.T) {
	handler := basepath.Middleware(NewRouter(registry.New(), nil, nil))

	request := httptest.NewRequest(http.MethodGet, "/node/api/v1/health", nil)
	request.Header.Set("X-Forwarded-Prefix", "/node")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("prefixed health status %d: %s", recorder.Code, recorder.Body.String())
	}

	// Proxies that use "proxy_pass http://host:8080/" already strip the
	// prefix themselves; the header alone must not break those.
	strippedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	strippedRequest.Header.Set("X-Forwarded-Prefix", "/node")
	strippedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(strippedRecorder, strippedRequest)
	if strippedRecorder.Code != http.StatusOK {
		t.Fatalf("pre-stripped health status %d: %s", strippedRecorder.Code, strippedRecorder.Body.String())
	}
}

func TestRouterServesStaticAssetsBehindAForwardedPrefix(t *testing.T) {
	handler := basepath.Middleware(NewRouter(registry.New(), nil, nil))
	request := httptest.NewRequest(http.MethodGet, "/node/static/js/dashboard.js", nil)
	request.Header.Set("X-Forwarded-Prefix", "/node")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("prefixed asset status %d", recorder.Code)
	}
}

// A session cookie scoped to "/" would be sent to (and could be overwritten
// by) every other app on the proxy host, and would not come back for /node/.
func TestSessionCookiesAreScopedToTheForwardedPrefix(t *testing.T) {
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	handler := basepath.Middleware(NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: manager}))

	loginRequest := httptest.NewRequest(http.MethodPost, "/node/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRequest.Header.Set("X-Forwarded-Proto", "https")
	loginRequest.Header.Set("X-Forwarded-Prefix", "/node")
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusOK {
		t.Fatalf("login status %d: %s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Path != "/node/" {
		t.Fatalf("login cookies = %+v, want a single cookie with Path=/node/", cookies)
	}
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(login.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "/node/api/v1/auth/logout", nil)
	logoutRequest.AddCookie(cookies[0])
	logoutRequest.Header.Set("X-Forwarded-Proto", "https")
	logoutRequest.Header.Set("X-Forwarded-Prefix", "/node")
	logoutRequest.Header.Set("X-CSRF-Token", session.CSRFToken)
	logout := httptest.NewRecorder()
	handler.ServeHTTP(logout, logoutRequest)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout status %d: %s", logout.Code, logout.Body.String())
	}
	cleared := logout.Result().Cookies()
	// The clearing cookie has to repeat the Path, or the browser keeps the
	// /node/ one alive next to the new /-scoped empty value.
	if len(cleared) != 1 || cleared[0].Path != "/node/" || cleared[0].Value != "" {
		t.Fatalf("logout cookies = %+v, want a single cleared cookie with Path=/node/", cleared)
	}
}

func TestSessionCookiesStayRootScopedWithoutAPrefix(t *testing.T) {
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	handler := basepath.Middleware(NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: manager}))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("guest login status %d: %s", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Path != "/" {
		t.Fatalf("guest cookies = %+v, want a single cookie with Path=/", cookies)
	}
}
