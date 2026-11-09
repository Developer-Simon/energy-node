package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/backup"
)

const importArchivePassword = "archiv-geheim-1"

// importArchive baut ein Archiv mit dem Bereich Einstellungen aus dem Datenverzeichnis der Fixture.
func importArchive(t *testing.T, f *backupFixture, nodeVersion, password string) []byte {
	t.Helper()
	var buf bytes.Buffer
	opts := backup.CreateOptions{
		Areas:       map[backup.Area]bool{backup.AreaSettings: true},
		Trigger:     "manual",
		NodeVersion: nodeVersion,
		Password:    password,
	}
	sources := backup.Sources{DataDir: f.dataDir, DevicesDir: t.TempDir()}
	if _, err := backup.Create(context.Background(), sources, opts, &buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// importRequest schickt eine Anfrage an den Import über HTTPS mit CSRF-Token.
func (f *backupFixture) importRequest(path, contentType, password string, body []byte, who backupUser) *httptest.ResponseRecorder {
	f.t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	request.AddCookie(who.cookie)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-CSRF-Token", who.csrf)
	request.Header.Set("Content-Type", contentType)
	if password != "" {
		request.Header.Set("X-Backup-Password", password)
	}
	recorder := httptest.NewRecorder()
	f.router.ServeHTTP(recorder, request)
	return recorder
}

// importSnapshot liefert den Inhalt der Einstellungen und ob Staging oder Upload übrig sind.
func (f *backupFixture) importSnapshot() (settings string, leftovers []string) {
	f.t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.dataDir, "settings.json"))
	if err != nil {
		f.t.Fatal(err)
	}
	for _, name := range []string{filepath.Join("backup-staging", "import"), filepath.Join("backup-staging", "import-upload")} {
		if _, err := os.Stat(filepath.Join(f.dataDir, name)); err == nil {
			leftovers = append(leftovers, name)
		}
	}
	return string(raw), leftovers
}

func TestImportRejectsCorruptWithCode(t *testing.T) {
	f := newBackupFixture(t)
	importer := f.user("importer", auth.RoleBackupImport)
	archive := importArchive(t, f, "1.0.0", "")
	truncated := archive[:len(archive)/2]
	before, _ := f.importSnapshot()

	recorder := f.importRequest("/api/v1/backup/import", "application/octet-stream", "", truncated, importer)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: %s", recorder.Code, recorder.Body.String())
	}
	if code := backupErrorCode(t, recorder); code != "backup_corrupt" {
		t.Fatalf("code %q, want backup_corrupt", code)
	}
	after, leftovers := f.importSnapshot()
	if after != before {
		t.Fatalf("live settings changed: %q -> %q", before, after)
	}
	if len(leftovers) > 0 {
		t.Fatalf("staging or upload left behind: %v", leftovers)
	}
}

func TestImportTooNewAndForeignCodes(t *testing.T) {
	f := newBackupFixture(t)
	importer := f.user("importer", auth.RoleBackupImport)

	previous := backup.NodeVersion
	backup.NodeVersion = "1.0.0"
	t.Cleanup(func() { backup.NodeVersion = previous })

	newer := importArchive(t, f, "9.0.0", "")
	recorder := f.importRequest("/api/v1/backup/import", "application/octet-stream", "", newer, importer)
	if recorder.Code != http.StatusUnprocessableEntity || backupErrorCode(t, recorder) != "backup_too_new" {
		t.Fatalf("too new: status %d body %s", recorder.Code, recorder.Body.String())
	}

	foreign := []byte("das ist kein Backup-Archiv")
	recorder = f.importRequest("/api/v1/backup/import", "application/octet-stream", "", foreign, importer)
	if recorder.Code != http.StatusUnprocessableEntity || backupErrorCode(t, recorder) != "backup_incompatible" {
		t.Fatalf("foreign: status %d body %s", recorder.Code, recorder.Body.String())
	}
}

func TestImportPasswordFlow(t *testing.T) {
	f := newBackupFixture(t)
	importer := f.user("importer", auth.RoleBackupImport)
	locked := importArchive(t, f, "1.0.0", importArchivePassword)

	recorder := f.importRequest("/api/v1/backup/import", "application/octet-stream", "", locked, importer)
	if recorder.Code != http.StatusConflict || backupErrorCode(t, recorder) != "password_required" {
		t.Fatalf("no password: status %d body %s", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, "backup-staging", "import-upload")); err != nil {
		t.Fatalf("upload not kept for unlock: %v", err)
	}

	unlock := "/api/v1/backup/import/unlock"
	wrong := f.importRequest(unlock, "application/json", "", []byte(`{"password":"falsch-falsch-1"}`), importer)
	if wrong.Code != http.StatusUnprocessableEntity || backupErrorCode(t, wrong) != "wrong_password" {
		t.Fatalf("wrong password: status %d body %s", wrong.Code, wrong.Body.String())
	}

	right := f.importRequest(unlock, "application/json", "", []byte(`{"password":"`+importArchivePassword+`"}`), importer)
	if right.Code != http.StatusOK {
		t.Fatalf("correct password: status %d body %s", right.Code, right.Body.String())
	}
	var preview struct {
		Manifest struct {
			NodeVersion string `json:"node_version"`
		} `json:"manifest"`
		Areas []struct {
			Area string `json:"area"`
		} `json:"areas"`
	}
	if err := json.NewDecoder(right.Body).Decode(&preview); err != nil {
		t.Fatal(err)
	}
	if preview.Manifest.NodeVersion != "1.0.0" || len(preview.Areas) == 0 {
		t.Fatalf("preview not returned: %+v", preview)
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, "backup-staging", "import-upload")); err == nil {
		t.Fatal("upload left after successful unlock")
	}
}

func TestImportRequiresRoleHTTPSCSRF(t *testing.T) {
	f := newBackupFixture(t)
	exporter := f.user("exporter", auth.RoleBackupExport)
	importer := f.user("importer", auth.RoleBackupImport)
	archive := importArchive(t, f, "1.0.0", "")

	denied := f.importRequest("/api/v1/backup/import", "application/octet-stream", "", archive, exporter)
	if denied.Code != http.StatusForbidden || backupErrorCode(t, denied) != "backup_forbidden" {
		t.Fatalf("role: status %d body %s", denied.Code, denied.Body.String())
	}

	// Über Klartext meldet sich nur ein Konto mit http_login an, die Sitzung ist
	// eingeschränkt. Der Import muss auch dann abgewiesen werden.
	if _, err := f.manager.CreateUser("admin", "plain", backupTestPassword, []string{auth.RoleBackupImport, auth.RoleHTTPLogin}); err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"plain","password":"`+backupTestPassword+`"}`))
	login.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	f.router.ServeHTTP(loginRecorder, login)
	if loginRecorder.Code != http.StatusOK || len(loginRecorder.Result().Cookies()) == 0 {
		t.Fatalf("plain login: status %d body %s", loginRecorder.Code, loginRecorder.Body.String())
	}
	var plainSession struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(loginRecorder.Body).Decode(&plainSession); err != nil {
		t.Fatal(err)
	}
	plain := backupUser{cookie: loginRecorder.Result().Cookies()[0], csrf: plainSession.CSRFToken}
	noHTTPS := httptest.NewRequest(http.MethodPost, "/api/v1/backup/import", bytes.NewReader(archive))
	noHTTPS.AddCookie(plain.cookie)
	noHTTPS.Header.Set("X-CSRF-Token", plain.csrf)
	noHTTPS.Header.Set("Content-Type", "application/octet-stream")
	noHTTPSRecorder := httptest.NewRecorder()
	f.router.ServeHTTP(noHTTPSRecorder, noHTTPS)
	if noHTTPSRecorder.Code != http.StatusForbidden {
		t.Fatalf("https: status %d body %s", noHTTPSRecorder.Code, noHTTPSRecorder.Body.String())
	}

	noCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/backup/import", bytes.NewReader(archive))
	noCSRF.AddCookie(importer.cookie)
	noCSRF.Header.Set("X-Forwarded-Proto", "https")
	noCSRFRecorder := httptest.NewRecorder()
	f.router.ServeHTTP(noCSRFRecorder, noCSRF)
	if noCSRFRecorder.Code != http.StatusForbidden || backupErrorCode(t, noCSRFRecorder) != "csrf_failed" {
		t.Fatalf("csrf: status %d body %s", noCSRFRecorder.Code, noCSRFRecorder.Body.String())
	}

	unlockNoCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/backup/import/unlock", strings.NewReader(`{"password":"x"}`))
	unlockNoCSRF.AddCookie(exporter.cookie)
	unlockNoCSRF.Header.Set("X-Forwarded-Proto", "https")
	unlockRecorder := httptest.NewRecorder()
	f.router.ServeHTTP(unlockRecorder, unlockNoCSRF)
	if unlockRecorder.Code != http.StatusForbidden {
		t.Fatalf("unlock without role or csrf: status %d", unlockRecorder.Code)
	}
}

func TestImportFromStoredArchive(t *testing.T) {
	f := newBackupFixture(t)
	importer := f.user("importer", auth.RoleBackupImport)

	var buf bytes.Buffer
	sources := backup.Sources{DataDir: f.dataDir, DevicesDir: t.TempDir()}
	opts := backup.CreateOptions{Areas: map[backup.Area]bool{backup.AreaSettings: true}, Trigger: "manual", NodeVersion: "1.0.0"}
	if _, err := backup.Create(context.Background(), sources, opts, &buf); err != nil {
		t.Fatal(err)
	}
	store, err := backup.NewStore(f.backupDir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := store.Add(backup.Info{Trigger: "manual", Place: "system", Areas: []backup.Area{backup.AreaSettings}}, 0, func(w io.Writer) error {
		_, err := w.Write(buf.Bytes())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	recorder := f.importRequest("/api/v1/backup/import", "application/json", "", []byte(`{"archive":"`+info.ID+`"}`), importer)
	if recorder.Code != http.StatusOK {
		t.Fatalf("stored archive: status %d body %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"different_device"`) {
		t.Fatalf("response without preview: %s", recorder.Body.String())
	}
}
