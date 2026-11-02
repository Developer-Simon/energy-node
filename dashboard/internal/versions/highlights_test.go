package versions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
    {"text":"custom categories","pr":115,"breaking":false,"highlight":true}]}]},
  {"version":"v0.4.3","date":"2026-10-01","groups":[{"type":"feat","label":"Features","entries":[
    {"text":"older automation change","pr":99,"breaking":false,"highlight":true}]}]}]},
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
	found := false
	for _, c := range view.Components {
		if c.ID != "service:automation" {
			continue
		}
		found = true
		if c.From != "" || len(c.Releases) != 1 || c.Releases[0].Version != "v0.5.0" {
			t.Fatalf("new component = %+v, want only its current release v0.5.0 and no from", c)
		}
	}
	if !found {
		t.Fatal("service:automation missing from the view")
	}
}

func TestHighlightsCorruptPreviousManifestDegradesToInstalledRelease(t *testing.T) {
	dir := writeState(t, map[string]string{
		"changelog.json":          highlightsDoc,
		"installed-manifest.json": installedV090,
		"previous-manifest.json":  `{not json`,
	})
	view, err := Highlights(dir)
	if err != nil {
		t.Fatalf("corrupt previous-manifest.json must not fail: %v", err)
	}
	if view.SincePrevious || view.From != "" {
		t.Fatalf("range = %+v, want since_previous=false", view)
	}
}

func TestHighlightsUnreadablePreviousManifestDegradesToInstalledRelease(t *testing.T) {
	dir := writeState(t, map[string]string{
		"changelog.json":          highlightsDoc,
		"installed-manifest.json": installedV090,
	})
	// A directory in place of the file: ReadFile fails with a non-ENOENT error.
	if err := os.Mkdir(filepath.Join(dir, "previous-manifest.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	view, err := Highlights(dir)
	if err != nil {
		t.Fatalf("unreadable previous-manifest.json must not fail: %v", err)
	}
	if view.SincePrevious {
		t.Fatalf("range = %+v, want since_previous=false", view)
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

func TestHighlightsCarryTranslations(t *testing.T) {
	dir := writeState(t, map[string]string{
		"changelog.json": `{"schema_version":1,"components":[{"id":"dashboard","label":"Dashboard","version":"v0.9.0","releases":[
		  {"version":"v0.9.0","groups":[{"type":"feat","entries":[
		    {"text":"device map layers","pr":103,"breaking":false,"highlight":true,"i18n":{"de":"Ebenen im Gerätediagramm"}},
		    {"text":"untranslated","pr":104,"breaking":false,"highlight":true}]}]}]}]}`,
		"installed-manifest.json": `{"version":"v0.9.0","components":{"dashboard":"v0.9.0"}}`,
	})
	view, err := Highlights(dir)
	if err != nil {
		t.Fatal(err)
	}
	entries := view.Components[0].Releases[0].Entries
	if entries[0].I18n["de"] != "Ebenen im Gerätediagramm" || entries[1].I18n != nil {
		t.Fatalf("i18n = %+v / %+v", entries[0].I18n, entries[1].I18n)
	}
	raw, _ := json.Marshal(entries[1])
	if strings.Contains(string(raw), "i18n") {
		t.Fatalf("untranslated entry serialises i18n: %s", raw)
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

func TestHighlightsPassesDetailsThrough(t *testing.T) {
	dir := writeState(t, map[string]string{
		"changelog.json": `{"schema_version":1,"components":[{"id":"dashboard","label":"Dashboard","version":"v0.9.0","releases":[
		  {"version":"v0.9.0","groups":[{"type":"feat","entries":[
		    {"text":"device map layers","pr":103,"breaking":false,"highlight":true,
		     "details":{"text":{"en":"Layers in the device map","de":"Ebenen im Gerätediagramm"},
		      "image":{"url":"https://example.invalid/shot.png","width":800,"height":600,"crop":[0.1,0.2,0.8,0.9]},
		      "doc":"device-map-layers"}}]}]}]}]}`,
		"installed-manifest.json": `{"version":"v0.9.0","components":{"dashboard":"v0.9.0"}}`,
	})
	view, err := Highlights(dir)
	if err != nil {
		t.Fatal(err)
	}
	entry := view.Components[0].Releases[0].Entries[0]
	d := entry.Details
	if d == nil || d.Text["de"] != "Ebenen im Gerätediagramm" || d.Text["en"] != "Layers in the device map" {
		t.Fatalf("details = %+v", d)
	}
	if d.Image == nil || d.Image.URL != "https://example.invalid/shot.png" || d.Image.Width != 800 || d.Image.Height != 600 {
		t.Fatalf("image = %+v", d.Image)
	}
	if len(d.Image.Crop) != 4 || d.Image.Crop[0] != 0.1 || d.Image.Crop[3] != 0.9 {
		t.Fatalf("crop = %v", d.Image.Crop)
	}
	if d.Doc != "device-map-layers" {
		t.Fatalf("doc = %q", d.Doc)
	}
}

func TestHighlightsWithoutDetailsOmitsTheKey(t *testing.T) {
	dir := writeState(t, map[string]string{
		"changelog.json":          highlightsDoc,
		"installed-manifest.json": installedV090,
	})
	view, err := Highlights(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"details"`) {
		t.Fatalf("response serialises details without any: %s", raw)
	}
}

func TestHighlightsPassesTourThrough(t *testing.T) {
	dir := writeState(t, map[string]string{
		"changelog.json": `{"schema_version":1,"components":[{"id":"dashboard","label":"Dashboard","version":"v0.9.0","releases":[
		  {"version":"v0.9.0","groups":[{"type":"feat","entries":[
		    {"text":"favourites","pr":180,"breaking":false,"highlight":true,
		     "tour":{"steps":[{"panel":"devices-panel","page":"users","target":"favorites","device":"phone","need":"view_devices",
		       "title":{"en":"Favourites","de":"Favoriten"},"text":{"en":"Star a device","de":"Markiere ein Gerät"}}]}},
		    {"text":"backend only","pr":181,"breaking":false,"highlight":true,
		     "tour":{"steps":[],"exempt":"Only the backend changed"}}]}]}]}]}`,
		"installed-manifest.json": `{"version":"v0.9.0","components":{"dashboard":"v0.9.0"}}`,
	})
	view, err := Highlights(dir)
	if err != nil {
		t.Fatal(err)
	}
	entries := view.Components[0].Releases[0].Entries
	if entries[0].ID != "h180" || entries[1].ID != "h181" {
		t.Fatalf("ids = %q %q", entries[0].ID, entries[1].ID)
	}
	tour := entries[0].Tour
	if tour == nil || len(tour.Steps) != 1 || tour.Exempt != "" {
		t.Fatalf("tour = %+v", tour)
	}
	step := tour.Steps[0]
	if step.Panel != "devices-panel" || step.Page != "users" || step.Target != "favorites" || step.Device != "phone" || step.Need != "view_devices" {
		t.Fatalf("step = %+v", step)
	}
	if step.Title["de"] != "Favoriten" || step.Text["en"] != "Star a device" {
		t.Fatalf("texts = %v %v", step.Title, step.Text)
	}
	if exempt := entries[1].Tour; exempt == nil || exempt.Exempt != "Only the backend changed" || len(exempt.Steps) != 0 {
		t.Fatalf("exempt = %+v", exempt)
	}
}

func TestHighlightsWithoutTourOmitsTheKey(t *testing.T) {
	dir := writeState(t, map[string]string{
		"changelog.json":          highlightsDoc,
		"installed-manifest.json": installedV090,
	})
	view, err := Highlights(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"tour"`) {
		t.Fatalf("response serialises a tour without any: %s", raw)
	}
}

func TestFreshInstallMeansNoPreviousManifest(t *testing.T) {
	dir := writeState(t, map[string]string{"installed-manifest.json": installedV090})
	if !FreshInstall(dir) {
		t.Fatal("no previous manifest must be a fresh install")
	}
	dir = writeState(t, map[string]string{"previous-manifest.json": `{"version":"v0.8.5"}`})
	if FreshInstall(dir) {
		t.Fatal("a previous manifest means an update")
	}
}

// releasesDoc has releases of dashboard across four minor versions, plus one
// release newer than the installed v0.10.2.
const releasesDoc = `{"schema_version":1,"components":[
 {"id":"dashboard","label":"Dashboard","kind":"app","version":"v0.10.2","releases":[
  {"version":"v0.10.9","groups":[{"type":"feat","entries":[{"text":"not installed yet","pr":209,"breaking":false,"highlight":true}]}]},
  {"version":"v0.10.2","groups":[{"type":"fix","entries":[{"text":"ten two","pr":202,"breaking":false,"highlight":true}]}]},
  {"version":"v0.10.0","groups":[{"type":"feat","entries":[{"text":"ten zero","pr":190,"breaking":false,"highlight":true}]}]},
  {"version":"v0.9.1","groups":[{"type":"fix","entries":[{"text":"nine one","pr":191,"breaking":false,"highlight":true}]}]},
  {"version":"v0.9.0","groups":[{"type":"feat","entries":[{"text":"nine zero","pr":180,"breaking":false,"highlight":true}]}]},
  {"version":"v0.8.3","groups":[{"type":"feat","entries":[{"text":"eight three","pr":83,"breaking":false,"highlight":true}]}]},
  {"version":"v0.8.0","groups":[{"type":"feat","entries":[{"text":"eight zero","pr":80,"breaking":false,"highlight":true}]}]},
  {"version":"v0.7.4","groups":[{"type":"feat","entries":[{"text":"seven four","pr":74,"breaking":false,"highlight":true}]}]}]}]}`

func TestReleasesKeepsTheLastThreeMinors(t *testing.T) {
	dir := writeState(t, map[string]string{
		"changelog.json":          releasesDoc,
		"installed-manifest.json": `{"version":"v0.10.2","components":{"dashboard":"v0.10.2"}}`,
	})
	view, err := Releases(dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	if view.To != "v0.10.2" {
		t.Fatalf("to = %q", view.To)
	}
	var versionsSeen []string
	var texts []string
	for _, r := range view.Releases {
		versionsSeen = append(versionsSeen, r.Version)
		for _, e := range r.Entries {
			texts = append(texts, e.Text)
		}
	}
	wantVersions := []string{"v0.10.2", "v0.10.0", "v0.9.1", "v0.9.0", "v0.8.3", "v0.8.0"}
	if strings.Join(versionsSeen, ",") != strings.Join(wantVersions, ",") {
		t.Fatalf("releases = %v, want %v (0.10.9 is newer than installed, 0.7.4 is a fourth minor back)", versionsSeen, wantVersions)
	}
	if strings.Join(texts, "|") != "ten two|ten zero|nine one|nine zero|eight three|eight zero" {
		t.Fatalf("entries = %v", texts)
	}
}

func TestReleasesWithFewerMinorsKeepsEverythingThere(t *testing.T) {
	dir := writeState(t, map[string]string{
		"changelog.json":          highlightsDoc,
		"installed-manifest.json": installedV090,
	})
	view, err := Releases(dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range view.Releases {
		got = append(got, r.Version)
	}
	if strings.Join(got, ",") != "v0.9.0,v0.8.6,v0.8.5" {
		t.Fatalf("dashboard releases = %v, want the whole 0.9 and 0.8 range", got)
	}
}

func TestReleasesSkipsDeselectedComponents(t *testing.T) {
	dir := writeState(t, map[string]string{
		"changelog.json":          highlightsDoc,
		"installed-manifest.json": installedV090,
		"selection.json":          `{"steps":{"61":false}}`,
	})
	view, err := Releases(dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range view.Releases {
		for _, e := range r.Entries {
			if e.Component == "service:shelly" {
				t.Fatalf("deselected shelly is listed: %+v", e)
			}
		}
	}
}

func TestReleasesWithoutChangelogIsNoChangelog(t *testing.T) {
	if _, err := Releases(t.TempDir(), 3); err == nil {
		t.Fatal("missing changelog must fail")
	}
}

// attachDoc has three dashboard releases of minor 0.10 and v0.9.8, plus a
// bootstrap and an automation release that fall between them by date.
const attachDoc = `{"schema_version":1,"components":[
 {"id":"dashboard","label":"Dashboard","kind":"app","version":"v0.10.33","releases":[
  {"version":"v0.10.33","date":"2026-10-31","groups":[{"type":"feat","entries":[{"text":"dashboard new","pr":300,"breaking":false,"highlight":true}]}]},
  {"version":"v0.10.8","date":"2026-10-22","groups":[{"type":"fix","entries":[{"text":"dashboard eight","pr":208,"breaking":false,"highlight":true}]}]},
  {"version":"v0.10.0","date":"2026-10-19","groups":[{"type":"feat","entries":[{"text":"dashboard ten","pr":190,"breaking":false,"highlight":true}]}]},
  {"version":"v0.9.8","date":"2026-10-15","groups":[{"type":"feat","entries":[{"text":"dashboard nine","pr":198,"breaking":false,"highlight":true}]}]}]},
 {"id":"bootstrap","label":"Bootstrap","kind":"tool","version":"v0.2.6","releases":[
  {"version":"v0.2.6","date":"2026-10-31","groups":[{"type":"fix","entries":[{"text":"bootstrap six","pr":260,"breaking":false,"highlight":true}]}]},
  {"version":"v0.2.5","date":"2026-10-20","groups":[{"type":"feat","entries":[{"text":"bootstrap five","pr":250,"breaking":false,"highlight":true}]}]}]},
 {"id":"service:automation","label":"Automation","kind":"service","version":"v0.7.2","releases":[
  {"version":"v0.7.2","date":"2026-10-27","groups":[{"type":"feat","entries":[{"text":"automation seven two","pr":272,"breaking":false,"highlight":true}]}]}]}]}`

func TestExploreAttachesComponentReleasesToDashboardReleases(t *testing.T) {
	dir := writeState(t, map[string]string{
		"changelog.json":          attachDoc,
		"installed-manifest.json": `{"version":"v0.10.33","components":{"dashboard":"v0.10.33","bootstrap":"v0.2.6","service:automation":"v0.7.2"}}`,
	})
	view, err := Releases(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	var releases []string
	for _, r := range view.Releases {
		releases = append(releases, r.Version)
	}
	if strings.Join(releases, ",") != "v0.10.33,v0.10.8,v0.10.0" {
		t.Fatalf("releases = %v, want the three releases of minor 0.10, newest first", releases)
	}
	attached := map[string]string{}
	var order []string
	for _, r := range view.Releases {
		for _, e := range r.Entries {
			attached[e.Text] = r.Version + "|" + e.Component + "|" + e.ComponentLabel + "|" + e.ComponentVersion
			if r.Version == "v0.10.33" {
				order = append(order, e.Text)
			}
		}
	}
	want := map[string]string{
		"dashboard new":        "v0.10.33|dashboard|Dashboard|v0.10.33",
		"bootstrap six":        "v0.10.33|bootstrap|Bootstrap|v0.2.6",
		"automation seven two": "v0.10.33|service:automation|Automation|v0.7.2",
		"bootstrap five":       "v0.10.8|bootstrap|Bootstrap|v0.2.5",
		"dashboard eight":      "v0.10.8|dashboard|Dashboard|v0.10.8",
		"dashboard ten":        "v0.10.0|dashboard|Dashboard|v0.10.0",
	}
	for text, w := range want {
		if attached[text] != w {
			t.Errorf("%q attached as %q, want %q", text, attached[text], w)
		}
	}
	if len(attached) != len(want) {
		t.Fatalf("attached = %v, want exactly %v (v0.9.8 is outside the window)", attached, want)
	}
	if strings.Join(order, "|") != "dashboard new|bootstrap six|automation seven two" {
		t.Fatalf("v0.10.33 entries = %v, want dashboard first, then changelog order", order)
	}
}

// TestExploreDropsComponentReleasesOlderThanTheWindow: a component release
// dated before the oldest window release belongs to no card and is dropped,
// an undated one keeps the oldest card.
func TestExploreDropsComponentReleasesOlderThanTheWindow(t *testing.T) {
	dir := writeState(t, map[string]string{
		"changelog.json": `{"schema_version":1,"components":[
 {"id":"dashboard","label":"Dashboard","kind":"app","version":"v0.10.33","releases":[
  {"version":"v0.10.33","date":"2026-10-31","groups":[{"type":"feat","entries":[{"text":"dashboard new","pr":300,"breaking":false,"highlight":true}]}]},
  {"version":"v0.10.0","date":"2026-10-19","groups":[{"type":"feat","entries":[{"text":"dashboard ten","pr":190,"breaking":false,"highlight":true}]}]}]},
 {"id":"bootstrap","label":"Bootstrap","kind":"tool","version":"v0.2.6","releases":[
  {"version":"v0.2.6","date":"2026-10-31","groups":[{"type":"fix","entries":[{"text":"bootstrap six","pr":260,"breaking":false,"highlight":true}]}]},
  {"version":"v0.2.1","date":"2026-09-29","groups":[{"type":"feat","entries":[{"text":"bootstrap early","pr":210,"breaking":false,"highlight":true}]}]},
  {"version":"v0.2.0","date":"2026-09-28","groups":[{"type":"feat","entries":[{"text":"bootstrap older","pr":200,"breaking":false,"highlight":true}]}]},
  {"version":"v0.2.2","date":"","groups":[{"type":"fix","entries":[{"text":"bootstrap undated","pr":220,"breaking":false,"highlight":true}]}]}]}]}`,
		"installed-manifest.json": `{"version":"v0.10.33","components":{"dashboard":"v0.10.33","bootstrap":"v0.2.6"}}`,
	})
	view, err := Releases(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	attached := map[string]string{}
	for _, r := range view.Releases {
		for _, e := range r.Entries {
			attached[e.Text] = r.Version
		}
	}
	for _, text := range []string{"bootstrap early", "bootstrap older"} {
		if got, ok := attached[text]; ok {
			t.Errorf("%q attached to %s, want dropped (older than the window)", text, got)
		}
	}
	want := map[string]string{
		"dashboard new":     "v0.10.33",
		"bootstrap six":     "v0.10.33",
		"dashboard ten":     "v0.10.0",
		"bootstrap undated": "v0.10.0",
	}
	for text, w := range want {
		if attached[text] != w {
			t.Errorf("%q attached to %q, want %q", text, attached[text], w)
		}
	}
	if len(attached) != len(want) {
		t.Fatalf("attached = %v, want exactly %v", attached, want)
	}
}

func TestExploreWithoutDashboardFallsBackPerComponent(t *testing.T) {
	dir := writeState(t, map[string]string{
		"changelog.json": `{"schema_version":1,"components":[
 {"id":"bootstrap","label":"Bootstrap","kind":"tool","version":"v0.2.6","releases":[
  {"version":"v0.2.6","date":"2026-10-31","groups":[{"type":"fix","entries":[{"text":"six","pr":260,"breaking":false,"highlight":true}]}]},
  {"version":"v0.2.5","date":"2026-10-20","groups":[{"type":"feat","entries":[{"text":"five","pr":250,"breaking":false,"highlight":true}]}]}]}]}`,
		"installed-manifest.json": `{"version":"v0.2.6","components":{"bootstrap":"v0.2.6"}}`,
	})
	view, err := Releases(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	var releases []string
	for _, r := range view.Releases {
		releases = append(releases, r.Version)
		for _, e := range r.Entries {
			if e.Component != "bootstrap" || e.ComponentVersion != r.Version {
				t.Fatalf("entry %q = %+v, want its own release as the component release", e.Text, e)
			}
		}
	}
	if strings.Join(releases, ",") != "v0.2.6,v0.2.5" {
		t.Fatalf("releases = %v, want the bootstrap releases each on its own", releases)
	}
}

func TestMinorOf(t *testing.T) {
	for in, want := range map[string]string{
		"v0.10.8":     "0.10",
		"0.9.1-dev":   "0.9",
		"v1.2":        "1.2",
		"branch-only": "0.0",
	} {
		if got := MinorOf(in); got != want {
			t.Fatalf("MinorOf(%q) = %q, want %q", in, got, want)
		}
	}
}
