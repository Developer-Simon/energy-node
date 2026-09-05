package shellypresets

import (
	"os"
	"path/filepath"
	"testing"
)

const testSchema = `{
  "type": "array",
  "items": {
    "type": "object",
    "required": ["id", "name", "properties"],
    "additionalProperties": false,
    "properties": {
      "id": {"type": "string", "minLength": 1},
      "name": {"type": "string", "minLength": 1},
      "properties": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "generation": {"type": "integer", "enum": [1, 2]},
          "switch_channels": {"type": "integer", "minimum": 0},
          "has_power": {"type": "boolean"},
          "has_humidity": {"type": "boolean"}
        }
      }
    }
  }
}`

func writePresets(t *testing.T, dir, presetsJSON string) *Store {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "shelly_presets.schema.json"), []byte(testSchema), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "shelly_presets.json")
	if err := os.WriteFile(path, []byte(presetsJSON), 0600); err != nil {
		t.Fatal(err)
	}
	return NewStore(path)
}

func TestStoreListReturnsValidatedPresets(t *testing.T) {
	dir := t.TempDir()
	store := writePresets(t, dir, `[
		{"id": "a", "name": "Preset A", "properties": {"generation": 1, "switch_channels": 1}},
		{"id": "b", "name": "Preset B", "properties": {"has_humidity": true}}
	]`)
	presets, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != 2 {
		t.Fatalf("got %d presets, want 2", len(presets))
	}
	if presets[0].ID != "a" || presets[0].Properties["switch_channels"] != float64(1) {
		t.Fatalf("unexpected first preset: %#v", presets[0])
	}
}

func TestStoreListRejectsDuplicateIDs(t *testing.T) {
	dir := t.TempDir()
	store := writePresets(t, dir, `[
		{"id": "a", "name": "Preset A", "properties": {}},
		{"id": "a", "name": "Preset A again", "properties": {}}
	]`)
	if _, err := store.List(); err == nil {
		t.Fatal("expected duplicate preset id to be rejected")
	}
}

func TestStoreListRejectsSchemaViolations(t *testing.T) {
	dir := t.TempDir()
	store := writePresets(t, dir, `[
		{"id": "a", "name": "Preset A", "properties": {"unknown_field": true}}
	]`)
	if _, err := store.List(); err == nil {
		t.Fatal("expected additionalProperties violation to be rejected")
	}
}

func TestStoreListFailsOnMissingFile(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "missing.json"))
	if _, err := store.List(); err == nil {
		t.Fatal("expected error for missing preset file")
	}
}
