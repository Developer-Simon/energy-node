package hostapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
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
