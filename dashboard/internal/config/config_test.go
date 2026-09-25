package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerValidatesAndRevisionizesSave(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "devices.schema.json"), []byte(`{"type":"object","required":["name"],"additionalProperties":false,"properties":{"name":{"type":"string","minLength":1}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "devices.json")
	if err := os.WriteFile(path, []byte(`{"name":"old"}`), 0600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(dir)
	if _, err := manager.Save("devices", []byte(`{"name":""}`)); err == nil {
		t.Fatal("expected invalid value to be rejected")
	}
	content, _ := os.ReadFile(path)
	if string(content) != `{"name":"old"}` {
		t.Fatalf("invalid save changed file: %s", content)
	}
	if _, err := manager.Save("devices", []byte(`{"name":"new"}`)); err != nil {
		t.Fatal(err)
	}
	revisions, err := manager.Revisions("devices")
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 {
		t.Fatalf("got %d revisions, want 1", len(revisions))
	}
}

func TestManagerExcludeNameHidesDocumentFromScanAndAPI(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shelly_presets.schema.json"), []byte(`{"type":"array"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shelly_presets.json"), []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devices.schema.json"), []byte(`{"type":"object"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devices.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}

	manager := NewManager(dir)
	manager.ExcludeName("shelly_presets")

	documents, err := manager.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) != 1 || documents[0].Name != "devices" {
		t.Fatalf("got %#v, want only the devices document", documents)
	}
	if _, err := manager.Read("shelly_presets"); err == nil {
		t.Fatal("expected excluded document to be unreadable through the generic API")
	}
}

func TestScanAppliesDisplayNamesWithFallbackToFileName(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"shelly_devices", "unmapped_devices"} {
		if err := os.WriteFile(filepath.Join(dir, name+".schema.json"), []byte(`{"type":"object"}`), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
	}

	manager := NewManager(dir)
	documents, err := manager.Scan()
	if err != nil {
		t.Fatal(err)
	}

	labels := make(map[string]string, len(documents))
	for _, document := range documents {
		labels[document.Name] = document.Label
	}
	if labels["shelly_devices"] != "Shelly Geräte" {
		t.Fatalf("got label %q for shelly_devices, want the mapped display name", labels["shelly_devices"])
	}
	if labels["unmapped_devices"] != "unmapped_devices" {
		t.Fatalf("got label %q for unmapped_devices, want fallback to the file name", labels["unmapped_devices"])
	}
}

func TestManagerReportsReloadFailureAfterAtomicSave(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "devices.schema.json"), []byte(`{"type":"object"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devices.json"), []byte(`{"name":"old"}`), 0600); err != nil {
		t.Fatal(err)
	}

	manager := NewManager(dir)
	manager.SetReloadFunc(func(string, []byte) error { return os.ErrInvalid })
	document, err := manager.Save("devices", []byte(`{"name":"new"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !document.ReloadFailed || document.ReloadError == "" {
		t.Fatalf("got reload status %#v, want a reported failure", document)
	}
	content, err := os.ReadFile(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != `{"name":"new"}` {
		t.Fatalf("saved content is %s", content)
	}
}

// The dashboard's own validator is the only feedback the user gets on a bad
// value: a config the service later rejects is still reported as "reloaded"
// through the fire-and-forget MQTT reload.
func TestValidateDocumentEnforcesNumericAndArrayBounds(t *testing.T) {
	cases := []struct {
		name    string
		schema  string
		data    string
		wantErr bool
	}{
		{"exclusiveMinimum rejects the bound", `{"type":"number","exclusiveMinimum":0}`, `0`, true},
		{"exclusiveMinimum rejects below", `{"type":"number","exclusiveMinimum":0}`, `-1`, true},
		{"exclusiveMinimum accepts above", `{"type":"number","exclusiveMinimum":0}`, `0.5`, false},
		{"exclusiveMaximum rejects the bound", `{"type":"number","exclusiveMaximum":1}`, `1`, true},
		{"exclusiveMaximum accepts below", `{"type":"number","exclusiveMaximum":1}`, `0.98`, false},
		{"multipleOf rejects off-grid", `{"type":"number","multipleOf":0.5}`, `0.75`, true},
		{"multipleOf accepts on-grid", `{"type":"number","multipleOf":0.5}`, `1.5`, false},
		{"minItems rejects empty array", `{"type":"array","minItems":1}`, `[]`, true},
		{"minItems accepts one entry", `{"type":"array","minItems":1}`, `[1]`, false},
		{"maxItems rejects overflow", `{"type":"array","maxItems":1}`, `[1,2]`, true},
		// numberValue() returns 0 for non-numbers, so a naive port of the
		// minimum check would fail a string against exclusiveMinimum: 0.
		{"exclusiveMinimum ignores strings", `{"exclusiveMinimum":0}`, `"text"`, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := ValidateDocument([]byte(testCase.data), []byte(testCase.schema))
			if testCase.wantErr && err == nil {
				t.Fatalf("expected %s against %s to be rejected", testCase.data, testCase.schema)
			}
			if !testCase.wantErr && err != nil {
				t.Fatalf("expected %s against %s to be accepted: %v", testCase.data, testCase.schema, err)
			}
		})
	}
}

func TestValidateDocumentReportsNestedPath(t *testing.T) {
	schema := []byte(`{"type":"array","minItems":1,"items":{"type":"object","properties":{"charge_efficiency":{"type":"number","exclusiveMinimum":0,"maximum":1}}}}`)
	err := ValidateDocument([]byte(`[{"charge_efficiency":0}]`), schema)
	if err == nil {
		t.Fatal("expected charge_efficiency 0 to be rejected")
	}
	if !strings.Contains(err.Error(), "$[0].charge_efficiency") {
		t.Fatalf("error does not name the offending path: %v", err)
	}
	if err := ValidateDocument([]byte(`[]`), schema); err == nil {
		t.Fatal("expected the empty array to be rejected by minItems")
	}
}

func TestScanReportsPersistedReloadFailure(t *testing.T) {
	dir := t.TempDir()
	schema := []byte(`{"type":"object","properties":{"value":{"type":"integer"}}}`)
	if err := os.WriteFile(filepath.Join(dir, "demo.schema.json"), schema, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "demo.json"), []byte(`{"value":0}`), 0640); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(dir)
	manager.SetReloadFunc(func(string, []byte) error { return errors.New("bridge antwortet nicht") })

	if _, err := manager.Save("demo", []byte(`{"value":1}`)); err != nil {
		t.Fatalf("save should persist the file even when the reload fails: %v", err)
	}

	// Der Kern der Aufgabe: der Zustand ueberlebt den naechsten Listenaufruf.
	documents, err := manager.Scan()
	if err != nil {
		t.Fatal(err)
	}
	var demo Document
	for _, document := range documents {
		if document.Name == "demo" {
			demo = document
		}
	}
	if !demo.ReloadFailed {
		t.Fatalf("scan lost the reload failure: %#v", demo)
	}
	if !strings.Contains(demo.ReloadError, "bridge antwortet nicht") {
		t.Fatalf("reload error %q does not carry the cause", demo.ReloadError)
	}

	// Ein geglueckter Reload muss den Zustand wieder aufheben.
	manager.SetReloadFunc(func(string, []byte) error { return nil })
	if _, err := manager.Save("demo", []byte(`{"value":2}`)); err != nil {
		t.Fatal(err)
	}
	documents, err = manager.Scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range documents {
		if document.Name == "demo" && document.ReloadFailed {
			t.Fatalf("a successful reload did not clear the failure: %#v", document)
		}
	}
}

func TestConfigurationRevisionsAreCappedAtLimit(t *testing.T) {
	dir := t.TempDir()
	schema := []byte(`{"type":"object","properties":{"value":{"type":"integer"}}}`)
	if err := os.WriteFile(filepath.Join(dir, "demo.schema.json"), schema, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "demo.json"), []byte(`{"value":0}`), 0640); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(dir)
	manager.SetReloadFunc(func(string, []byte) error { return nil })

	for i := 1; i <= revisionLimit+3; i++ {
		if _, err := manager.Save("demo", []byte(fmt.Sprintf(`{"value":%d}`, i))); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	revisions, err := manager.Revisions("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != revisionLimit {
		t.Fatalf("got %d revisions, want the limit of %d", len(revisions), revisionLimit)
	}
	// Die aeltesten drei sind weggeschnitten: die verbliebene aelteste
	// Revision haelt den Stand vor dem vierten Speichervorgang, also value:3.
	data, err := manager.ReadRevision("demo", revisions[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"value":3`) {
		t.Fatalf("oldest kept revision is %s, want value:3", data)
	}
}

const conditionalSchema = `{
  "type": "object",
  "unevaluatedProperties": false,
  "properties": {
    "kind": {"type": "string", "enum": ["a", "b"], "default": "a"},
    "shared": {"type": "string"},
    "extra": {"type": "boolean", "default": true}
  },
  "allOf": [
    {"if": {"properties": {"kind": {"const": "a"}}},
     "then": {"properties": {"only_a": {"type": "integer"}}},
     "else": {"properties": {"only_b": {"type": "integer"}}}},
    {"if": {"properties": {"extra": {"const": true}}},
     "then": {
       "properties": {"mode": {"type": "string", "enum": ["x", "y"]}},
       "allOf": [
         {"if": {"required": ["mode"], "properties": {"mode": {"const": "y"}}},
          "then": {"properties": {"only_y": {"type": "number", "minimum": 1}}}}
       ]}}
  ]
}`

func TestConditionalSchemaAcceptsFieldsOfActiveBranches(t *testing.T) {
	for _, doc := range []string{
		`{"only_a": 1}`,
		`{"kind": "a", "only_a": 1, "shared": "s"}`,
		`{"kind": "b", "only_b": 2}`,
		`{"mode": "y", "only_y": 3}`,
		`{"extra": true, "mode": "x"}`,
	} {
		if err := ValidateDocument([]byte(doc), []byte(conditionalSchema)); err != nil {
			t.Errorf("%s: unexpected error %v", doc, err)
		}
	}
}

func TestConditionalSchemaRejectsFieldsOfInactiveBranches(t *testing.T) {
	for doc, want := range map[string]string{
		`{"kind": "b", "only_a": 1}`:    "$.only_a is not allowed",
		`{"only_b": 1}`:                 "$.only_b is not allowed",
		`{"extra": false, "mode": "x"}`: "$.mode is not allowed",
		`{"only_y": 3}`:                 "$.only_y is not allowed",
		`{"mode": "x", "only_y": 3}`:    "$.only_y is not allowed",
		`{"unknown": 1}`:                "$.unknown is not allowed",
		`{"mode": "y", "only_y": 0}`:    "$.only_y is below minimum",
	} {
		err := ValidateDocument([]byte(doc), []byte(conditionalSchema))
		if err == nil || err.Error() != want {
			t.Errorf("%s: got %v, want %q", doc, err, want)
		}
	}
}

func TestConstIsValidated(t *testing.T) {
	schema := `{"type": "object", "properties": {"v": {"const": 2}}}`
	if err := ValidateDocument([]byte(`{"v": 2}`), []byte(schema)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument([]byte(`{"v": 3}`), []byte(schema)); err == nil {
		t.Fatal("const 2 accepted 3")
	}
}

func TestUnsupportedSchemaKeywordsAreRejected(t *testing.T) {
	for _, schema := range []string{
		`{"oneOf": []}`,
		`{"anyOf": []}`,
		`{"not": {}}`,
		`{"dependentSchemas": {}}`,
		`{"properties": {"a": {"$ref": "#/x"}}}`,
		`{"allOf": [{"then": {"anyOf": []}}]}`,
		`{"additionalProperties": false, "unevaluatedProperties": false}`,
	} {
		err := ValidateDocument([]byte(`{}`), []byte(schema))
		if err == nil || !strings.HasPrefix(err.Error(), "invalid schema: ") {
			t.Errorf("%s: got %v, want an invalid schema error", schema, err)
		}
	}
}

func TestAdditionalPropertiesStillWorksWithoutConditions(t *testing.T) {
	schema := `{"type": "object", "additionalProperties": false, "properties": {"a": {"type": "string"}}}`
	if err := ValidateDocument([]byte(`{"a": "x"}`), []byte(schema)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument([]byte(`{"b": 1}`), []byte(schema)); err == nil || err.Error() != "$.b is not allowed" {
		t.Fatalf("got %v", err)
	}
}

func TestSaveAndScanReportTheFileChecksum(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x_devices.json"), []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x_devices.schema.json"), []byte(`{"type":"array"}`), 0600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(dir)
	doc, err := manager.Save("x_devices", []byte(`[{"id":"a"}]`))
	if err != nil {
		t.Fatal(err)
	}
	want := checksum([]byte(`[{"id":"a"}]`))
	if doc.Checksum != want {
		t.Fatalf("save checksum %q, want %q", doc.Checksum, want)
	}
	docs, err := manager.Scan()
	if err != nil || len(docs) != 1 || docs[0].Checksum != want {
		t.Fatalf("scan = %+v, %v", docs, err)
	}
}

func TestServiceIDForConfig(t *testing.T) {
	for name, want := range map[string]string{"battery_soc_devices": "battery_soc", "automation_rules": "automation", "shelly_devices": "shelly"} {
		if got, ok := ServiceIDForConfig(name); !ok || got != want {
			t.Errorf("%s = (%q, %v), want %q", name, got, ok, want)
		}
	}
	if _, ok := ServiceIDForConfig("shelly_presets"); ok {
		t.Error("shelly_presets is no service configuration")
	}
}
