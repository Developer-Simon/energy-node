package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestComposedSchemaIsCommitted is the CI half of the drift guard: go test
// ./... (job dashboard-go) fails when config.schema.json no longer matches the
// fragments. Locally, run the same check via "go run ./cmd/schemagen -check".
func TestComposedSchemaIsCommitted(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	want, err := compose(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(composedSchemaPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("config.schema.json is stale — run: (cd dashboard && go run ./cmd/schemagen)")
	}
}

// TestComposeSplicesExactlyTheManifestedServices builds a throwaway repo layout
// with a minimal core and two service dirs and asserts compose() adds exactly
// those two branches — one manifest in, one services.<id> branch out.
func TestComposeSplicesExactlyTheManifestedServices(t *testing.T) {
	root := t.TempDir()
	mkdir(t, filepath.Join(root, "dashboard", "cmd", "schemagen"))
	mkdir(t, filepath.Join(root, "dashboard", "internal", "appconfig"))

	core := `{
  "type": "object",
  "additionalProperties": false,
  "required": ["services"],
  "properties": {
    "services": {"type": "object", "additionalProperties": false, "required": [], "properties": {}}
  }
}`
	write(t, filepath.Join(root, "dashboard", "cmd", "schemagen", "core.schema.json"), core)

	writeService(t, root, "alpha_svc", "alpha", []string{"service_id", "rate"})
	writeService(t, root, "beta_svc", "beta", []string{"service_id"})
	// A service dir without a manifest must be ignored.
	mkdir(t, filepath.Join(root, "services", "no_manifest"))

	out, err := compose(root)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("compose output is not JSON: %v", err)
	}
	services := doc["properties"].(map[string]any)["services"].(map[string]any)

	gotRequired := services["required"].([]any)
	if len(gotRequired) != 2 || gotRequired[0] != "alpha" || gotRequired[1] != "beta" {
		t.Fatalf("services.required = %v, want [alpha beta] (sorted)", gotRequired)
	}
	props := services["properties"].(map[string]any)
	if _, ok := props["alpha"]; !ok {
		t.Fatalf("services.properties has no alpha branch: %v", props)
	}
	if _, ok := props["beta"]; !ok {
		t.Fatalf("services.properties has no beta branch: %v", props)
	}
	if len(props) != 2 {
		t.Fatalf("services.properties has %d branches, want 2", len(props))
	}
	if string(out[len(out)-1]) != "\n" {
		t.Fatalf("compose output must end with a newline")
	}
}

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeService(t *testing.T, root, dir, id string, required []string) {
	t.Helper()
	sd := filepath.Join(root, "services", dir)
	mkdir(t, sd)
	man, _ := json.Marshal(map[string]any{
		"service_id": id, "unit": id + ".service", "schema": "config.schema.json", "required": required,
	})
	write(t, filepath.Join(sd, "manifest.json"), string(man))
	frag, _ := json.Marshal(map[string]any{
		"type": "object", "additionalProperties": false, "required": required,
		"properties": map[string]any{"service_id": map[string]any{"type": "string", "minLength": 1}},
	})
	write(t, filepath.Join(sd, "config.schema.json"), string(frag))
}
