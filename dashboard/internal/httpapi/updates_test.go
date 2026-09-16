package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/updatecheck"
)

func githubReleaseServer(t *testing.T, tagName string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": tagName, "html_url": "https://example.invalid/release"})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestUpdatesRoutesAreGuestReachableButRequireASession(t *testing.T) {
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	checker := &updatecheck.Checker{Repo: "Developer-Simon/energy-node", BaseURL: githubReleaseServer(t, "v1.4.2").URL}
	cache := &updatecheck.Cache{}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{
		Auth: manager, Version: "1.4.1", UpdatesChecker: checker, UpdatesCache: cache,
	})

	anonymous := httptest.NewRecorder()
	router.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/api/v1/updates/status", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401: %s", anonymous.Code, anonymous.Body.String())
	}

	guest := httptest.NewRecorder()
	router.ServeHTTP(guest, httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", nil))
	if guest.Code != http.StatusOK {
		t.Fatalf("guest login status %d: %s", guest.Code, guest.Body.String())
	}
	guestCookie := guest.Result().Cookies()[0]

	statusBeforeCheck := httptest.NewRequest(http.MethodGet, "/api/v1/updates/status", nil)
	statusBeforeCheck.AddCookie(guestCookie)
	before := httptest.NewRecorder()
	router.ServeHTTP(before, statusBeforeCheck)
	if before.Code != http.StatusOK {
		t.Fatalf("guest status read = %d, want 200: %s", before.Code, before.Body.String())
	}
	var beforePayload struct {
		Checked bool `json:"checked"`
	}
	if err := json.NewDecoder(before.Body).Decode(&beforePayload); err != nil {
		t.Fatal(err)
	}
	if beforePayload.Checked {
		t.Fatal("expected no cached result before the first check")
	}

	checkRequest := httptest.NewRequest(http.MethodGet, "/api/v1/updates/check", nil)
	checkRequest.AddCookie(guestCookie)
	checkResponse := httptest.NewRecorder()
	router.ServeHTTP(checkResponse, checkRequest)
	if checkResponse.Code != http.StatusOK {
		t.Fatalf("guest triggered check = %d, want 200 (RoleCheckUpdates is a guest default): %s", checkResponse.Code, checkResponse.Body.String())
	}
	var result updatecheck.Result
	if err := json.NewDecoder(checkResponse.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if !result.Available || result.Latest != "1.4.2" {
		t.Fatalf("unexpected check result: %+v", result)
	}

	statusAfterCheck := httptest.NewRequest(http.MethodGet, "/api/v1/updates/status", nil)
	statusAfterCheck.AddCookie(guestCookie)
	after := httptest.NewRecorder()
	router.ServeHTTP(after, statusAfterCheck)
	var afterResult updatecheck.Result
	if err := json.NewDecoder(after.Body).Decode(&afterResult); err != nil {
		t.Fatal(err)
	}
	if !afterResult.Available {
		t.Fatalf("status after check should reflect the cached result: %+v", afterResult)
	}
}

func TestUpdatesRoutesAreAbsentWithoutACache(t *testing.T) {
	// Without RouterDependencies.UpdatesCache, NewRouterWithDependencies
	// never registers the /api/v1/updates/ routes at all -- the request
	// falls through to the SPA shell's "/" catch-all, same as any other
	// unmapped path.
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: manager})

	guest := httptest.NewRecorder()
	router.ServeHTTP(guest, httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", nil))
	guestCookie := guest.Result().Cookies()[0]

	request := httptest.NewRequest(http.MethodGet, "/api/v1/updates/status", nil)
	request.AddCookie(guestCookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var probe struct {
		Checked *bool `json:"checked"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &probe); err == nil && probe.Checked != nil {
		t.Fatalf("expected the SPA shell, not the updates handler, to answer this path: %s", response.Body.String())
	}
}
