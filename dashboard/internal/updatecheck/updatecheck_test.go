package updatecheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func serverWithRelease(t *testing.T, tagName string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/Developer-Simon/energy-node/releases/latest" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name":     tagName,
			"html_url":     "https://github.com/Developer-Simon/energy-node/releases/tag/" + tagName,
			"published_at": "2026-09-01T00:00:00Z",
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestCheckReportsAnAvailableUpdate(t *testing.T) {
	server := serverWithRelease(t, "v1.4.2")
	checker := &Checker{Repo: "Developer-Simon/energy-node", BaseURL: server.URL, Now: func() time.Time { return time.Date(2026, 9, 16, 3, 0, 0, 0, time.UTC) }}

	result, err := checker.Check(context.Background(), "1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Available {
		t.Fatalf("expected an available update, got %+v", result)
	}
	if result.Latest != "1.4.2" {
		t.Fatalf("latest = %q, want stripped of the v-prefix", result.Latest)
	}
	if result.NotesURL == "" {
		t.Fatal("expected a notes URL")
	}
	if result.CheckedAt.IsZero() {
		t.Fatal("expected CheckedAt to be set")
	}
}

func TestCheckReportsUpToDate(t *testing.T) {
	server := serverWithRelease(t, "v1.4.1")
	checker := &Checker{Repo: "Developer-Simon/energy-node", BaseURL: server.URL}

	result, err := checker.Check(context.Background(), "1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Available {
		t.Fatalf("expected no update, got %+v", result)
	}
}

func TestCheckIgnoresAPatchAheadOfGitHub(t *testing.T) {
	// A locally built dev binary can be ahead of the last tagged release.
	server := serverWithRelease(t, "v1.4.1")
	checker := &Checker{Repo: "Developer-Simon/energy-node", BaseURL: server.URL}

	result, err := checker.Check(context.Background(), "1.5.0")
	if err != nil {
		t.Fatal(err)
	}
	if result.Available {
		t.Fatalf("a newer local version must never be reported as an available update: %+v", result)
	}
}

func TestCheckHandlesTheVPrefixMainBuildVersionActuallyCarries(t *testing.T) {
	// main.buildVersion is built from dashboard/VERSION ("vX.Y.Z") plus an
	// optional "-dev"/"-branch.N" suffix -- never a bare "1.4.1". A regex
	// requiring a leading digit would silently make every real deployment
	// report "no update available", always, regardless of the actual
	// comparison.
	server := serverWithRelease(t, "v1.4.2")
	checker := &Checker{Repo: "Developer-Simon/energy-node", BaseURL: server.URL}

	result, err := checker.Check(context.Background(), "v1.4.1-dev")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Available {
		t.Fatalf("expected the v-prefixed, suffixed current version to compare correctly: %+v", result)
	}
}

func TestCheckSkipsComparisonForANonVersionCurrent(t *testing.T) {
	server := serverWithRelease(t, "v1.4.2")
	checker := &Checker{Repo: "Developer-Simon/energy-node", BaseURL: server.URL}

	result, err := checker.Check(context.Background(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if result.Available {
		t.Fatalf("a non-release dev build must not claim an update is available: %+v", result)
	}
}

func TestCheckHandlesADoubleDigitPatchCorrectly(t *testing.T) {
	// A naive string compare would rank "1.4.9" above "1.4.10".
	server := serverWithRelease(t, "v1.4.10")
	checker := &Checker{Repo: "Developer-Simon/energy-node", BaseURL: server.URL}

	result, err := checker.Check(context.Background(), "1.4.9")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Available {
		t.Fatalf("expected 1.4.10 to be recognised as newer than 1.4.9: %+v", result)
	}
}

func TestCheckReturnsAnErrorOnANon200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	checker := &Checker{Repo: "Developer-Simon/energy-node", BaseURL: server.URL}

	if _, err := checker.Check(context.Background(), "1.4.1"); err == nil {
		t.Fatal("expected an error for a non-200 response")
	}
}

func TestCacheStartsEmpty(t *testing.T) {
	var cache Cache
	if _, has := cache.Get(); has {
		t.Fatal("a fresh cache must report no cached result")
	}
}

func TestCacheReturnsWhatWasSet(t *testing.T) {
	var cache Cache
	cache.Set(Result{Current: "1.4.1", Latest: "1.4.2", Available: true})
	result, has := cache.Get()
	if !has {
		t.Fatal("expected a cached result after Set")
	}
	if result.Latest != "1.4.2" {
		t.Fatalf("result = %+v", result)
	}
}
