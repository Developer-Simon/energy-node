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
