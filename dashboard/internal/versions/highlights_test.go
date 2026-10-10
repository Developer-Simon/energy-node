package versions

import (
	"os"
	"path/filepath"
	"testing"
)

const highlightsDoc = `{"schema_version":1,"bundle_version":"v0.9.0","components":[
 {"id":"dashboard","label":"Dashboard","kind":"app","version":"v0.9.0","releases":[
  {"version":"v0.9.0","date":"2026-10-10","groups":[{"type":"feat","label":"Features","entries":[
    {"text":"device map layers","scope":"dashboard","pr":103,"breaking":false,"highlight":true},
    {"text":"a detail","scope":"dashboard","hash":"abc","breaking":false,"highlight":false}]}]},
  {"version":"v0.8.6","date":"2026-10-05","groups":[{"type":"fix","label":"Bug Fixes","entries":[
    {"text":"fix saving","pr":91,"breaking":false,"highlight":true}]}]},
  {"version":"v0.8.5","date":"2026-10-01","groups":[{"type":"feat","label":"Features","entries":[
    {"text":"already installed before","pr":80,"breaking":false,"highlight":true}]}]}]},
 {"id":"service:automation","label":"Automation","kind":"service","version":"v0.5.0","releases":[
  {"version":"v0.5.0","date":"2026-10-10","groups":[{"type":"feat","label":"Features","entries":[
    {"text":"custom categories","pr":115,"breaking":false,"highlight":true}]}]}]},
 {"id":"service:shelly","label":"Shelly","kind":"service","version":"v0.4.1","releases":[
  {"version":"v0.4.1","date":"2026-10-10","groups":[{"type":"feat","label":"Features","entries":[
    {"text":"shelly change","pr":120,"breaking":false,"highlight":true}]}]}]},
 {"id":"bootstrap","label":"Bootstrap","kind":"tool","version":"v0.3.0","releases":[
  {"version":"v0.3.0","date":"2026-09-01","groups":[{"type":"feat","label":"Features","entries":[
    {"text":"unchanged component","pr":10,"breaking":false,"highlight":true}]}]}]}]}`

func writeState(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const installedV090 = `{"version":"v0.9.0","components":{"dashboard":"v0.9.0","bootstrap":"v0.3.0"},
 "steps":[{"id":"60","dir":"automation","version":"v0.5.0","optional":true},
          {"id":"61","dir":"shelly","version":"v0.4.1","optional":true}]}`

func TestHighlightsSincePreviousCutsTheRange(t *testing.T) {
	dir := writeState(t, map[string]string{
		"changelog.json":          highlightsDoc,
		"installed-manifest.json": installedV090,
		"previous-manifest.json": `{"version":"v0.8.5","components":{"dashboard":"v0.8.5","bootstrap":"v0.3.0"},
		  "steps":[{"id":"60","dir":"automation","version":"v0.4.3"},{"id":"61","dir":"shelly","version":"v0.4.0"}]}`,
		"selection.json": `{"steps":{"61":false}}`,
	})
	view, err := Highlights(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !view.SincePrevious || view.From != "v0.8.5" || view.To != "v0.9.0" {
		t.Fatalf("range = %+v", view)
	}
	got := map[string][]string{}
	for _, c := range view.Components {
		for _, r := range c.Releases {
			for _, e := range r.Entries {
				got[c.ID] = append(got[c.ID], e.Text)
			}
		}
	}
	want := map[string][]string{
		"dashboard":          {"device map layers", "fix saving"},
		"service:automation": {"custom categories"},
	}
	if len(got) != len(want) || len(got["dashboard"]) != 2 || got["dashboard"][0] != "device map layers" ||
		got["dashboard"][1] != "fix saving" || got["service:automation"][0] != "custom categories" {
		t.Fatalf("entries = %v, want %v (no details, no v0.8.5, no unchanged bootstrap, no deselected shelly)", got, want)
	}
	if view.Components[0].From != "v0.8.5" || view.Components[0].To != "v0.9.0" {
		t.Fatalf("component range = %+v", view.Components[0])
	}
}

func TestHighlightsWithoutPreviousShowsTheInstalledReleaseOnly(t *testing.T) {
	dir := writeState(t, map[string]string{
		"changelog.json":          highlightsDoc,
		"installed-manifest.json": installedV090,
	})
	view, err := Highlights(dir)
	if err != nil {
		t.Fatal(err)
	}
	if view.SincePrevious || view.From != "" || view.To != "v0.9.0" {
		t.Fatalf("range = %+v", view)
	}
	for _, c := range view.Components {
		if len(c.Releases) != 1 || c.Releases[0].Version != c.To {
			t.Fatalf("%s: releases = %+v, want only %s", c.ID, c.Releases, c.To)
		}
	}
}

func TestHighlightsNewComponentShowsOnlyItsCurrentRelease(t *testing.T) {
	dir := writeState(t, map[string]string{
		"changelog.json":          highlightsDoc,
		"installed-manifest.json": installedV090,
		"previous-manifest.json":  `{"version":"v0.8.5","components":{"dashboard":"v0.8.5"}}`,
	})
	view, err := Highlights(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range view.Components {
		if c.ID == "service:automation" && (c.From != "" || len(c.Releases) != 1) {
			t.Fatalf("new component = %+v, want only its current release and no from", c)
		}
	}
}

func TestHighlightsUnflaggedDocumentKeepsEveryEntry(t *testing.T) {
	doc := `{"components":[{"id":"dashboard","label":"Dashboard","version":"v0.7.5","releases":[
	  {"version":"v0.7.5","groups":[{"type":"feat","entries":[{"text":"a"},{"text":"b"}]}]}]}]}`
	dir := writeState(t, map[string]string{
		"changelog.json":          doc,
		"installed-manifest.json": `{"version":"v0.7.5","components":{"dashboard":"v0.7.5"}}`,
	})
	view, err := Highlights(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Components) != 1 || len(view.Components[0].Releases[0].Entries) != 2 {
		t.Fatalf("view = %+v, want both entries of a document from before the highlight flag", view)
	}
}

func TestHighlightsWithoutChangelogIsNoChangelog(t *testing.T) {
	dir := writeState(t, map[string]string{"installed-manifest.json": installedV090})
	if _, err := Highlights(dir); err == nil {
		t.Fatal("want NO_CHANGELOG")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.9.0", "v0.8.5", 1}, {"0.8.5", "v0.8.5", 0}, {"v0.10.0", "v0.9.9", 1},
		{"v0.9.0-dev", "v0.9.0", 0}, {"v1.0.0", "v1.0.1", -1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestLoadReportsThePreviousBundle(t *testing.T) {
	dir := writeState(t, map[string]string{
		"installed-manifest.json": `{"version":"v0.9.0","components":{"dashboard":"v0.9.0"}}`,
		"previous-manifest.json":  `{"version":"v0.8.5","components":{"dashboard":"v0.8.5"}}`,
	})
	snap, err := Load(dir, "v0.9.0")
	if err != nil || snap.Previous == nil || snap.Previous.Version != "v0.8.5" {
		t.Fatalf("snap = %+v, err = %v", snap, err)
	}

	without := writeState(t, map[string]string{
		"installed-manifest.json": `{"version":"v0.9.0","components":{"dashboard":"v0.9.0"}}`,
	})
	snap, err = Load(without, "v0.9.0")
	if err != nil || snap.Previous != nil {
		t.Fatalf("snap.Previous = %+v, want nil without previous-manifest.json", snap.Previous)
	}
}
