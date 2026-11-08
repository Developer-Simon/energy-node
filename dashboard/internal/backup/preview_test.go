package backup

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeFile legt base/rel mit dem Inhalt body an.
func writeFile(t *testing.T, base, rel, body string) {
	t.Helper()
	p := filepath.Join(base, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// previewFixture liefert ein leeres Staging-Verzeichnis und einen leeren Live-Knoten.
func previewFixture(t *testing.T) (string, Sources) {
	t.Helper()
	root := t.TempDir()
	staging := filepath.Join(root, "staging")
	if err := os.MkdirAll(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	src := Sources{
		DataDir:       filepath.Join(root, "live", "data"),
		DevicesDir:    filepath.Join(root, "live", "devices"),
		BatteryStates: map[string]string{},
	}
	return staging, src
}

func findPreview(t *testing.T, previews []AreaPreview, area Area) AreaPreview {
	t.Helper()
	for _, p := range previews {
		if p.Area == area {
			return p
		}
	}
	t.Fatalf("Bereich %s fehlt in der Vorschau", area)
	return AreaPreview{}
}

func TestPreviewListsChangedNewRemoved(t *testing.T) {
	staging, src := previewFixture(t)
	writeFile(t, staging, "data/settings.json", `{"theme":"light"}`)
	writeFile(t, staging, "data/energy.json", `{"a":1}`)
	writeFile(t, src.DataDir, "settings.json", `{"theme":"dark"}`)
	writeFile(t, src.DataDir, "mqtt.json", `{"b":1}`)

	previews, err := Preview(staging, src)
	if err != nil {
		t.Fatal(err)
	}
	got := findPreview(t, previews, AreaSettings)
	want := []Change{
		{Kind: "new", Label: "data/energy.json"},
		{Kind: "del", Label: "data/mqtt.json"},
		{Kind: "mod", Label: "data/settings.json"},
	}
	if !reflect.DeepEqual(got.Changes, want) {
		t.Fatalf("Changes = %+v, want %+v", got.Changes, want)
	}
	if !got.Available || got.Count != 2 {
		t.Fatalf("Available=%v Count=%d, want true and 2", got.Available, got.Count)
	}
	if wantSize := int64(len(`{"theme":"light"}`) + len(`{"a":1}`)); got.Size != wantSize {
		t.Fatalf("Size = %d, want %d", got.Size, wantSize)
	}
}

func TestPreviewIgnoresKeyOrder(t *testing.T) {
	staging, src := previewFixture(t)
	writeFile(t, staging, "data/settings.json", `{"a":1,"b":{"c":2,"d":[1,2]}}`)
	writeFile(t, src.DataDir, "settings.json", `{"b":{"d":[1,2],"c":2},"a":1}`)

	previews, err := Preview(staging, src)
	if err != nil {
		t.Fatal(err)
	}
	if got := findPreview(t, previews, AreaSettings); len(got.Changes) != 0 {
		t.Fatalf("Changes = %+v, want none", got.Changes)
	}
}

func TestPreviewNamesDevicesAndRules(t *testing.T) {
	staging, src := previewFixture(t)
	writeFile(t, staging, "devices/meter_devices.json", `[{"id":"m1","name":"Zähler neu"},{"id":"m2","name":"Neu"}]`)
	writeFile(t, src.DevicesDir, "meter_devices.json", `[{"id":"m1","name":"Zähler"},{"id":"m3","name":"Alt"}]`)
	writeFile(t, staging, "devices/automation_rules.json", `{"rules":[{"id":"r1","name":"Boiler bei Überschuss"}],"scenarios":[]}`)
	writeFile(t, src.DevicesDir, "automation_rules.json", `{"rules":[{"id":"r1","name":"Boiler"},{"id":"r2","name":"Alt"}],"scenarios":[]}`)

	previews, err := Preview(staging, src)
	if err != nil {
		t.Fatal(err)
	}
	got := findPreview(t, previews, AreaDevices)
	want := []Change{
		{Kind: "mod", Label: "Regel", Name: "Boiler bei Überschuss"},
		{Kind: "del", Label: "Regel", Name: "Alt"},
		{Kind: "mod", Label: "Gerät", Name: "Zähler neu"},
		{Kind: "new", Label: "Gerät", Name: "Neu"},
		{Kind: "del", Label: "Gerät", Name: "Alt"},
	}
	if !reflect.DeepEqual(got.Changes, want) {
		t.Fatalf("Changes = %+v, want %+v", got.Changes, want)
	}
}

func TestPreviewComparesLayoutPages(t *testing.T) {
	staging, src := previewFixture(t)
	writeFile(t, staging, "data/layout.json", `{"version":1,"pages":[{"id":"oben","name":"Übersicht"},{"id":"keller","name":"Keller"}]}`)
	writeFile(t, src.DataDir, "layout.json", `{"version":1,"pages":[{"id":"oben","name":"Start"}]}`)

	previews, err := Preview(staging, src)
	if err != nil {
		t.Fatal(err)
	}
	got := findPreview(t, previews, AreaSettings)
	want := []Change{
		{Kind: "new", Label: "Seite", Name: "Keller"},
		{Kind: "mod", Label: "Seite", Name: "Übersicht"},
	}
	if !reflect.DeepEqual(got.Changes, want) {
		t.Fatalf("Changes = %+v, want %+v", got.Changes, want)
	}
}

func TestPreviewMarksMissingAreasUnavailable(t *testing.T) {
	staging, src := previewFixture(t)
	writeFile(t, staging, "data/settings.json", `{"theme":"dark"}`)
	writeFile(t, src.DataDir, "mqtt_credentials.json", `{"password":"geheim"}`)

	previews, err := Preview(staging, src)
	if err != nil {
		t.Fatal(err)
	}
	secrets := findPreview(t, previews, AreaSecrets)
	if secrets.Available || secrets.Changes != nil || secrets.Count != 0 {
		t.Fatalf("Secrets = %+v, want unavailable without changes", secrets)
	}
	if !findPreview(t, previews, AreaSettings).Available {
		t.Fatal("Einstellungen sind im Archiv und müssen verfügbar sein")
	}
}
