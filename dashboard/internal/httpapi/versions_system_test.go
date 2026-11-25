package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/versions"
)

func systemTestSources(t *testing.T) versions.SystemSources {
	t.Helper()
	dir := t.TempDir()
	writeState(t, dir, "status", "Package: caddy\nStatus: hold ok installed\nVersion: 2.6.2-5\n\n")
	writeState(t, dir, "os-release", "NAME=Debian\nVERSION_ID=12\n")
	return versions.SystemSources{DpkgStatus: filepath.Join(dir, "status"), OSRelease: filepath.Join(dir, "os-release"), StateDir: dir}
}

func TestVersionsSystemNeedsEditSetup(t *testing.T) {
	src := systemTestSources(t)
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{
		Auth: manager, InstallerStateDir: src.StateDir, DpkgStatusPath: src.DpkgStatus, OSReleasePath: src.OSRelease,
	})

	guest := getWith(router, loginAsGuest(t, router), "/api/v1/versions")
	if guest.Code != http.StatusOK || strings.Contains(guest.Body.String(), `"system"`) {
		t.Fatalf("guest: %d %s, want 200 without system", guest.Code, guest.Body.String())
	}

	cookie, _ := loginAsAdmin(t, router)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/versions", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"caddy","label":"Caddy","version":"2.6.2"`) {
		t.Fatalf("admin: %d %s", rec.Code, rec.Body.String())
	}
}

func TestVersionsSystemWithoutAccountsIsShown(t *testing.T) {
	src := systemTestSources(t)
	rec := httptest.NewRecorder()
	handleVersions(src.StateDir, "v1.0.0", src)(rec, httptest.NewRequest(http.MethodGet, "/api/v1/versions", nil))
	if !strings.Contains(rec.Body.String(), `"system":[{"id":"os"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
