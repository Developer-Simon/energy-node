package hostapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func connectFirst(t *testing.T, server *hostapi.Server) {
	t.Helper()
	if rec := do(t, server, http.MethodPost, "/api/connect", `{"host":"h","user":"u","kind":"password","secret":"pw"}`); rec.Code != http.StatusOK {
		t.Fatalf("connect failed: %s", rec.Body.String())
	}
}

func TestPrecheckReturnsTheBackendsReport(t *testing.T) {
	server, _ := newTestServer(t, nil)
	connectFirst(t, server)
	rec := do(t, server, http.MethodGet, "/api/precheck", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got hostapi.Precheck
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if got.Arch != "armv6l" || !got.ArchOK || got.PythonABI != "cp311" {
		t.Errorf("Precheck = %+v", got)
	}
}

func TestPrecheckIsRefusedWithoutAConnection(t *testing.T) {
	server, _ := newTestServer(t, nil)
	rec := do(t, server, http.MethodGet, "/api/precheck", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 NOT_CONNECTED", rec.Code)
	}
}

func TestManifestCarriesStepsAndComponents(t *testing.T) {
	server, _ := newTestServer(t, nil)
	connectFirst(t, server)
	rec := do(t, server, http.MethodGet, "/api/manifest", "")
	var got hostapi.ManifestView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if len(got.Steps) != 3 || got.Components["dashboard"] == "" {
		t.Fatalf("ManifestView = %+v", got)
	}
	var optional int
	for _, step := range got.Steps {
		if step.Optional {
			optional++
		}
	}
	if optional != 2 {
		t.Errorf("optional steps = %d, want 2 - the configuration screen builds its checkboxes from this", optional)
	}
}

func TestSelectionRoundTrips(t *testing.T) {
	server, fake := newTestServer(t, nil)
	connectFirst(t, server)

	rec := do(t, server, http.MethodPut, "/api/selection", `{"steps":{"40":false,"85":true}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fake.SavedSelected["40"] || !fake.SavedSelected["85"] {
		t.Errorf("the backend saved %v", fake.SavedSelected)
	}

	rec = do(t, server, http.MethodGet, "/api/selection", "")
	var got hostapi.SelectionView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if got.Steps["40"] || !got.Steps["85"] {
		t.Errorf("SelectionView = %+v, want the saved selection", got)
	}
}

func TestSelectionRejectsAMalformedBody(t *testing.T) {
	server, _ := newTestServer(t, nil)
	connectFirst(t, server)
	rec := do(t, server, http.MethodPut, "/api/selection", `{"steps":"all"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPlanShowsFromAndToPerComponent(t *testing.T) {
	server, _ := newTestServer(t, nil)
	connectFirst(t, server)
	rec := do(t, server, http.MethodGet, "/api/plan", "")
	var got hostapi.PlanView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	delta, ok := got.Components["dashboard"]
	if !ok {
		t.Fatalf("PlanView carries no component delta: %+v", got)
	}
	if delta.From == nil || *delta.From != "v2.0.0" || delta.To != "v2.1.0" {
		t.Errorf("dashboard delta = %+v, want v2.0.0 -> v2.1.0", delta)
	}
}

func TestPlanKeepsAnUnknownFromAsNull(t *testing.T) {
	server, fake := newTestServer(t, nil)
	connectFirst(t, server)
	fake.PlanResult.Components = map[string]hostapi.ComponentDelta{"dashboard": {From: nil, To: "v2.1.0"}}
	rec := do(t, server, http.MethodGet, "/api/plan", "")
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	components := raw["components"].(map[string]any)
	dashboard := components["dashboard"].(map[string]any)
	if dashboard["from"] != nil {
		t.Errorf(`from = %v, want JSON null - "unknown" is not ""`, dashboard["from"])
	}
}

func TestABackendErrorBecomesBackendError(t *testing.T) {
	server, fake := newTestServer(t, nil)
	connectFirst(t, server)
	fake.PlanErr = hostapitestErr("plan.sh exited with 2")
	rec := do(t, server, http.MethodGet, "/api/plan", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var payload map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	if payload["error"] != "BACKEND_ERROR" || payload["detail"] == "" {
		t.Errorf("payload = %v, want BACKEND_ERROR with a detail", payload)
	}
}

type plainErr string

func (e plainErr) Error() string { return string(e) }

func hostapitestErr(s string) error { return plainErr(s) }
