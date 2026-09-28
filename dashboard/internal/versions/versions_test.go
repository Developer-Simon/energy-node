package versions_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/versions"
)

const manifest = `{
  "version": "v0.7.5", "built_at": "2026-09-21T10:00:00+02:00", "arch": "armv6",
  "components": {"dashboard": "v0.7.5", "services": "v0.4.0"},
  "steps": [
    {"id": "10", "optional": false},
    {"id": "81", "optional": true, "dir": "apsystems_ez1", "version": "v0.4.1"},
    {"id": "83", "optional": true, "dir": "shelly", "version": "v0.4.0"}
  ]
}`

const document = `{
  "schema_version": 1, "bundle_version": "v0.7.5",
  "components": [
    {"id": "dashboard", "label": "Dashboard", "kind": "app", "version": "v0.7.5", "releases": []},
    {"id": "service:apsystems_ez1", "label": "APsystems EZ1", "kind": "service", "version": "v0.4.1", "releases": []},
    {"id": "service:shelly", "label": "Shelly", "kind": "service", "version": "v0.4.0", "releases": []}
  ]
}`

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadWithoutAnInstalledManifestIsAnEmptySnapshot(t *testing.T) {
	snap, err := versions.Load(t.TempDir(), "v0.7.5-dev")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Bundle != nil || len(snap.Components) != 0 || snap.HasChangelog {
		t.Fatalf("snapshot = %+v, want empty", snap)
	}
	if snap.Running.Dashboard != "v0.7.5-dev" {
		t.Fatalf("running = %q", snap.Running.Dashboard)
	}
}

func TestLoadTakesLabelsAndKindsFromTheChangelog(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "installed-manifest.json", manifest)
	write(t, dir, "changelog.json", document)
	snap, err := versions.Load(dir, "v0.7.5")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Bundle == nil || snap.Bundle.Version != "v0.7.5" || snap.Bundle.Arch != "armv6" {
		t.Fatalf("bundle = %+v", snap.Bundle)
	}
	if !snap.HasChangelog || len(snap.Components) != 3 {
		t.Fatalf("snapshot = %+v", snap)
	}
	first := snap.Components[0]
	if first.ID != "dashboard" || first.Label != "Dashboard" || first.Kind != "app" || first.Version != "v0.7.5" || !first.Installed {
		t.Fatalf("first component = %+v", first)
	}
}

func TestLoadMarksADeselectedOptionalServiceAsNotInstalled(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "installed-manifest.json", manifest)
	write(t, dir, "changelog.json", document)
	write(t, dir, "selection.json", `{"steps": {"81": true, "83": false}}`)
	snap, err := versions.Load(dir, "v0.7.5")
	if err != nil {
		t.Fatal(err)
	}
	installed := map[string]bool{}
	for _, c := range snap.Components {
		installed[c.ID] = c.Installed
	}
	if !installed["service:apsystems_ez1"] || installed["service:shelly"] || !installed["dashboard"] {
		t.Fatalf("installed flags = %v", installed)
	}
}

func TestLoadFallsBackToTheManifestWhenTheBundleHasNoChangelog(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "installed-manifest.json", manifest)
	snap, err := versions.Load(dir, "v0.7.5")
	if err != nil {
		t.Fatal(err)
	}
	if snap.HasChangelog {
		t.Fatal("has_changelog must be false without changelog.json")
	}
	ids := []string{}
	for _, c := range snap.Components {
		ids = append(ids, c.ID+"="+c.Version+"|"+c.Label+"|"+c.Kind)
	}
	// Plan 2 has not shipped yet, so this is what every node shows today: the
	// labels and kinds must be the real ones, not the bare ids.
	want := []string{
		"dashboard=v0.7.5|Dashboard|app",
		"service:apsystems_ez1=v0.4.1|APsystems EZ1|service",
		"service:shelly=v0.4.0|Shelly|service",
		"services=v0.4.0|Shared services|shared",
	}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
}

func TestLoadNamesAnUnknownServiceByItsDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "installed-manifest.json", `{"version":"v0.8.0","components":{},"steps":[{"id":"89","optional":true,"dir":"new_thing","version":"v0.1.0"}]}`)
	snap, err := versions.Load(dir, "v0.8.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Components) != 1 || snap.Components[0].Label != "new_thing" || snap.Components[0].Kind != "service" {
		t.Fatalf("components = %+v, want label new_thing, kind service", snap.Components)
	}
}

// The fallback table must not drift from the component table the changelog
// generator and plan 2's changelog.json use.
func TestFallbackComponentsMatchTheComponentTable(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "version", "components.json"))
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Components []struct {
			ID     string `json:"id"`
			Label  string `json:"label"`
			Kind   string `json:"kind"`
			Bundle bool   `json:"bundle"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &table); err != nil {
		t.Fatal(err)
	}
	fallback := versions.FallbackComponents()
	bundled := 0
	for _, row := range table.Components {
		if !row.Bundle {
			continue
		}
		bundled++
		got, ok := fallback[row.ID]
		if !ok || got[0] != row.Label || got[1] != row.Kind {
			t.Errorf("fallbackComponents[%q] = %v, want [%s %s] as in components.json", row.ID, got, row.Label, row.Kind)
		}
	}
	if len(fallback) != bundled {
		t.Errorf("fallbackComponents has %d entries, components.json has %d bundle components", len(fallback), bundled)
	}
}

func TestChangelogFiltersComponentsAndKeepsTheSchema(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "changelog.json", document)
	raw, err := versions.Changelog(dir, []string{"service:shelly", "dashboard"})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		SchemaVersion int    `json:"schema_version"`
		BundleVersion string `json:"bundle_version"`
		Components    []struct {
			ID string `json:"id"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.SchemaVersion != 1 || out.BundleVersion != "v0.7.5" {
		t.Fatalf("top-level fields lost: %s", raw)
	}
	if len(out.Components) != 2 || out.Components[0].ID != "dashboard" || out.Components[1].ID != "service:shelly" {
		t.Fatalf("components = %+v, want dashboard then service:shelly in file order", out.Components)
	}
}

func TestChangelogWithoutIdsIsTheFileVerbatim(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "changelog.json", document)
	raw, err := versions.Changelog(dir, nil)
	if err != nil || string(raw) != document {
		t.Fatalf("raw = %s, err = %v", raw, err)
	}
}
