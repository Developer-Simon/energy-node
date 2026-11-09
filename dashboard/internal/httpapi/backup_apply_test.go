package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/backup"
	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
)

const backupApplyPath = "/api/v1/backup/import/apply"

// recordingExec zeichnet die Systemaktionen in der Reihenfolge auf, in der sie laufen.
type recordingExec struct {
	mu      sync.Mutex
	actions []systemactions.Action
}

func (e *recordingExec) Execute(_ context.Context, action systemactions.Action) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.actions = append(e.actions, action)
	return nil
}

func (e *recordingExec) recorded() []systemactions.Action {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]systemactions.Action(nil), e.actions...)
}

// uploadArchive legt ein Archiv als Import-Upload an und prüft, dass die Sitzung steht.
func (f *backupFixture) uploadArchive(archive []byte, who backupUser) {
	f.t.Helper()
	rec := f.importRequest("/api/v1/backup/import", "application/octet-stream", "", archive, who)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("import status %d: %s", rec.Code, rec.Body.String())
	}
}

// preImportArchives liefert die Sicherungen vor einem Einspielen, neueste zuerst.
func (f *backupFixture) preImportArchives() []backup.Info {
	f.t.Helper()
	store, err := backup.NewStore(f.backupDir)
	if err != nil {
		f.t.Fatal(err)
	}
	all, err := store.List()
	if err != nil {
		f.t.Fatal(err)
	}
	var out []backup.Info
	for _, info := range all {
		if info.Trigger == "pre_import" {
			out = append(out, info)
		}
	}
	return out
}

func TestApplyStopsServicesThenRestarts(t *testing.T) {
	f := newBackupFixture(t)
	importer := f.user("importer", auth.RoleBackupImport)
	exec := &recordingExec{}
	f.deps.Exec = exec
	f.uploadArchive(importArchive(t, f, "1.0.0", ""), importer)

	rec := f.importRequest(backupApplyPath, "application/json", "", []byte(`{"areas":{"settings":true}}`), importer)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("apply status %d: %s", rec.Code, rec.Body.String())
	}
	f.waitIdle()

	want := []systemactions.Action{systemactions.StopServices, systemactions.RestartDashboard}
	if got := exec.recorded(); !slices.Equal(got, want) {
		t.Fatalf("actions %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, "backup-pending", "pending.json")); err != nil {
		t.Fatalf("pending.json missing after restart: %v", err)
	}
	if job := f.jobs.Status(); job.Error != "" || job.Result == "" {
		t.Fatalf("job %+v, want result and no error", job)
	}
}

func TestApplyFailureBeforeRestartRestartsServices(t *testing.T) {
	f := newBackupFixture(t)
	importer := f.user("importer", auth.RoleBackupImport)
	exec := &recordingExec{}
	f.deps.Exec = exec
	f.deps.Location = nil
	f.uploadArchive(importArchive(t, f, "1.0.0", ""), importer)

	rec := f.importRequest(backupApplyPath, "application/json", "", []byte(`{"areas":{"settings":true}}`), importer)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("apply status %d: %s", rec.Code, rec.Body.String())
	}
	f.waitIdle()

	job := f.jobs.Status()
	if job.Error != "backup_no_location" {
		t.Fatalf("job error %q, want backup_no_location", job.Error)
	}
	want := []systemactions.Action{systemactions.StartServices}
	if got := exec.recorded(); !slices.Equal(got, want) {
		t.Fatalf("actions %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, "backup-pending")); err == nil {
		t.Fatal("backup-pending left behind after failure")
	}
	settings, err := os.ReadFile(filepath.Join(f.dataDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(settings) != `{"theme":"hell"}` {
		t.Fatalf("live settings changed: %s", settings)
	}
}

func TestPreImportKeepsSecretsOnlyWhenReplaced(t *testing.T) {
	f := newBackupFixture(t)
	keeper := f.user("keeper", auth.RoleBackupImport, auth.RoleBackupSecrets)
	exec := &recordingExec{}
	f.deps.Exec = exec

	// Ohne Zugangsdaten im Auswahl bleibt die Sicherung ohne Zugangsdaten.
	f.uploadArchive(importArchive(t, f, "1.0.0", ""), keeper)
	if rec := f.importRequest(backupApplyPath, "application/json", "", []byte(`{"areas":{"settings":true}}`), keeper); rec.Code != http.StatusAccepted {
		t.Fatalf("settings apply status %d: %s", rec.Code, rec.Body.String())
	}
	f.waitIdle()
	archives := f.preImportArchives()
	if len(archives) != 1 || archives[0].Secrets {
		t.Fatalf("settings only: pre-import archives %+v, want one without secrets", archives)
	}

	// Mit Zugangsdaten im Archiv und im Auswahl ist die Sicherung mit Zugangsdaten.
	if err := os.WriteFile(filepath.Join(f.dataDir, "mqtt_credentials.json"), []byte(`{"user":"node"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	opts := backup.CreateOptions{
		Areas:       map[backup.Area]bool{backup.AreaSettings: true, backup.AreaSecrets: true},
		Secrets:     true,
		Trigger:     "manual",
		NodeVersion: "1.0.0",
	}
	if _, err := backup.Create(context.Background(), backup.Sources{DataDir: f.dataDir, DevicesDir: t.TempDir()}, opts, &buf); err != nil {
		t.Fatal(err)
	}
	f.uploadArchive(buf.Bytes(), keeper)
	rec := f.importRequest(backupApplyPath, "application/json", "", []byte(`{"areas":{"settings":true,"secrets":true}}`), keeper)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("secrets apply status %d: %s", rec.Code, rec.Body.String())
	}
	f.waitIdle()
	archives = f.preImportArchives()
	if len(archives) != 2 || !archives[0].Secrets {
		t.Fatalf("secrets replaced: pre-import archives %+v, want newest with secrets", archives)
	}
}

func TestApplyNeedsSecretsRoleForSecrets(t *testing.T) {
	f := newBackupFixture(t)
	importer := f.user("importer", auth.RoleBackupImport)
	exec := &recordingExec{}
	f.deps.Exec = exec
	f.uploadArchive(importArchive(t, f, "1.0.0", ""), importer)

	rec := f.importRequest(backupApplyPath, "application/json", "", []byte(`{"areas":{"secrets":true}}`), importer)
	if rec.Code != http.StatusForbidden || backupErrorCode(t, rec) != "backup_forbidden" {
		t.Fatalf("status %d body %s, want 403 backup_forbidden", rec.Code, rec.Body.String())
	}
	f.waitIdle()
	if got := exec.recorded(); len(got) != 0 {
		t.Fatalf("actions %v after refused apply, want none", got)
	}
}

func TestPreImportKeepsOnlyThreeNewest(t *testing.T) {
	f := newBackupFixture(t)
	importer := f.user("importer", auth.RoleBackupImport)
	f.deps.Exec = &recordingExec{}
	manual := f.storeArchive()

	for i := 0; i < 4; i++ {
		f.uploadArchive(importArchive(t, f, "1.0.0", ""), importer)
		if rec := f.importRequest(backupApplyPath, "application/json", "", []byte(`{"areas":{"settings":true}}`), importer); rec.Code != http.StatusAccepted {
			t.Fatalf("apply %d status %d: %s", i, rec.Code, rec.Body.String())
		}
		f.waitIdle()
	}
	if got := len(f.preImportArchives()); got != 3 {
		t.Fatalf("%d pre-import archives, want 3", got)
	}
	store, err := backup.NewStore(f.backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Open(manual); err != nil {
		t.Fatalf("manual archive pruned: %v", err)
	}
}

func TestImportResultRoute(t *testing.T) {
	f := newBackupFixture(t)
	importer := f.user("importer", auth.RoleBackupImport)

	empty := f.call(http.MethodGet, "/api/v1/backup/import/result", "", importer, "")
	if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != "{}" {
		t.Fatalf("no result: status %d body %s, want 200 {}", empty.Code, empty.Body.String())
	}

	written := `{"ok":true,"applied":["settings"],"finished_at":"2026-11-08T10:00:00Z"}`
	if err := os.WriteFile(filepath.Join(f.dataDir, "backup-result.json"), []byte(written), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := f.call(http.MethodGet, "/api/v1/backup/import/result", "", importer, "")
	var result struct {
		OK      bool          `json:"ok"`
		Applied []backup.Area `json:"applied"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !result.OK || len(result.Applied) != 1 {
		t.Fatalf("result: status %d body %s", rec.Code, rec.Body.String())
	}
}
