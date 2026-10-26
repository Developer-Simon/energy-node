package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/updatecheck"
	"github.com/Developer-Simon/energy-node-dashboard/internal/updateinstall"
)

// fakeInstaller is an UpdateInstaller whose answers the test sets.
type fakeInstaller struct {
	offered   string
	err       error
	status    updateinstall.Status
	installed int
}

func (f *fakeInstaller) Install(ctx context.Context) (string, error) {
	f.installed++
	if f.err != nil {
		return "", f.err
	}
	return f.offered, nil
}

func (f *fakeInstaller) Status() updateinstall.Status {
	return f.status
}

// newInstallTestRouter builds an authenticated router with the installer and
// an update cache, so /api/v1/updates/install and /api/v1/updates/status are
// registered.
func newInstallTestRouter(t *testing.T, installer UpdateInstaller, redeploy http.Handler) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{
		Auth:            manager,
		Redeploy:        redeploy,
		UpdatesCache:    &updatecheck.Cache{},
		UpdateInstaller: installer,
	})
	return router, manager
}

func postInstall(router http.Handler, cookie *http.Cookie, csrf string, target string, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	request.Header.Set("X-Forwarded-Proto", "https")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestInstallRequiresRoleAndCSRF(t *testing.T) {
	installer := &fakeInstaller{offered: "0.11.0"}
	router, manager := newInstallTestRouter(t, installer, nil)
	installerCookie, csrf := loginWithRoles(t, router, manager, "ivo", []string{auth.RoleInstallUpdates})
	viewerCookie, viewerCSRF := loginWithRoles(t, router, manager, "vera", []string{auth.RoleViewDevices})

	const path = "/api/v1/updates/install"
	if rec := postInstall(router, viewerCookie, viewerCSRF, path, ""); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"install_updates_forbidden"`) {
		t.Fatalf("without install_updates: %d %s", rec.Code, rec.Body.String())
	}
	if rec := postInstall(router, installerCookie, "", path, ""); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"csrf_failed"`) {
		t.Fatalf("without CSRF token: %d %s", rec.Code, rec.Body.String())
	}
	if installer.installed != 0 {
		t.Fatalf("installer ran %d times for refused requests", installer.installed)
	}
	if rec := postInstall(router, installerCookie, csrf, path, ""); rec.Code != http.StatusAccepted {
		t.Fatalf("with role and CSRF: %d %s", rec.Code, rec.Body.String())
	}
}

func TestInstallIgnoresBodyAndQuery(t *testing.T) {
	installer := &fakeInstaller{offered: "0.11.0"}
	router, manager := newInstallTestRouter(t, installer, nil)
	cookie, csrf := loginWithRoles(t, router, manager, "ivo", []string{auth.RoleInstallUpdates})

	rec := postInstall(router, cookie, csrf, "/api/v1/updates/install?version=0.1.0", `{"version":"0.1.0"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Version != "0.11.0" || installer.installed != 1 {
		t.Fatalf("version %q, installed %d times, body %s", body.Version, installer.installed, rec.Body.String())
	}
}

func TestInstallWithoutOfferIsConflict(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{updateinstall.ErrNoUpdate, "no_update_available"},
		{updateinstall.ErrBusy, "update_in_progress"},
	}
	for _, tc := range cases {
		installer := &fakeInstaller{err: tc.err}
		router, manager := newInstallTestRouter(t, installer, nil)
		cookie, csrf := loginWithRoles(t, router, manager, "ivo", []string{auth.RoleInstallUpdates})
		rec := postInstall(router, cookie, csrf, "/api/v1/updates/install", "")
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), fmt.Sprintf(`"code":%q`, tc.code)) {
			t.Fatalf("%v: status %d: %s", tc.err, rec.Code, rec.Body.String())
		}
	}
}

func TestHTTPSessionInstallsButCannotOpenInstaller(t *testing.T) {
	installer := &fakeInstaller{offered: "0.11.0"}
	reached := false
	redeploy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	router, manager := newInstallTestRouter(t, installer, redeploy)
	createRestrictedTestAccount(t, manager, "henry", []string{auth.RoleHTTPLogin, auth.RoleInstallUpdates, auth.RoleUpdateSettings})

	body := fmt.Sprintf(`{"username":"henry","password":%q}`, restrictedTestPassword)
	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	login.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, login)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("HTTP login status %d: %s", loginRec.Code, loginRec.Body.String())
	}
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	httpCookie := loginRec.Result().Cookies()[0]

	install := httptest.NewRequest(http.MethodPost, "/api/v1/updates/install", nil)
	install.AddCookie(httpCookie)
	install.Header.Set("X-CSRF-Token", session.CSRFToken)
	installRec := httptest.NewRecorder()
	router.ServeHTTP(installRec, install)
	if installRec.Code != http.StatusAccepted {
		t.Fatalf("HTTP session install status %d: %s", installRec.Code, installRec.Body.String())
	}

	screen := httptest.NewRequest(http.MethodGet, "/redeploy/", nil)
	screen.AddCookie(httpCookie)
	screenRec := httptest.NewRecorder()
	router.ServeHTTP(screenRec, screen)
	if screenRec.Code != http.StatusForbidden || !strings.Contains(screenRec.Body.String(), `"code":"update_settings_forbidden"`) {
		t.Fatalf("HTTP session GET /redeploy/ status %d: %s", screenRec.Code, screenRec.Body.String())
	}
	if reached {
		t.Fatal("the HTTP session reached the installer screen")
	}
}

func TestProgressShowsPreparing(t *testing.T) {
	installer := &fakeInstaller{status: updateinstall.Status{State: "preparing", Version: "0.11.0"}}
	rec := httptest.NewRecorder()
	handleUpdatesProgress(t.TempDir(), nil, installer)(rec, httptest.NewRequest(http.MethodGet, "/api/v1/updates/progress", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		State   string `json:"state"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.State != "preparing" || body.Version != "0.11.0" {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestProgressPrefersTheNewerFailure(t *testing.T) {
	failedAt := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	failure := updateinstall.Status{State: "fail", Version: "0.11.0", Code: "PREPARE_FAILED", FinishedAt: &failedAt}

	// A job that ended before the installer failed loses to the failure.
	older := t.TempDir()
	writeJobFinishedAt(t, older, failedAt.Add(-time.Hour))
	rec := httptest.NewRecorder()
	handleUpdatesProgress(older, nil, &fakeInstaller{status: failure})(rec, httptest.NewRequest(http.MethodGet, "/api/v1/updates/progress", nil))
	if !strings.Contains(rec.Body.String(), `"state":"fail"`) || !strings.Contains(rec.Body.String(), `"code":"PREPARE_FAILED"`) || !strings.Contains(rec.Body.String(), `"version":"0.11.0"`) {
		t.Fatalf("older job: body = %s", rec.Body.String())
	}

	// A job that ended after the failure wins.
	newer := t.TempDir()
	writeJobFinishedAt(t, newer, failedAt.Add(time.Hour))
	rec = httptest.NewRecorder()
	handleUpdatesProgress(newer, nil, &fakeInstaller{status: failure})(rec, httptest.NewRequest(http.MethodGet, "/api/v1/updates/progress", nil))
	if !strings.Contains(rec.Body.String(), `"state":"ok"`) {
		t.Fatalf("newer job: body = %s", rec.Body.String())
	}
}

// TestProgressJobInFlightBeatsOldInstallerFailure: ein laufender oder wartender
// Job bleibt sichtbar, auch wenn ein älterer Installer-Fehler vorliegt.
func TestProgressJobInFlightBeatsOldInstallerFailure(t *testing.T) {
	failedAt := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	failure := updateinstall.Status{State: "fail", Version: "0.11.0", Code: "PREPARE_FAILED", FinishedAt: &failedAt}

	running := t.TempDir()
	writeState(t, running, "current.json", `{"bundle_version":"v0.10.0","steps":["10","60"],"run_id":"r2"}`)
	writeState(t, running, "log", "1 ##STEP 10 ok\n2 ##STEP 60 begin\n")
	rec := httptest.NewRecorder()
	handleUpdatesProgress(running, nil, &fakeInstaller{status: failure})(rec, httptest.NewRequest(http.MethodGet, "/api/v1/updates/progress", nil))
	if !strings.Contains(rec.Body.String(), `"state":"running"`) || strings.Contains(rec.Body.String(), `"state":"fail"`) {
		t.Fatalf("running job: body = %s", rec.Body.String())
	}

	pending := t.TempDir()
	writeState(t, pending, "pending.json", `{"bundle_version":"v0.10.0","steps":["10"],"run_id":"r3"}`)
	rec = httptest.NewRecorder()
	handleUpdatesProgress(pending, nil, &fakeInstaller{status: failure})(rec, httptest.NewRequest(http.MethodGet, "/api/v1/updates/progress", nil))
	if !strings.Contains(rec.Body.String(), `"state":"pending"`) || strings.Contains(rec.Body.String(), `"state":"fail"`) {
		t.Fatalf("pending job: body = %s", rec.Body.String())
	}
}

// writeJobFinishedAt writes a finished, successful job whose status.json
// carries the given modification time, which is when the job ended.
func writeJobFinishedAt(t *testing.T, dir string, at time.Time) {
	t.Helper()
	writeState(t, dir, "current.json", `{"bundle_version":"v0.10.0","steps":["10"],"run_id":"r1"}`)
	writeState(t, dir, "log", "1 ##STEP 10 ok\n")
	writeState(t, dir, "status.json", `{"result":"ok"}`)
	if err := os.Chtimes(filepath.Join(dir, "status.json"), at, at); err != nil {
		t.Fatal(err)
	}
}

func TestStatusAcceptsInstallUpdates(t *testing.T) {
	router, manager := newInstallTestRouter(t, &fakeInstaller{}, nil)
	cookie, _ := loginWithRoles(t, router, manager, "ivo", []string{auth.RoleInstallUpdates})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/updates/status", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, request)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
		t.Fatalf("install_updates-only GET /api/v1/updates/status = %d: %s", rec.Code, rec.Body.String())
	}
}
