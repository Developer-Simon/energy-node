package hostapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func TestBootstrapCarriesAutoPrepare(t *testing.T) {
	server, fake := newTestServer(t, nil)
	fake.Description.AutoPrepare = true
	rec := do(t, server, http.MethodGet, "/api/bootstrap", "")
	var got hostapi.Bootstrap
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.AutoPrepare {
		t.Errorf("bootstrap.auto_prepare = false, want true: %s", rec.Body)
	}

	fake.Description.AutoPrepare = false
	rec = do(t, server, http.MethodGet, "/api/bootstrap", "")
	if strings.Contains(rec.Body.String(), "auto_prepare") {
		t.Errorf("auto_prepare must be omitted when false: %s", rec.Body)
	}
}

func TestPrepareRunNeedsNoConnectionWhenTheHostAsksForNone(t *testing.T) {
	server, fake := newTestServer(t, nil)
	fake.Description.NeedsConnection = false
	fake.PrepareLog = []string{"Lade paket"}
	rec := do(t, server, http.MethodPost, "/api/run", `{"mode":"prepare"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	waitFor(t, func() bool {
		return strings.Contains(do(t, server, http.MethodGet, "/api/events?once=1", "").Body.String(), "run-finished")
	})
}
