package host

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func changelogHost(t *testing.T, withChangelog bool) *Host {
	t.Helper()
	dir := t.TempDir()
	if withChangelog {
		if err := os.WriteFile(filepath.Join(dir, "changelog.json"), []byte(`{"schema_version":1,"components":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &Host{
		cfg:       Config{RemoteStateDir: "/state"},
		manifest:  &bundle.Manifest{Version: "v0.7.5"},
		bundleDir: dir,
		client:    &transport.Client{},
	}
}

func stubInstalledManifest(t *testing.T, raw []byte, err error) {
	t.Helper()
	orig := readInstalledManifest
	t.Cleanup(func() { readInstalledManifest = orig })
	readInstalledManifest = func(context.Context, *transport.Client, string) ([]byte, error) { return raw, err }
}

func TestChangelogPairsThePackageChangelogWithWhatTheNodeHasInstalled(t *testing.T) {
	stubInstalledManifest(t, []byte(`{"components":{"dashboard":"v0.7.4"},"steps":[{"dir":"shelly","version":"v0.4.0"}]}`), nil)
	view, err := changelogHost(t, true).Changelog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.BundleVersion != "v0.7.5" || string(view.Document) != `{"schema_version":1,"components":[]}` {
		t.Fatalf("view = %+v", view)
	}
	if view.Installed["dashboard"] != "v0.7.4" || view.Installed["service:shelly"] != "v0.4.0" {
		t.Fatalf("installed = %v", view.Installed)
	}
}

func TestChangelogOnAFreshNodeHasNothingInstalled(t *testing.T) {
	stubInstalledManifest(t, nil, nil)
	view, err := changelogHost(t, true).Changelog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.Installed == nil || len(view.Installed) != 0 {
		t.Fatalf("installed = %#v, want an empty non-nil map", view.Installed)
	}
}

func TestChangelogOfAPackageWithoutOneIsNoChangelog(t *testing.T) {
	stubInstalledManifest(t, nil, nil)
	_, err := changelogHost(t, false).Changelog(context.Background())
	var apiErr *hostapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "NO_CHANGELOG" {
		t.Fatalf("err = %#v, want NO_CHANGELOG", err)
	}
}

func TestChangelogFailsBeforeConnect(t *testing.T) {
	h := changelogHost(t, true)
	h.client = nil
	_, err := h.Changelog(context.Background())
	var apiErr *hostapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "NOT_CONNECTED" || apiErr.Status != http.StatusConflict {
		t.Fatalf("err = %#v, want NOT_CONNECTED/409", err)
	}
}

func TestChangelogTurnsAReadFailureIntoAnError(t *testing.T) {
	stubInstalledManifest(t, nil, errors.New("ssh broke"))
	_, err := changelogHost(t, true).Changelog(context.Background())
	var apiErr *hostapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "CHANGELOG_FAILED" {
		t.Fatalf("err = %#v, want CHANGELOG_FAILED", err)
	}
}
