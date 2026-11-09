package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/backup"
)

// writeCLIConfig schreibt eine vollstaendige Konfiguration mit den
// angegebenen Verzeichnissen. Grundlage ist die Vorlage aus appconfig.
func writeCLIConfig(t *testing.T, dataDir, devicesDir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "appconfig", "energy-node.config.json"))
	if err != nil {
		t.Fatalf("Vorlage lesen: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Vorlage ist kein JSON: %v", err)
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatalf("Vorlage ohne paths")
	}
	paths["data_dir"] = dataDir
	paths["devices_dir"] = devicesDir
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("schreiben: %v", err)
	}
	return path
}

func cliDirs(t *testing.T) (dataDir, devicesDir string) {
	t.Helper()
	dataDir = t.TempDir()
	devicesDir = t.TempDir()
	return dataDir, devicesDir
}

func TestCLICreateWritesArchive(t *testing.T) {
	dataDir, devicesDir := cliDirs(t)
	cfgPath := writeCLIConfig(t, dataDir, devicesDir)
	var stdout, stderr bytes.Buffer
	code := runBackupCLI([]string{"create", "--config", cfgPath, "--trigger", "update"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, erwartet 0; stderr: %q", code, stderr.String())
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, "backups"))
	if err != nil {
		t.Fatalf("backups lesen: %v", err)
	}
	var archives, sidecars int
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), ".tar.gz"):
			archives++
		case strings.HasSuffix(e.Name(), ".json"):
			sidecars++
		}
	}
	if archives != 1 || sidecars != 1 {
		t.Fatalf("archives=%d sidecars=%d, erwartet je 1", archives, sidecars)
	}
	id := strings.TrimSpace(stdout.String())
	if id == "" || !strings.HasSuffix(id, "-update") {
		t.Fatalf("stdout = %q, erwartet eine Archiv-ID mit Suffix -update", stdout.String())
	}
}

func TestCLISkipsWhenBeforeUpdateOff(t *testing.T) {
	dataDir, devicesDir := cliDirs(t)
	settings := backup.DefaultSettings()
	settings.BeforeUpdate = false
	if err := backup.SaveSettings(dataDir, settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	cfgPath := writeCLIConfig(t, dataDir, devicesDir)
	var stdout, stderr bytes.Buffer
	code := runBackupCLI([]string{"create", "--config", cfgPath, "--trigger", "update"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("exit %d, erwartet 3; stderr: %q", code, stderr.String())
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, "backups"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("backups lesen: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("%d Dateien in backups, erwartet keine", len(entries))
	}
}

func TestCLIRejectsUnknownTrigger(t *testing.T) {
	dataDir, devicesDir := cliDirs(t)
	cfgPath := writeCLIConfig(t, dataDir, devicesDir)
	var stdout, stderr bytes.Buffer
	code := runBackupCLI([]string{"create", "--config", cfgPath, "--trigger", "manual"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit %d, erwartet 2; stderr: %q", code, stderr.String())
	}
}
