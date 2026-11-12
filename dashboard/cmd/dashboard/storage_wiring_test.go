package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
