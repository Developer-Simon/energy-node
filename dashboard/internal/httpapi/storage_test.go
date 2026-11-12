package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/storage"
)

const storageTestPassword = "geheimes-passwort-2"

type storageDrives []storage.Drive

func (d storageDrives) Drives(ctx context.Context) ([]storage.Drive, error) { return d, nil }

// storageFixture baut einen Router mit den Speicher-Routen. Die Helfer im
// Dienst sind Stubs, so laeuft kein Auftrag im Hintergrund.
type storageFixture struct {
	t       *testing.T
	router  http.Handler
	manager *auth.Manager
	dataDir string
	svc     *storage.Service
	deps    *StorageDeps
}

func newStorageFixture(t *testing.T) *storageFixture {
	t.Helper()
	dataDir := t.TempDir()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	svc := &storage.Service{
		Paths: storage.Paths{
			Runtime:        filepath.Join(dataDir, "runtime"),
			Mount:          filepath.Join(t.TempDir(), "mnt"),
			SystemFallback: filepath.Join(dataDir, "runtime-fallback"),
			RAMFallback:    filepath.Join(t.TempDir(), "ram"),
		},
		DataDir: dataDir,
		NodeID:  "node-1",
		Deps: storage.Deps{
			Lister: storageDrives{
				{Kname: "sdb", Name: "USB", NeedsFormat: true, Contents: storage.ContentsNone},
				{Kname: "sdc", Name: "Old", UUID: "11111111-2222-3333-4444-555555555555", FSType: "ext4", Ready: true},
			},
			Storage: func(ctx context.Context, verb, arg string) (string, error) {
				return "", errors.New("helper is not used in route tests")
			},
		},
	}
	deps := &StorageDeps{
		DataDir: dataDir,
		Service: svc,
		Lister:  svc.Deps.Lister,
		Status:  func() storage.Status { return storage.Status{Mode: storage.ModeNone} },
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{
		Auth:    manager,
		Storage: deps,
	})
	return &storageFixture{t: t, router: router, manager: manager, dataDir: dataDir, svc: svc, deps: deps}
}

// login meldet ein Konto über HTTPS an und liefert Cookie und CSRF-Token.
func (f *storageFixture) login(name string, roles ...string) (*http.Cookie, string) {
	f.t.Helper()
	if _, err := f.manager.CreateUser("admin", name, storageTestPassword, roles); err != nil {
		f.t.Fatal(err)
	}
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, name, storageTestPassword)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	f.router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		f.t.Fatalf("login %s status %d: %s", name, recorder.Code, recorder.Body.String())
	}
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&session); err != nil {
		f.t.Fatal(err)
	}
	return recorder.Result().Cookies()[0], session.CSRFToken
}

// call schickt eine Anfrage. secure setzt HTTPS, cookie und csrf sind optional.
func (f *storageFixture) call(method, path, body string, cookie *http.Cookie, csrf string, secure bool) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if secure {
		request.Header.Set("X-Forwarded-Proto", "https")
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	recorder := httptest.NewRecorder()
	f.router.ServeHTTP(recorder, request)
	return recorder
}

func storageErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body: %v (%s)", err, recorder.Body.String())
	}
	return body.Code
}

// storageRoutes lists every route with a request that the validation refuses
// or that is harmless, so the route tests never start a job.
var storageRoutes = []struct {
	method, path, body string
	write              bool
}{
	{http.MethodGet, "/api/v1/storage/status", "", false},
	{http.MethodPost, "/api/v1/storage/recheck", "", true},
	{http.MethodGet, "/api/v1/storage/drives", "", false},
	{http.MethodPost, "/api/v1/storage/setup", `{"kname":"sdz","use_data":true,"fallback":"system"}`, true},
	{http.MethodPut, "/api/v1/storage/fallback", `{"fallback":"ram"}`, true},
	{http.MethodPost, "/api/v1/storage/replace", `{"kname":"sdz","use_data":true,"fallback":"system"}`, true},
	{http.MethodPost, "/api/v1/storage/remove", "", true},
}

func TestStorageRoutesRequireRoleHTTPSCSRF(t *testing.T) {
	f := newStorageFixture(t)
	cookie, csrf := f.login("keeper", auth.RoleStorageManage)
	viewer, viewerCSRF := f.login("viewer", auth.RoleViewOverview)

	for _, route := range storageRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			// Anonym: keine Anmeldung.
			anon := f.call(route.method, route.path, route.body, nil, "", true)
			if anon.Code != http.StatusUnauthorized {
				t.Fatalf("anonymous status %d, want 401: %s", anon.Code, anon.Body.String())
			}

			// Angemeldet, aber ohne storage_manage.
			forbidden := f.call(route.method, route.path, route.body, viewer, viewerCSRF, true)
			if forbidden.Code != http.StatusForbidden || storageErrorCode(t, forbidden) != "storage_forbidden" {
				t.Fatalf("without storage_manage: status %d %s, want 403 storage_forbidden", forbidden.Code, forbidden.Body.String())
			}

			// Mit dem Recht, aber über HTTP: das Sitzungscookie gilt dort nicht.
			plain := f.call(route.method, route.path, route.body, cookie, csrf, false)
			if plain.Code == http.StatusOK || plain.Code == http.StatusAccepted {
				t.Fatalf("plain HTTP status %d, want it rejected", plain.Code)
			}

			// Schreibende Routen brauchen das CSRF-Token.
			if route.write {
				noCSRF := f.call(route.method, route.path, route.body, cookie, "", true)
				if noCSRF.Code != http.StatusForbidden || storageErrorCode(t, noCSRF) != "csrf_failed" {
					t.Fatalf("without CSRF status %d %s, want 403 csrf_failed", noCSRF.Code, noCSRF.Body.String())
				}
			}
		})
	}
}

func TestStorageLoginRefusedOverHTTP(t *testing.T) {
	f := newStorageFixture(t)
	f.login("keeper", auth.RoleStorageManage)
	plainLogin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(fmt.Sprintf(`{"username":"keeper","password":%q}`, storageTestPassword)))
	plainLogin.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	f.router.ServeHTTP(recorder, plainLogin)
	if recorder.Code != http.StatusForbidden || storageErrorCode(t, recorder) != "secure_login_required" {
		t.Fatalf("plain login status %d %s, want 403 secure_login_required", recorder.Code, recorder.Body.String())
	}
}

func TestStorageStatusAndDrivesWithRole(t *testing.T) {
	f := newStorageFixture(t)
	cookie, csrf := f.login("keeper", auth.RoleStorageManage)

	status := f.call(http.MethodGet, "/api/v1/storage/status", "", cookie, "", true)
	if status.Code != http.StatusOK {
		t.Fatalf("status %d: %s", status.Code, status.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(status.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"config", "status", "system", "drive", "job"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("status response lacks %q: %s", key, status.Body.String())
		}
	}

	drives := f.call(http.MethodGet, "/api/v1/storage/drives", "", cookie, "", true)
	var listed []storage.Drive
	if drives.Code != http.StatusOK || json.Unmarshal(drives.Body.Bytes(), &listed) != nil || len(listed) != 2 {
		t.Fatalf("drives status %d: %s", drives.Code, drives.Body.String())
	}

	fallback := f.call(http.MethodPut, "/api/v1/storage/fallback", `{"fallback":"ram"}`, cookie, csrf, true)
	if fallback.Code != http.StatusOK {
		t.Fatalf("fallback status %d: %s", fallback.Code, fallback.Body.String())
	}
	cfg, err := storage.LoadConfig(f.dataDir)
	if err != nil || cfg.Fallback != storage.FallbackRAM {
		t.Fatalf("fallback not saved: %+v, %v", cfg, err)
	}
	bad := f.call(http.MethodPut, "/api/v1/storage/fallback", `{"fallback":"cloud"}`, cookie, csrf, true)
	if bad.Code != http.StatusBadRequest || storageErrorCode(t, bad) != storage.CodeFallbackInvalid {
		t.Fatalf("bad fallback status %d %s", bad.Code, bad.Body.String())
	}
}

func TestStorageSetupRejectsUnknownDriveBeforeAnyHelperCall(t *testing.T) {
	f := newStorageFixture(t)
	cookie, csrf := f.login("keeper", auth.RoleStorageManage)

	cases := []struct {
		name string
		body string
		code string
	}{
		{"unknown kname", `{"kname":"sdz","use_data":true,"fallback":"system"}`, storage.CodeUnknownDrive},
		{"format a device the own detection does not offer", `{"kname":"/dev/sda","format":true,"use_data":true,"fallback":"system"}`, storage.CodeUnknownDrive},
		{"format a ready drive", `{"kname":"sdc","format":true,"use_data":true,"fallback":"system"}`, storage.CodeFormatNotNeeded},
		{"no format confirmation for a new drive", `{"kname":"sdb","use_data":true,"fallback":"system"}`, storage.CodeNeedsFormat},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := f.call(http.MethodPost, "/api/v1/storage/setup", tc.body, cookie, csrf, true)
			if recorder.Code != http.StatusBadRequest || storageErrorCode(t, recorder) != tc.code {
				t.Fatalf("status %d %s, want 400 %s", recorder.Code, recorder.Body.String(), tc.code)
			}
		})
	}
	if job := f.svc.Job(); job.Phase != "" || job.Done {
		t.Fatalf("rejected requests started a job: %+v", job)
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, "storage.json")); !os.IsNotExist(err) {
		t.Fatalf("rejected request wrote storage.json: %v", err)
	}
}

func TestStorageRemoveWithoutSetupIsRefused(t *testing.T) {
	f := newStorageFixture(t)
	cookie, csrf := f.login("keeper", auth.RoleStorageManage)
	recorder := f.call(http.MethodPost, "/api/v1/storage/remove", "", cookie, csrf, true)
	if recorder.Code != http.StatusConflict || storageErrorCode(t, recorder) != storage.CodeNotSetUp {
		t.Fatalf("remove status %d %s, want 409 %s", recorder.Code, recorder.Body.String(), storage.CodeNotSetUp)
	}
}

// TestStorageDemoReportsTheStickNotTheFolder checks that in demo mode the
// drive space comes from the listed stick with the configured uuid. The mount
// is a folder on the system disk, so its own numbers would be the system disk's.
func TestStorageDemoReportsTheStickNotTheFolder(t *testing.T) {
	f := newStorageFixture(t)
	const uuid = "11111111-2222-3333-4444-555555555555"
	if err := os.MkdirAll(f.svc.Paths.Mount, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.svc.Paths.Mount, ".energy-node-volume"), []byte("node-1\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveConfig(f.dataDir, storage.Config{UUID: uuid, Fallback: storage.FallbackSystem, UseData: true}); err != nil {
		t.Fatal(err)
	}
	f.deps.DemoSpace = true
	f.deps.Lister = storageDrives{
		{Kname: "sdc", Name: "Kingston DataTraveler", UUID: uuid, Size: 31914983424, Used: 1234567},
	}
	cookie, _ := f.login("keeper", auth.RoleStorageManage)

	status := f.call(http.MethodGet, "/api/v1/storage/status", "", cookie, "", true)
	var body struct {
		Drive struct {
			Size int64 `json:"size"`
			Used int64 `json:"used"`
		} `json:"drive"`
	}
	if status.Code != http.StatusOK || json.Unmarshal(status.Body.Bytes(), &body) != nil {
		t.Fatalf("status %d: %s", status.Code, status.Body.String())
	}
	if body.Drive.Size != 31914983424 || body.Drive.Used != 1234567 {
		t.Fatalf("demo drive space = %d/%d, want the stick's 31914983424/1234567", body.Drive.Size, body.Drive.Used)
	}
}

// TestStorageReplaceRejectsBeforeAnyHelperCall checks the validation of the
// replace route: an unknown kname, a device name the own detection does not
// offer and a drive that is not set up are refused, and the helper is never called.
func TestStorageReplaceRejectsBeforeAnyHelperCall(t *testing.T) {
	f := newStorageFixture(t)
	cookie, csrf := f.login("keeper", auth.RoleStorageManage)
	helperCalls := 0
	f.svc.Deps.Storage = func(ctx context.Context, verb, arg string) (string, error) {
		helperCalls++
		return "", errors.New("helper must not run")
	}

	// The drive must be set up, or the not-set-up check answers first.
	if err := storage.SaveConfig(f.dataDir, storage.Config{UUID: "99999999-2222-3333-4444-555555555555", Fallback: storage.FallbackSystem, UseData: true}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		body string
		code string
	}{
		{"unknown kname", `{"kname":"sdz","use_data":true,"fallback":"system"}`, storage.CodeUnknownDrive},
		{"format a device the own detection does not offer", `{"kname":"/dev/sda","format":true,"use_data":true,"fallback":"system"}`, storage.CodeUnknownDrive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := f.call(http.MethodPost, "/api/v1/storage/replace", tc.body, cookie, csrf, true)
			if recorder.Code != http.StatusBadRequest || storageErrorCode(t, recorder) != tc.code {
				t.Fatalf("status %d %s, want 400 %s", recorder.Code, recorder.Body.String(), tc.code)
			}
		})
	}
	if helperCalls != 0 {
		t.Fatalf("rejected replace called the helper %d times", helperCalls)
	}
	if job := f.svc.Job(); job.Phase != "" || job.Done {
		t.Fatalf("rejected requests started a job: %+v", job)
	}
}

// TestStorageReplaceSameDriveIsRefused checks that the body reaches the
// service: replacing the set-up drive with itself is refused with its code.
func TestStorageReplaceSameDriveIsRefused(t *testing.T) {
	f := newStorageFixture(t)
	cookie, csrf := f.login("keeper", auth.RoleStorageManage)
	const uuid = "11111111-2222-3333-4444-555555555555"
	if err := storage.SaveConfig(f.dataDir, storage.Config{UUID: uuid, Fallback: storage.FallbackSystem, UseData: true}); err != nil {
		t.Fatal(err)
	}
	recorder := f.call(http.MethodPost, "/api/v1/storage/replace", `{"kname":"sdc","uuid":"`+uuid+`","use_data":true,"fallback":"system"}`, cookie, csrf, true)
	if recorder.Code != http.StatusBadRequest || storageErrorCode(t, recorder) != storage.CodeSameDrive {
		t.Fatalf("status %d %s, want 400 %s", recorder.Code, recorder.Body.String(), storage.CodeSameDrive)
	}
}

// TestStorageReplaceWithoutSetupIsRefused checks that a replace before the
// first setup is refused with storage_not_set_up, before any helper call.
func TestStorageReplaceWithoutSetupIsRefused(t *testing.T) {
	f := newStorageFixture(t)
	cookie, csrf := f.login("keeper", auth.RoleStorageManage)
	helperCalls := 0
	f.svc.Deps.Storage = func(ctx context.Context, verb, arg string) (string, error) {
		helperCalls++
		return "", errors.New("helper must not run")
	}
	recorder := f.call(http.MethodPost, "/api/v1/storage/replace", `{"kname":"sdc","use_data":true,"fallback":"system"}`, cookie, csrf, true)
	if recorder.Code != http.StatusConflict || storageErrorCode(t, recorder) != storage.CodeNotSetUp {
		t.Fatalf("status %d %s, want 409 %s", recorder.Code, recorder.Body.String(), storage.CodeNotSetUp)
	}
	if helperCalls != 0 {
		t.Fatalf("replace without setup called the helper %d times", helperCalls)
	}
}

func TestStorageRecheckAsksTheWatcherAndAnswersLikeStatus(t *testing.T) {
	f := newStorageFixture(t)
	cookie, csrf := f.login("keeper", auth.RoleStorageManage)
	calls := 0
	f.deps.Recheck = func(context.Context) storage.Status {
		calls++
		return storage.Status{Mode: storage.ModeDrive}
	}
	f.deps.Status = func() storage.Status { return storage.Status{Mode: storage.ModeDrive} }

	response := f.call(http.MethodPost, "/api/v1/storage/recheck", "", cookie, csrf, true)
	if response.Code != http.StatusOK || calls != 1 {
		t.Fatalf("recheck status %d, watcher asked %d times: %s", response.Code, calls, response.Body.String())
	}
	var body struct {
		Status struct {
			Mode string `json:"mode"`
		} `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Status.Mode != string(storage.ModeDrive) {
		t.Fatalf("recheck body: %v (%s)", err, response.Body.String())
	}
}
