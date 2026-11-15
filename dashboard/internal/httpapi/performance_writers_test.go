package httpapi

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
)

func TestSaveSystemConfigReturnsRestartSorted(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	template, err := os.ReadFile(filepath.Join("..", "appconfig", "energy-node.config.json"))
	if err != nil {
		t.Fatalf("Vorlage lesen: %v", err)
	}
	if err := os.WriteFile(configPath, template, 0o664); err != nil {
		t.Fatal(err)
	}
	body := documentWith(t, configPath, func(document map[string]any) {
		document["paths"].(map[string]any)["data_dir"] = "/home/energynode/other"
		document["mqtt"].(map[string]any)["host"] = "broker.local"
	})

	result, err := saveSystemConfig(context.Background(), systemConfigDeps{
		Path:    configPath,
		DataDir: t.TempDir(),
	}, []byte(body))
	if err != nil {
		t.Fatalf("saveSystemConfig: %v", err)
	}
	if len(result.Restart) == 0 {
		t.Fatal("Restart ist leer, erwartet mqtt und paths")
	}
	if !sort.StringsAreSorted(result.Restart) {
		t.Fatalf("Restart nicht sortiert: %v", result.Restart)
	}
}

func TestSaveSystemConfigSchemaViolation(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	template, err := os.ReadFile(filepath.Join("..", "appconfig", "energy-node.config.json"))
	if err != nil {
		t.Fatalf("Vorlage lesen: %v", err)
	}
	if err := os.WriteFile(configPath, template, 0o664); err != nil {
		t.Fatal(err)
	}

	_, err = saveSystemConfig(context.Background(), systemConfigDeps{
		Path:    configPath,
		DataDir: t.TempDir(),
	}, []byte("{}"))

	var saveErr *saveError
	if !errors.As(err, &saveErr) {
		t.Fatalf("Fehler ist kein *saveError: %v", err)
	}
	if saveErr.Code != "schema_violation" || saveErr.Status != http.StatusBadRequest {
		t.Fatalf("Code %q Status %d, erwartet schema_violation 400", saveErr.Code, saveErr.Status)
	}
}

func TestSaveConfigurationRejectsInvalid(t *testing.T) {
	configDir := t.TempDir()
	document := `{"version":1,"settings":{},"rules":[{"id":"r1","enabled":true,"name":"x"}],"scenarios":[]}`
	if err := os.WriteFile(filepath.Join(configDir, "automation_rules.json"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "automation_rules.schema.json"), []byte(`{"type":"object"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := saveConfiguration(config.NewManager(configDir), "automation_rules", []byte("not json"))
	if err == nil {
		t.Fatal("ungueltiges automation_rules-Dokument wurde angenommen")
	}
}
