package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleUpdatesProgressNamesTheStep(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "current.json"), []byte(`{"bundle_version":"v0.9.1","steps":["10","60"],"run_id":"r1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "log"), []byte("1 ##STEP 10 ok\n2 ##STEP 60 begin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	title := func(_ *http.Request, id string) string { return "Schritt-" + id }

	rec := httptest.NewRecorder()
	handleUpdatesProgress(dir, title)(rec, httptest.NewRequest(http.MethodGet, "/api/v1/updates/progress", nil))

	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("got %d, cache-control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	var body struct {
		State string `json:"state"`
		Done  int    `json:"done"`
		Step  struct{ ID, Title string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.State != "running" || body.Done != 1 || body.Step.ID != "60" || body.Step.Title != "Schritt-60" {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestHandleUpdatesProgressIdleWithoutJob(t *testing.T) {
	rec := httptest.NewRecorder()
	handleUpdatesProgress(t.TempDir(), nil)(rec, httptest.NewRequest(http.MethodGet, "/api/v1/updates/progress", nil))
	if rec.Code != http.StatusOK || !json.Valid(rec.Body.Bytes()) || !strings.Contains(rec.Body.String(), `"state":"idle"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpdatesProgressRejectsPost(t *testing.T) {
	rec := httptest.NewRecorder()
	handleUpdatesProgress(t.TempDir(), nil)(rec, httptest.NewRequest(http.MethodPost, "/api/v1/updates/progress", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestHandleUpdatesProgressBrokenStatusIs500(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "current.json"), []byte(`{"steps":["10"]}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "status.json"), []byte(`{`), 0o644)
	rec := httptest.NewRecorder()
	handleUpdatesProgress(dir, nil)(rec, httptest.NewRequest(http.MethodGet, "/api/v1/updates/progress", nil))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "updates_progress_unreadable") {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}
