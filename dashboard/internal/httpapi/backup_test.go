package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/backup"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

const backupTestPassword = "geheimes-passwort-1"

// backupUser ist ein angemeldetes Konto mit seinem CSRF-Token.
type backupUser struct {
	cookie *http.Cookie
	csrf   string
}

// backupFixture baut einen Router mit Backup-Routen, eigenem Datenverzeichnis
// und Konten mit unterschiedlichen Rollen.
type backupFixture struct {
	t         *testing.T
	router    http.Handler
	manager   *auth.Manager
	dataDir   string
	backupDir string
	jobs      *backup.Jobs
}

func newBackupFixture(t *testing.T) *backupFixture {
	t.Helper()
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "settings.json"), []byte(`{"theme":"hell"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	settingsFn := func() backup.Settings {
		s, err := backup.LoadSettings(dataDir)
		if err != nil {
			return backup.DefaultSettings()
		}
		return s
	}
	jobs := &backup.Jobs{}
	deps := &BackupDeps{
		DataDir:  dataDir,
		Sources:  backup.Sources{DataDir: dataDir, DevicesDir: t.TempDir()},
		Version:  "test-1.0",
		Settings: settingsFn,
		Location: &backup.SystemLocation{DataDir: dataDir, Settings: settingsFn},
		Jobs:     jobs,
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{
		Auth:   manager,
		Backup: deps,
	})
	f := &backupFixture{
		t:         t,
		router:    router,
		manager:   manager,
		dataDir:   dataDir,
		backupDir: filepath.Join(dataDir, "backups"),
		jobs:      jobs,
	}
	// Registered after the TempDir cleanups, so it runs first: no archive
	// write may still be running when the directories are removed.
	t.Cleanup(f.waitIdle)
	return f
}

// user legt ein Konto mit den gegebenen Rollen an und meldet es über HTTPS an.
func (f *backupFixture) user(name string, roles ...string) backupUser {
	f.t.Helper()
	if _, err := f.manager.CreateUser("admin", name, backupTestPassword, roles); err != nil {
		f.t.Fatal(err)
	}
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, name, backupTestPassword)
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
	return backupUser{cookie: recorder.Result().Cookies()[0], csrf: session.CSRFToken}
}

// call schickt eine Anfrage über HTTPS. csrf leer heisst: kein Token.
func (f *backupFixture) call(method, path, body string, who backupUser, csrf string) *httptest.ResponseRecorder {
	f.t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.AddCookie(who.cookie)
	request.Header.Set("X-Forwarded-Proto", "https")
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	recorder := httptest.NewRecorder()
	f.router.ServeHTTP(recorder, request)
	return recorder
}

// waitIdle wartet, bis kein Backup-Auftrag mehr läuft.
func (f *backupFixture) waitIdle() {
	deadline := time.Now().Add(10 * time.Second)
	for f.jobs.Status().Running && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
}

// storeArchive legt ein Archiv im Ablageort an und liefert seine ID.
func (f *backupFixture) storeArchive() string {
	f.t.Helper()
	store, err := backup.NewStore(f.backupDir)
	if err != nil {
		f.t.Fatal(err)
	}
	info, err := store.Add(backup.Info{Trigger: "manual", Place: "system", Areas: []backup.Area{backup.AreaSettings}}, 0, func(w io.Writer) error {
		_, err := w.Write([]byte("archiv"))
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return info.ID
}

func backupErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&body); err != nil {
		t.Fatalf("error body: %v (%s)", err, recorder.Body.String())
	}
	return body.Code
}

func TestBackupRoutesRequireRoles(t *testing.T) {
	f := newBackupFixture(t)
	viewer := f.user("viewer", auth.RoleViewOverview)
	exporter := f.user("exporter", auth.RoleBackupExport)
	importer := f.user("importer", auth.RoleBackupImport)
	admin, adminCSRF := loginAsAdmin(t, f.router)
	adminUser := backupUser{cookie: admin, csrf: adminCSRF}
	storedID := f.storeArchive()
	deletableID := f.storeArchive()

	type routeCase struct {
		name    string
		method  string
		path    string
		body    string
		write   bool
		allowed []backupUser
		denied  []backupUser
		okCode  int
	}
	cases := []routeCase{
		{
			name: "status", method: http.MethodGet, path: "/api/v1/backup/status",
			allowed: []backupUser{exporter, importer, adminUser}, denied: []backupUser{viewer}, okCode: http.StatusOK,
		},
		{
			name: "settings", method: http.MethodPut, path: "/api/v1/backup/settings",
			body:  `{"before_update":true,"schedule":false,"every":"week","keep":5,"system_fallback":true,"secrets":false,"content":{"settings":true}}`,
			write: true, allowed: []backupUser{exporter, adminUser}, denied: []backupUser{importer, viewer}, okCode: http.StatusOK,
		},
		{
			name: "archives", method: http.MethodPost, path: "/api/v1/backup/archives",
			body:  `{"areas":{"settings":true},"target":"store"}`,
			write: true, allowed: []backupUser{exporter}, denied: []backupUser{importer, viewer}, okCode: http.StatusAccepted,
		},
		{
			name: "archive get", method: http.MethodGet, path: "/api/v1/backup/archives/" + storedID,
			allowed: []backupUser{exporter}, denied: []backupUser{importer, viewer}, okCode: http.StatusOK,
		},
		{
			name: "archive delete", method: http.MethodDelete, path: "/api/v1/backup/archives/" + deletableID,
			write: true, allowed: []backupUser{exporter}, denied: []backupUser{importer, viewer}, okCode: http.StatusOK,
		},
		{
			name: "job", method: http.MethodGet, path: "/api/v1/backup/job",
			allowed: []backupUser{exporter, importer}, denied: []backupUser{viewer}, okCode: http.StatusOK,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, who := range c.denied {
				csrf := ""
				if c.write {
					csrf = who.csrf
				}
				recorder := f.call(c.method, c.path, c.body, who, csrf)
				if recorder.Code != http.StatusForbidden {
					t.Fatalf("status %d without role, want 403: %s", recorder.Code, recorder.Body.String())
				}
			}
			for _, who := range c.allowed {
				csrf := ""
				if c.write {
					csrf = who.csrf
				}
				recorder := f.call(c.method, c.path, c.body, who, csrf)
				if recorder.Code != c.okCode {
					t.Fatalf("status %d with role, want %d: %s", recorder.Code, c.okCode, recorder.Body.String())
				}
				f.waitIdle()
			}
		})
	}
}

func TestBackupRoutesRequireHTTPSAndCSRF(t *testing.T) {
	f := newBackupFixture(t)
	exporter := f.user("exporter", auth.RoleBackupExport)
	archiveID := f.storeArchive()

	// Ein Konto mit HTTPS-Rollen meldet sich nicht über HTTP an, so kommt auch
	// kein Sitzungscookie für Backup-Aufrufe über HTTP zustande.
	plainLogin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(fmt.Sprintf(`{"username":"exporter","password":%q}`, backupTestPassword)))
	plainLogin.Header.Set("Content-Type", "application/json")
	plainRecorder := httptest.NewRecorder()
	f.router.ServeHTTP(plainRecorder, plainLogin)
	if plainRecorder.Code != http.StatusForbidden {
		t.Fatalf("plain login status %d, want 403: %s", plainRecorder.Code, plainRecorder.Body.String())
	}
	if code := backupErrorCode(t, plainRecorder); code != "secure_login_required" {
		t.Fatalf("plain login code %q, want secure_login_required", code)
	}
	// Ohne Cookie und über HTTP bleibt Backup gesperrt.
	anonymous := httptest.NewRequest(http.MethodGet, "/api/v1/backup/status", nil)
	anonymousRecorder := httptest.NewRecorder()
	f.router.ServeHTTP(anonymousRecorder, anonymous)
	if anonymousRecorder.Code == http.StatusOK {
		t.Fatalf("status without session over HTTP %d, want it rejected", anonymousRecorder.Code)
	}

	// Schreibende Routen ohne CSRF-Token werden abgewiesen.
	writes := []struct {
		method, path, body string
	}{
		{http.MethodPut, "/api/v1/backup/settings", `{"every":"week","keep":5}`},
		{http.MethodPost, "/api/v1/backup/archives", `{"areas":{"settings":true},"target":"store"}`},
		{http.MethodDelete, "/api/v1/backup/archives/" + archiveID, ""},
	}
	for _, w := range writes {
		recorder := f.call(w.method, w.path, w.body, exporter, "")
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s %s without CSRF status %d, want 403: %s", w.method, w.path, recorder.Code, recorder.Body.String())
		}
		if code := backupErrorCode(t, recorder); code != "csrf_failed" {
			t.Fatalf("%s %s without CSRF code %q, want csrf_failed", w.method, w.path, code)
		}
	}
	if _, err := os.Stat(filepath.Join(f.backupDir, archiveID+".json")); err != nil {
		t.Fatalf("archive was deleted although the CSRF check failed: %v", err)
	}
}

func TestSecretsNeedExtraRole(t *testing.T) {
	f := newBackupFixture(t)
	exporter := f.user("exporter", auth.RoleBackupExport)
	secrets := f.user("secrets", auth.RoleBackupExport, auth.RoleBackupSecrets)

	archive := f.call(http.MethodPost, "/api/v1/backup/archives", `{"areas":{"secrets":true},"secrets":true,"target":"store"}`, exporter, exporter.csrf)
	if archive.Code != http.StatusForbidden {
		t.Fatalf("archive with secrets without backup_secrets status %d, want 403: %s", archive.Code, archive.Body.String())
	}
	settingsSecrets := f.call(http.MethodPut, "/api/v1/backup/settings", `{"every":"week","keep":5,"secrets":true,"content":{"settings":true}}`, exporter, exporter.csrf)
	if settingsSecrets.Code != http.StatusForbidden {
		t.Fatalf("settings with secrets:true without backup_secrets status %d, want 403: %s", settingsSecrets.Code, settingsSecrets.Body.String())
	}
	settingsContent := f.call(http.MethodPut, "/api/v1/backup/settings", `{"every":"week","keep":5,"content":{"secrets":true}}`, exporter, exporter.csrf)
	if settingsContent.Code != http.StatusForbidden {
		t.Fatalf("settings with content.secrets without backup_secrets status %d, want 403: %s", settingsContent.Code, settingsContent.Body.String())
	}

	allowed := f.call(http.MethodPut, "/api/v1/backup/settings", `{"every":"week","keep":5,"system_fallback":true,"secrets":true,"content":{"settings":true,"secrets":true}}`, secrets, secrets.csrf)
	if allowed.Code != http.StatusOK {
		t.Fatalf("settings with secrets and backup_secrets status %d, want 200: %s", allowed.Code, allowed.Body.String())
	}
	job := f.call(http.MethodPost, "/api/v1/backup/archives", `{"areas":{"secrets":true},"secrets":true,"target":"store"}`, secrets, secrets.csrf)
	if job.Code != http.StatusAccepted {
		t.Fatalf("archive with secrets and backup_secrets status %d, want 202: %s", job.Code, job.Body.String())
	}
}

// downloadStaging liefert den Staging-Ordner der Downloads.
func (f *backupFixture) downloadStaging() string {
	return filepath.Join(f.dataDir, "backup-staging")
}

func TestDownloadStreamsAndDeletesTemporary(t *testing.T) {
	f := newBackupFixture(t)
	exporter := f.user("exporter", auth.RoleBackupExport)

	start := f.call(http.MethodPost, "/api/v1/backup/archives", `{"areas":{"settings":true},"target":"download"}`, exporter, exporter.csrf)
	if start.Code != http.StatusAccepted {
		t.Fatalf("download start status %d: %s", start.Code, start.Body.String())
	}
	f.waitIdle()

	jobRecorder := f.call(http.MethodGet, "/api/v1/backup/job", "", exporter, "")
	var job backup.JobStatus
	if err := json.NewDecoder(jobRecorder.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	if job.Error != "" || job.Result == "" {
		t.Fatalf("job finished with error %q, result %q", job.Error, job.Result)
	}
	tempFile := filepath.Join(f.downloadStaging(), "download-"+job.Result+".tar.gz")
	if _, err := os.Stat(tempFile); err != nil {
		t.Fatalf("download file missing: %v", err)
	}

	download := f.call(http.MethodGet, "/api/v1/backup/archives/"+job.Result, "", exporter, "")
	if download.Code != http.StatusOK {
		t.Fatalf("download status %d: %s", download.Code, download.Body.String())
	}
	if disposition := download.Header().Get("Content-Disposition"); !strings.HasPrefix(disposition, "attachment") {
		t.Fatalf("Content-Disposition = %q, want attachment", disposition)
	}
	if body := download.Body.Bytes(); len(body) < 2 || body[0] != 0x1f || body[1] != 0x8b {
		t.Fatal("download is not a gzip archive")
	}
	if _, err := os.Stat(tempFile); !os.IsNotExist(err) {
		t.Fatalf("download file still exists after serving: %v", err)
	}

	// Temporäre Downloads älter als eine Stunde verschwinden beim Status-Aufruf.
	if err := os.MkdirAll(f.downloadStaging(), 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(f.downloadStaging(), "download-20200101-000000-manual.tar.gz")
	if err := os.WriteFile(stale, []byte("alt"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if status := f.call(http.MethodGet, "/api/v1/backup/status", "", exporter, ""); status.Code != http.StatusOK {
		t.Fatalf("status %d: %s", status.Code, status.Body.String())
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale download survived the status call: %v", err)
	}
}

func TestArchiveIDsAreValidated(t *testing.T) {
	f := newBackupFixture(t)
	exporter := f.user("exporter", auth.RoleBackupExport)
	victim := filepath.Join(f.dataDir, "victim.json")
	if err := os.WriteFile(victim, []byte("nicht anfassen"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"..x", "a/b", "x.tar.gz", "../x"} {
		path := "/api/v1/backup/archives/" + id
		deleted := f.call(http.MethodDelete, path, "", exporter, exporter.csrf)
		if deleted.Code == http.StatusOK || deleted.Code == http.StatusAccepted {
			t.Fatalf("DELETE %q status %d, want it rejected", id, deleted.Code)
		}
		fetched := f.call(http.MethodGet, path, "", exporter, "")
		if fetched.Code == http.StatusOK {
			t.Fatalf("GET %q status %d, want it rejected", id, fetched.Code)
		}
	}
	if raw, err := os.ReadFile(victim); err != nil || string(raw) != "nicht anfassen" {
		t.Fatalf("file outside the archive directory changed: %q (err=%v)", raw, err)
	}
}

func TestSettingsPutKeepsOmittedFields(t *testing.T) {
	f := newBackupFixture(t)
	exporter := f.user("exporter", auth.RoleBackupExport)
	stored := backup.DefaultSettings()
	stored.Schedule = true
	stored.SystemFallback = true
	if err := backup.SaveSettings(f.dataDir, stored); err != nil {
		t.Fatal(err)
	}

	recorder := f.call(http.MethodPut, "/api/v1/backup/settings", `{"keep":7}`, exporter, exporter.csrf)
	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT status %d: %s", recorder.Code, recorder.Body.String())
	}
	saved, err := backup.LoadSettings(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Keep != 7 {
		t.Fatalf("keep = %d, want 7", saved.Keep)
	}
	if !saved.SystemFallback || !saved.Schedule || saved.Every != "week" || !saved.Content[backup.AreaSettings] {
		t.Fatalf("omitted fields were not kept: %+v", saved)
	}
}

func TestSettingsPutSecretsOnlyChecksNewlyEnabled(t *testing.T) {
	f := newBackupFixture(t)
	exporter := f.user("exporter", auth.RoleBackupExport)
	secrets := f.user("secrets", auth.RoleBackupExport, auth.RoleBackupSecrets)

	on := backup.DefaultSettings()
	on.SystemFallback = true
	on.Secrets = true
	on.Content[backup.AreaSecrets] = true
	if err := backup.SaveSettings(f.dataDir, on); err != nil {
		t.Fatal(err)
	}
	// Andere Felder speichern, während Zugangsdaten schon eingeschaltet sind.
	kept := f.call(http.MethodPut, "/api/v1/backup/settings", `{"keep":6,"secrets":true,"content":{"settings":true,"secrets":true}}`, exporter, exporter.csrf)
	if kept.Code != http.StatusOK {
		t.Fatalf("export-only save with secrets already on status %d, want 200: %s", kept.Code, kept.Body.String())
	}

	off := backup.DefaultSettings()
	off.SystemFallback = true
	if err := backup.SaveSettings(f.dataDir, off); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"secrets":true}`, `{"content":{"secrets":true}}`} {
		recorder := f.call(http.MethodPut, "/api/v1/backup/settings", body, exporter, exporter.csrf)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("export-only enabling secrets with %s status %d, want 403: %s", body, recorder.Code, recorder.Body.String())
		}
	}
	allowed := f.call(http.MethodPut, "/api/v1/backup/settings", `{"secrets":true,"content":{"settings":true,"secrets":true}}`, secrets, secrets.csrf)
	if allowed.Code != http.StatusOK {
		t.Fatalf("enabling secrets with backup_secrets status %d, want 200: %s", allowed.Code, allowed.Body.String())
	}
}
