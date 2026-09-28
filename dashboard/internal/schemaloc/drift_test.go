package schemaloc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// pendingSchemas are shipped schemas whose German texts are not in the
// catalog yet. PR 2 of A5 empties and then deletes this list.
var pendingSchemas = map[string]bool{
	"battery_soc_devices": true,
}

// shippedSchemas maps every schema the dashboard renders as a form to its
// file: the composed central schema and every services/*/*.schema.json
// except the config.schema.json fragments, which live inside "system".
func shippedSchemas(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob("../../../services/*/*.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{SystemSchemaID: "../appconfig/config.schema.json"}
	for _, file := range files {
		if filepath.Base(file) == "config.schema.json" {
			continue
		}
		out[strings.TrimSuffix(filepath.Base(file), ".schema.json")] = file
	}
	return out
}

func germanCatalog(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile("catalogs/de.json")
	if err != nil {
		t.Fatal(err)
	}
	catalog := map[string]string{}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestEveryShippedSchemaTextHasAGermanEntry(t *testing.T) {
	german := germanCatalog(t)
	var missing []string
	for id, file := range shippedSchemas(t) {
		if pendingSchemas[id] {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := Entries(id, raw)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, entry := range entries {
			if _, ok := german[entry.Key]; !ok {
				line, _ := json.Marshal(map[string]string{entry.Key: entry.Text})
				missing = append(missing, "  "+strings.Trim(string(line), "{}")+",")
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("catalogs/de.json lacks %d schema texts (schema text shown, translate before adding):\n%s", len(missing), strings.Join(missing, "\n"))
	}
}

func TestGermanCatalogHasNoOrphans(t *testing.T) {
	known := map[string]bool{}
	for id, file := range shippedSchemas(t) {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := Entries(id, raw)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, entry := range entries {
			known[entry.Key] = true
		}
	}
	for key := range germanCatalog(t) {
		if !known[key] {
			t.Errorf("catalogs/de.json: %s matches no schema text (renamed or removed field?)", key)
		}
	}
}

func TestPendingSchemasExist(t *testing.T) {
	shipped := shippedSchemas(t)
	for id := range pendingSchemas {
		if _, ok := shipped[id]; !ok {
			t.Errorf("pendingSchemas lists %s, which is no longer shipped", id)
		}
	}
}

// Operator-facing texts use a comma or a full stop, never a semicolon or a
// connecting dash, in every language.
func TestSchemaTextsFollowTheStyleRules(t *testing.T) {
	bad := func(text string) bool {
		return strings.Contains(text, ";") || strings.Contains(text, " – ") || strings.Contains(text, " — ") || strings.Contains(text, " - ")
	}
	for id, file := range shippedSchemas(t) {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := Entries(id, raw)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, entry := range entries {
			if bad(entry.Text) {
				t.Errorf("%s: %s: %q", file, entry.Key, entry.Text)
			}
		}
	}
	for key, text := range germanCatalog(t) {
		if bad(text) {
			t.Errorf("catalogs/de.json: %s: %q", key, text)
		}
	}
}
