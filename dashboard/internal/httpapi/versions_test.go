package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func versionsRouter(t *testing.T, stateDir string) (http.Handler, *http.Cookie) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{
		Auth: manager, Version: "v0.7.5", InstallerStateDir: stateDir,
	})
	guest := httptest.NewRecorder()
	router.ServeHTTP(guest, httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", nil))
	if guest.Code != http.StatusOK {
		t.Fatalf("guest login = %d: %s", guest.Code, guest.Body.String())
	}
	return router, guest.Result().Cookies()[0]
}

func getWith(router http.Handler, cookie *http.Cookie, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func writeState(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVersionsRoutesNeedASessionButNotARole(t *testing.T) {
	dir := t.TempDir()
	writeState(t, dir, "installed-manifest.json", `{"version":"v0.7.5","components":{"dashboard":"v0.7.5"}}`)
	writeState(t, dir, "changelog.json", `{"schema_version":1,"bundle_version":"v0.7.5","components":[{"id":"dashboard","label":"Dashboard","kind":"app","version":"v0.7.5","releases":[]}]}`)
	router, guest := versionsRouter(t, dir)

	for _, path := range []string{"/api/v1/versions", "/api/v1/changelog"} {
		if rec := getWith(router, nil, path); rec.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s = %d, want 401", path, rec.Code)
		}
		if rec := getWith(router, guest, path); rec.Code != http.StatusOK {
			t.Fatalf("guest %s = %d, want 200: %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestVersionsReportsTheBundleAndTheRunningDashboard(t *testing.T) {
	dir := t.TempDir()
	writeState(t, dir, "installed-manifest.json", `{"version":"v0.7.4","arch":"armv6","components":{"dashboard":"v0.7.4"}}`)
	router, guest := versionsRouter(t, dir)

	rec := getWith(router, guest, "/api/v1/versions")
	var body struct {
		Bundle  struct{ Version, Arch string } `json:"bundle"`
		Running struct{ Dashboard string }     `json:"running"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Bundle.Version != "v0.7.4" || body.Bundle.Arch != "armv6" || body.Running.Dashboard != "v0.7.5" {
		t.Fatalf("body = %+v: the page compares bundle and running to spot a self-update", body)
	}
}

func TestVersionsOnAnUninstalledNodeIsAnEmptySnapshotNotAnError(t *testing.T) {
	router, guest := versionsRouter(t, t.TempDir())
	rec := getWith(router, guest, "/api/v1/versions")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Bundle     *struct{}         `json:"bundle"`
		Components []json.RawMessage `json:"components"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Bundle != nil || body.Components == nil || len(body.Components) != 0 {
		t.Fatalf("body = %+v, want bundle null and components []", body)
	}
}

func TestChangelogFiltersByComponentAndReports404WithoutAFile(t *testing.T) {
	dir := t.TempDir()
	router, guest := versionsRouter(t, dir)
	if rec := getWith(router, guest, "/api/v1/changelog"); rec.Code != http.StatusNotFound {
		t.Fatalf("no file: status = %d, want 404", rec.Code)
	}

	writeState(t, dir, "changelog.json", `{"schema_version":1,"components":[{"id":"dashboard"},{"id":"service:shelly"}]}`)
	rec := getWith(router, guest, "/api/v1/changelog?component=service:shelly")
	var body struct {
		Components []struct {
			ID string `json:"id"`
		} `json:"components"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Components) != 1 || body.Components[0].ID != "service:shelly" {
		t.Fatalf("components = %+v", body.Components)
	}
}

func TestVersionsRoutesRejectWrites(t *testing.T) {
	router, guest := versionsRouter(t, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/versions", nil)
	req.AddCookie(guest)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusForbidden {
		t.Fatalf("POST status = %d, want 405 (or 403 from the CSRF guard)", rec.Code)
	}
}
