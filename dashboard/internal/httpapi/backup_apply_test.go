package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	// fail liefert den Fehler, den eine Aktion zurückgibt.
	fail map[systemactions.Action]error
}

func (e *recordingExec) Execute(_ context.Context, action systemactions.Action) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.actions = append(e.actions, action)
	return e.fail[action]
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

// envTestConfig ist eine vollständige Konfiguration mit dem MQTT-Host host, wie sie das Einspielen zusammenführt.
func envTestConfig(host string) string {
	return strings.Replace(`{
  "schema_version": 2,
  "mqtt": {"host": "127.0.0.1", "port": 1883, "username": "energynode_client", "password_file": "/etc/energy-node/mqtt.pw"},
  "paths": {"devices_dir": "/home/energynode/devices", "data_dir": "/home/energynode/dashboard/data"},
  "logging": {"level": "INFO"},
  "services": {
    "apsystems": {"service_id": "apsystems", "poll_interval_s": 60, "diagnostic_poll_multiplier": 10},
    "shelly": {"service_id": "shelly", "poll_interval_s": 20, "diagnostic_poll_multiplier": 15, "http_timeout_s": 5},
    "trucki": {"service_id": "trucki", "poll_interval_s": 30, "diagnostic_poll_multiplier": 10, "http_timeout_s": 5},
    "tuya": {"service_id": "tuya", "poll_interval_s": 30, "diagnostic_poll_multiplier": 1},
    "battery_soc": {"service_id": "battery_soc", "poll_interval_s": 10, "diagnostic_poll_multiplier": 1},
    "automation": {"service_id": "automation"}
  },
  "dashboard": {
    "bind_address": "0.0.0.0", "port": 8080, "client_id": "energy-node-dashboard",
    "device_identifier": "energy-node-dashboard", "log_level": "info", "sweep_interval_seconds": 300,
    "admin_username": "admin", "admin_password_file": "/etc/energy-node-dashboard/auth.pw",
    "tls": {"cert_file": "", "key_file": ""},
    "system_action_helper": "/usr/local/sbin/energy-node-dashboard-system-action",
    "mosquitto_bridge_target": "/etc/mosquitto/conf.d/bridge.conf",
    "node_device_id": "energy_node", "node_device_name": "Energy Node",
    "node_poll_interval_s": 60, "node_diagnostic_poll_multiplier": 10
  },
  "tailscale": {"bin": "/usr/sbin/tailscale", "status_timeout_s": 10},
  "tinytuya": {"probe_python": "/usr/bin/python3", "probe_script": "/home/energynode/tuya_mqtt/tinytuya_probe.py", "probe_timeout_s": 30}
}`, `"host": "127.0.0.1"`, `"host": "`+host+`"`, 1)
}

func TestApplyStagesEnvironmentBetweenStopAndRestart(t *testing.T) {
	f := newBackupFixture(t)
	importer := f.user("importer", auth.RoleBackupImport)
	exec := &recordingExec{}
	f.deps.Exec = exec

	dir := t.TempDir()
	archiveCfg := filepath.Join(dir, "archive.json")
	liveCfg := filepath.Join(dir, "live.json")
	if err := os.WriteFile(archiveCfg, []byte(envTestConfig("10.0.0.5")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(liveCfg, []byte(envTestConfig("127.0.0.1")), 0o600); err != nil {
		t.Fatal(err)
	}
	f.deps.Sources.AppConfigPath = liveCfg
	var buf bytes.Buffer
	opts := backup.CreateOptions{Areas: map[backup.Area]bool{backup.AreaSettings: true}, Trigger: "manual", NodeVersion: "1.0.0"}
	sources := backup.Sources{DataDir: f.dataDir, DevicesDir: t.TempDir(), AppConfigPath: archiveCfg}
	if _, err := backup.Create(context.Background(), sources, opts, &buf); err != nil {
		t.Fatal(err)
	}
	f.uploadArchive(buf.Bytes(), importer)

	rec := f.importRequest(backupApplyPath, "application/json", "", []byte(`{"areas":{"settings":true},"use_archive":{"broker":true}}`), importer)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("apply status %d: %s", rec.Code, rec.Body.String())
	}
	f.waitIdle()
	if job := f.jobs.Status(); job.Error != "" {
		t.Fatalf("job error %q", job.Error)
	}
	want := []systemactions.Action{systemactions.StopServices, systemactions.ApplyAppConfig, systemactions.RestartDashboard}
	if got := exec.recorded(); !slices.Equal(got, want) {
		t.Fatalf("actions %v, want %v", got, want)
	}
}

func TestApplyRestartFailureRestartsServices(t *testing.T) {
	f := newBackupFixture(t)
	importer := f.user("importer", auth.RoleBackupImport)
	exec := &recordingExec{fail: map[systemactions.Action]error{systemactions.RestartDashboard: errors.New("systemd busy")}}
	f.deps.Exec = exec
	f.uploadArchive(importArchive(t, f, "1.0.0", ""), importer)

	rec := f.importRequest(backupApplyPath, "application/json", "", []byte(`{"areas":{"settings":true}}`), importer)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("apply status %d: %s", rec.Code, rec.Body.String())
	}
	f.waitIdle()

	if job := f.jobs.Status(); job.Error == "" {
		t.Fatal("job error empty after failed restart")
	}
	want := []systemactions.Action{systemactions.StopServices, systemactions.RestartDashboard, systemactions.StartServices}
	if got := exec.recorded(); !slices.Equal(got, want) {
		t.Fatalf("actions %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, "backup-pending")); err == nil {
		t.Fatal("backup-pending left behind after failed restart")
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
