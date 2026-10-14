package hostapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func TestReadChangelogDocumentReturnsTheFileVerbatim(t *testing.T) {
	dir := t.TempDir()
	const doc = `{"schema_version":1,"components":[]}`
	if err := os.WriteFile(filepath.Join(dir, "changelog.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := hostapi.ReadChangelogDocument(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != doc {
		t.Fatalf("raw = %s, want %s", raw, doc)
	}
}

func TestReadChangelogDocumentReportsAMissingFileAsNoChangelog(t *testing.T) {
	_, err := hostapi.ReadChangelogDocument(t.TempDir())
	var apiErr *hostapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "NO_CHANGELOG" || apiErr.Status != http.StatusNotFound {
		t.Fatalf("err = %#v, want NO_CHANGELOG/404", err)
	}
}

func TestReadChangelogDocumentRejectsBrokenJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "changelog.json"), []byte(`{"broken":`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := hostapi.ReadChangelogDocument(dir)
	var apiErr *hostapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "CHANGELOG_UNREADABLE" {
		t.Fatalf("err = %#v, want CHANGELOG_UNREADABLE", err)
	}
}

func TestInstalledVersionsMergesComponentsAndServiceSteps(t *testing.T) {
	manifest := []byte(`{
		"components": {"dashboard": "v0.7.5", "services": "v0.4.0"},
		"steps": [
			{"id": "10", "optional": false},
			{"id": "81", "dir": "apsystems_ez1", "version": "v0.4.1"},
			{"id": "82", "dir": "battery_soc"}
		]
	}`)
	got, err := hostapi.InstalledVersions(manifest)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"dashboard": "v0.7.5", "services": "v0.4.0", "service:apsystems_ez1": "v0.4.1"}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("got %s, want %s (a step without a version must not appear)", gotJSON, wantJSON)
	}
}

func TestInstalledVersionsOfAnOldManifestIsJustItsComponents(t *testing.T) {
	got, err := hostapi.InstalledVersions([]byte(`{"components": {"dashboard": "v0.6.0"}}`))
	if err != nil || len(got) != 1 || got["dashboard"] != "v0.6.0" {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestInstalledVersionsRejectsNonJSON(t *testing.T) {
	if _, err := hostapi.InstalledVersions([]byte(`nope`)); err == nil {
		t.Fatal("expected an error")
	}
}

func TestChangelogEndpointReturnsTheBackendsView(t *testing.T) {
	server, fake := newTestServer(t, nil)
	fake.ChangelogView = &hostapi.ChangelogView{
		BundleVersion: "v0.7.5",
		Installed:     map[string]string{"dashboard": "v0.7.4"},
		Document:      json.RawMessage(`{"schema_version":1,"components":[]}`),
	}
	connectFirst(t, server)
	rec := do(t, server, http.MethodGet, "/api/changelog", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		BundleVersion string            `json:"bundle_version"`
		Installed     map[string]string `json:"installed"`
		Document      struct {
			SchemaVersion int `json:"schema_version"`
		} `json:"document"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if got.BundleVersion != "v0.7.5" || got.Installed["dashboard"] != "v0.7.4" || got.Document.SchemaVersion != 1 {
		t.Fatalf("view = %+v", got)
	}
}

func TestChangelogEndpointAlwaysSendsAnInstalledObject(t *testing.T) {
	server, fake := newTestServer(t, nil)
	fake.ChangelogView = &hostapi.ChangelogView{BundleVersion: "v0.7.5", Document: json.RawMessage(`{}`)}
	connectFirst(t, server)
	rec := do(t, server, http.MethodGet, "/api/changelog", "")
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["installed"]) != "{}" {
		t.Fatalf(`installed = %s, want {} (the screen does Object.keys on it; a first install has nothing installed)`, raw["installed"])
	}
}

func TestChangelogEndpointIsRefusedWithoutAConnection(t *testing.T) {
	server, fake := newTestServer(t, nil)
	fake.ChangelogView = &hostapi.ChangelogView{BundleVersion: "v0.7.5", Document: json.RawMessage(`{}`)}
	rec := do(t, server, http.MethodGet, "/api/changelog", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 NOT_CONNECTED", rec.Code)
	}
}

func TestChangelogEndpointReportsAPackageWithoutOneAs404(t *testing.T) {
	server, fake := newTestServer(t, nil)
	connectFirst(t, server)
	if rec := do(t, server, http.MethodGet, "/api/changelog", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("nil view: status = %d, want 404", rec.Code)
	}
	fake.ChangelogErr = &hostapi.Error{Code: "NO_CHANGELOG", Status: http.StatusNotFound}
	rec := do(t, server, http.MethodGet, "/api/changelog", "")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "NO_CHANGELOG") {
		t.Fatalf("status = %d body = %s, want 404 NO_CHANGELOG", rec.Code, rec.Body.String())
	}
}

// noChangelog hides the optional capability of the wrapped backend.
type noChangelog struct{ hostapi.Backend }

func TestChangelogEndpointIs501ForABackendWithoutTheCapability(t *testing.T) {
	server, _ := newTestServer(t, func(opts *hostapi.Options) { opts.Backend = noChangelog{opts.Backend} })
	connectFirst(t, server)
	if rec := do(t, server, http.MethodGet, "/api/changelog", ""); rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}

func TestChangelogEndpointRejectsWrites(t *testing.T) {
	server, _ := newTestServer(t, nil)
	connectFirst(t, server)
	if rec := do(t, server, http.MethodPost, "/api/changelog", "{}"); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func writeHistory(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const historyNew = `{"schema_version":1,"bundle_version":"v0.9.10","components":[
 {"id":"dashboard","label":"Dashboard","kind":"app","version":"v0.9.10","releases":[{"version":"v0.9.10","groups":[]}]},
 {"id":"bootstrap","label":"Bootstrap","kind":"tool","version":"v0.3.0","releases":[]}]}`
const historyOld = `{"schema_version":1,"bundle_version":"v0.9.8","components":[
 {"id":"dashboard","label":"Dashboard alt","kind":"app","version":"v0.9.8","releases":[{"version":"v0.9.8","groups":[]},{"version":"v0.9.10","groups":["stale"]}]},
 {"id":"gone","label":"Gone","kind":"tool","version":"v0.1.0","releases":[{"version":"v0.1.0","groups":[]}]}]}`

type mergedDoc struct {
	BundleVersion string `json:"bundle_version"`
	Components    []struct {
		ID       string `json:"id"`
		Label    string `json:"label"`
		Releases []struct {
			Version string            `json:"version"`
			Groups  []json.RawMessage `json:"groups"`
		} `json:"releases"`
	} `json:"components"`
}

func TestReadChangelogHistoryMergesNewestFirst(t *testing.T) {
	dir := writeHistory(t, map[string]string{
		"changelog/v0.9.10.json": historyNew,
		"changelog/v0.9.8.json":  historyOld,
		"changelog/notes.txt":    "not a changelog",
	})
	history, err := hostapi.ReadChangelogHistory(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if history.Total != 2 || history.Shown != 2 {
		t.Fatalf("total/shown = %d/%d", history.Total, history.Shown)
	}
	var doc mergedDoc
	if err := json.Unmarshal(history.Document, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.BundleVersion != "v0.9.10" || len(doc.Components) != 3 {
		t.Fatalf("doc = %+v", doc)
	}
	dash := doc.Components[0]
	if dash.ID != "dashboard" || dash.Label != "Dashboard" || len(dash.Releases) != 2 ||
		dash.Releases[0].Version != "v0.9.10" || len(dash.Releases[0].Groups) != 0 || dash.Releases[1].Version != "v0.9.8" {
		t.Fatalf("dashboard = %+v (newest metadata, newest duplicate wins, newest first)", dash)
	}
	if doc.Components[1].ID != "bootstrap" || doc.Components[2].ID != "gone" {
		t.Fatalf("order = %s, %s", doc.Components[1].ID, doc.Components[2].ID)
	}
}

func TestReadChangelogHistoryLimit(t *testing.T) {
	dir := writeHistory(t, map[string]string{"changelog/v0.9.10.json": historyNew, "changelog/v0.9.8.json": historyOld})
	history, err := hostapi.ReadChangelogHistory(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	var doc mergedDoc
	_ = json.Unmarshal(history.Document, &doc)
	if history.Total != 2 || history.Shown != 1 || len(doc.Components) != 2 {
		t.Fatalf("total/shown = %d/%d, components = %d", history.Total, history.Shown, len(doc.Components))
	}
}

func TestReadChangelogHistorySkipsBrokenFiles(t *testing.T) {
	dir := writeHistory(t, map[string]string{"changelog/v0.9.10.json": historyNew, "changelog/v0.9.9.json": "{broken"})
	history, err := hostapi.ReadChangelogHistory(dir, 0)
	if err != nil || history.Total != 1 {
		t.Fatalf("history = %+v, err = %v", history, err)
	}
}

func TestReadChangelogHistoryFallsBackToSingleFile(t *testing.T) {
	dir := writeHistory(t, map[string]string{"changelog.json": historyNew})
	history, err := hostapi.ReadChangelogHistory(dir, 5)
	if err != nil || history.Total != 1 || history.Shown != 1 || !json.Valid(history.Document) {
		t.Fatalf("history = %+v, err = %v", history, err)
	}
	if _, err := hostapi.ReadChangelogHistory(t.TempDir(), 0); err == nil {
		t.Fatal("no changelog at all must be NO_CHANGELOG")
	}
}
