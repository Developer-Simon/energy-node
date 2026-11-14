package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
	"github.com/Developer-Simon/energy-node-dashboard/internal/storage"
	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
)

// Ohne storage.json bleibt alles beim bisherigen Ort: kein Waechter.
func TestStorageDriveWatcherOffWithoutChoice(t *testing.T) {
	cfg := appconfig.Config{Paths: appconfig.PathsSection{RuntimeDir: t.TempDir()}}
	if w := storageDriveWatcher(cfg, t.TempDir(), nil); w != nil {
		t.Fatal("watcher built without a storage.json")
	}
}

// Eine Laufwerkswahl ohne runtime_dir startet keinen Waechter, weil der
// Symlink sonst nirgends liegen koennte.
func TestStorageDriveWatcherNeedsRuntimeDir(t *testing.T) {
	dataDir := t.TempDir()
	if err := storage.SaveConfig(dataDir, storage.Config{UseData: true, Fallback: storage.FallbackSystem}); err != nil {
		t.Fatal(err)
	}
	if w := storageDriveWatcher(appconfig.Config{}, dataDir, nil); w != nil {
		t.Fatal("watcher built without runtime_dir")
	}
}

// stagingExec stands in for the helper: it reads the staged config while the
// real helper would install it.
type stagingExec struct {
	dataDir string
	staged  []byte
	runs    int
}

func (e *stagingExec) Execute(ctx context.Context, action systemactions.Action) error {
	e.runs++
	raw, err := os.ReadFile(filepath.Join(e.dataDir, systemactions.StagedAppConfigName))
	if err != nil {
		return err
	}
	e.staged = raw
	return nil
}

// applyRuntimeDir keeps the other keys of the config and sets or removes only paths.runtime_dir.
func TestApplyRuntimeDirChangesOnlyTheKey(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"paths":{"data_dir":"/d"},"dashboard":{"port":8080}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	exec := &stagingExec{dataDir: dataDir}

	if err := applyRuntimeDir(context.Background(), exec, configPath, dataDir, "/var/lib/rt"); err != nil {
		t.Fatalf("applyRuntimeDir: %v", err)
	}
	var got map[string]map[string]any
	if err := json.Unmarshal(exec.staged, &got); err != nil {
		t.Fatalf("staged config: %v", err)
	}
	if got["paths"]["runtime_dir"] != "/var/lib/rt" || got["paths"]["data_dir"] != "/d" || got["dashboard"]["port"] != float64(8080) {
		t.Fatalf("staged config = %s", exec.staged)
	}

	if err := applyRuntimeDir(context.Background(), exec, configPath, dataDir, ""); err != nil {
		t.Fatalf("applyRuntimeDir remove: %v", err)
	}
	if err := json.Unmarshal(exec.staged, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["paths"]["runtime_dir"]; ok {
		t.Fatalf("runtime_dir kept after removal: %s", exec.staged)
	}
	if exec.runs != 2 {
		t.Fatalf("helper runs = %d, want 2", exec.runs)
	}
}

// Restarting the dashboard stops the dashboard's cgroup, which kills the sudo
// that asked for it. That signal is the restart working, not a failure.
func TestRestartKilledBySignalIsNotAnError(t *testing.T) {
	cmd := exec.Command("sh", "-c", "kill -TERM $$")
	killed := cmd.Run()
	if killed == nil {
		t.Fatal("test setup: command did not end by signal")
	}
	if !killedByRestart(killed) {
		t.Fatalf("killedByRestart(%v) = false, want true", killed)
	}
	failed := exec.Command("sh", "-c", "exit 3").Run()
	if killedByRestart(failed) {
		t.Fatalf("killedByRestart(%v) = true for a normal exit code", failed)
	}
	if killedByRestart(errors.New("sudo: not found")) || killedByRestart(nil) {
		t.Fatal("killedByRestart accepted a non-exit error")
	}
}

// Die Quellen der Systemplatte: Installation mit Wurzel, Konfig- und Installer-
// Ordner, Laufzeit und Backups, und die Ausschlüsse, die Install nicht sehen darf.
func TestStorageUsageSourcesCoverInstallAndExcludeData(t *testing.T) {
	svc := &storage.Service{
		Paths: storage.Paths{
			Runtime:        "/home/energynode/dashboard/data/runtime",
			Mount:          "/mnt/energy-node",
			SystemFallback: "/home/energynode/dashboard/data/runtime-fallback",
		},
		DataDir: "/home/energynode/dashboard/data",
	}
	src := storageUsageSources(svc, "/etc/energy-node/config.json", "/var/lib/energy-node-installer", "/home/energynode/dashboard/energy-node-dashboard")

	if src.Ref != "/home/energynode/dashboard/data" {
		t.Fatalf("Ref = %q", src.Ref)
	}
	wantInstall := []string{"/home/energynode", "/etc/energy-node", "/var/lib/energy-node-installer"}
	if !reflect.DeepEqual(src.Install, wantInstall) {
		t.Fatalf("Install = %v, want %v", src.Install, wantInstall)
	}
	wantRuntime := []string{svc.Paths.Runtime, svc.Paths.SystemFallback}
	if !reflect.DeepEqual(src.Runtime, wantRuntime) {
		t.Fatalf("Runtime = %v, want %v", src.Runtime, wantRuntime)
	}
	wantBackups := []string{"/home/energynode/dashboard/data/backups"}
	if !reflect.DeepEqual(src.Backups, wantBackups) {
		t.Fatalf("Backups = %v, want %v", src.Backups, wantBackups)
	}
	for _, want := range []string{svc.Paths.Runtime, svc.Paths.SystemFallback, wantBackups[0], svc.Paths.Mount} {
		if !containsString(src.Exclude, want) {
			t.Fatalf("Exclude = %v, misses %q", src.Exclude, want)
		}
	}
}

// Ohne Programmpfad fehlt die Installationswurzel, die anderen zwei bleiben.
func TestStorageUsageSourcesWithoutExecutable(t *testing.T) {
	svc := &storage.Service{
		Paths:   storage.Paths{Runtime: "/home/energynode/dashboard/data/runtime"},
		DataDir: "/home/energynode/dashboard/data",
	}
	src := storageUsageSources(svc, "/etc/energy-node/config.json", "/var/lib/energy-node-installer", "")
	want := []string{"/etc/energy-node", "/var/lib/energy-node-installer"}
	if !reflect.DeepEqual(src.Install, want) {
		t.Fatalf("Install = %v, want %v", src.Install, want)
	}
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
}
