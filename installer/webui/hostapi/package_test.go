package hostapi_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func TestBootstrapCarriesThePackageInfo(t *testing.T) {
	server, fake := newTestServer(t, nil)
	fake.Description.Package = &hostapi.PackageInfo{
		Bundled: &hostapi.BundledInfo{Version: "v1.4.2", Arch: "armv6"},
		Repo:    hostapi.RepoInfo{Available: true, Path: "/home/dev/energy-node"},
	}
	rec := do(t, server, http.MethodGet, "/api/bootstrap", "")
	var got hostapi.Bootstrap
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Package == nil || got.Package.Bundled == nil || got.Package.Repo.Path != "/home/dev/energy-node" {
		t.Errorf("bootstrap.package = %+v", got.Package)
	}
}

func TestPutPackageRecordsTheSelection(t *testing.T) {
	server, fake := newTestServer(t, nil)
	rec := do(t, server, http.MethodPut, "/api/package", `{"kind":"repo","path":"/home/dev/energy-node"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if fake.LastPackage.Kind != "repo" || fake.LastPackage.Path != "/home/dev/energy-node" {
		t.Errorf("LastPackage = %+v", fake.LastPackage)
	}
}

func TestUploadStreamsTheFilePartToTheBackend(t *testing.T) {
	server, fake := newTestServer(t, nil)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, _ := w.CreateFormFile("file", "energy-node-v1-armv6.tar.gz")
	part.Write([]byte("archive-bytes"))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/package/upload", &body)
	req.Header.Set("X-Installer-Token", testToken)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if fake.UploadedName != "energy-node-v1-armv6.tar.gz" || fake.UploadedBytes != len("archive-bytes") {
		t.Errorf("uploaded %q, %d bytes", fake.UploadedName, fake.UploadedBytes)
	}
}

func TestUploadWithoutAFilePartIsABadRequest(t *testing.T) {
	server, _ := newTestServer(t, nil)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.WriteField("other", "x")
	w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/package/upload", &body)
	req.Header.Set("X-Installer-Token", testToken)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPrepareRunStreamsItsLogAndFinishes(t *testing.T) {
	server, fake := newTestServer(t, nil)
	fake.PrepareLog = []string{"Suche das neueste Release", "Lade energy-node-v1-armv6.tar.gz"}
	do(t, server, http.MethodPost, "/api/connect", `{"host":"n","user":"u","kind":"password","secret":"x"}`)
	rec := do(t, server, http.MethodPost, "/api/run", `{"mode":"prepare"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	events := do(t, server, http.MethodGet, "/api/events?once=1", "").Body.String()
	waitFor(t, func() bool {
		events = do(t, server, http.MethodGet, "/api/events?once=1", "").Body.String()
		return strings.Contains(events, "run-finished")
	})
	if !strings.Contains(events, "Suche das neueste Release") || !strings.Contains(events, `"step_id":"package"`) {
		t.Errorf("events lack the prepare log:\n%s", events)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met in time")
}
