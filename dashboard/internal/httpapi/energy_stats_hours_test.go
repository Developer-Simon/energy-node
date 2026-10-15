package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

func TestEnergyStatsHoursReturnsRows(t *testing.T) {
	store := energystats.NewStore(t.TempDir())
	hour := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	if err := store.SetHA([]energystats.HARecord{{Hour: hour, Role: "pv", KWh: 0.5}}); err != nil {
		t.Fatal(err)
	}
	service := &energystats.Service{Store: store, Location: time.UTC}
	url := "/api/v1/energy/stats/hours?from=" + strconv.FormatInt(hour.UnixMilli(), 10) + "&to=" + strconv.FormatInt(hour.Add(time.Hour).UnixMilli(), 10)
	rec := httptest.NewRecorder()
	handleEnergyStatsHours(service)(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Rows) != 1 || body.Rows[0]["series"] != "role:pv" || body.Rows[0]["avg"] != 500.0 {
		t.Fatalf("Antwort: %s", rec.Body)
	}
}

func TestEnergyStatsHoursErrors(t *testing.T) {
	service := &energystats.Service{Store: energystats.NewStore(t.TempDir()), Location: time.UTC}
	cases := []struct {
		name    string
		service *energystats.Service
		method  string
		query   string
		status  int
		code    string
	}{
		{"ohne Dienst", nil, http.MethodGet, "?from=1&to=2", http.StatusNotImplemented, "energy_stats_unavailable"},
		{"ohne Parameter", service, http.MethodGet, "?from=x", http.StatusBadRequest, "energy_stats_invalid_range"},
		{"leerer Zeitraum", service, http.MethodGet, "?from=3600000&to=3600000", http.StatusBadRequest, "energy_stats_invalid_range"},
		{"POST", service, http.MethodPost, "?from=0&to=3600000", http.StatusMethodNotAllowed, ""},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		handleEnergyStatsHours(c.service)(rec, httptest.NewRequest(c.method, "/api/v1/energy/stats/hours"+c.query, nil))
		if rec.Code != c.status {
			t.Errorf("%s: Status %d, erwartet %d", c.name, rec.Code, c.status)
			continue
		}
		if c.code == "" {
			continue
		}
		var body struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: Antwort ist kein JSON: %v", c.name, err)
		}
		if body.Code != c.code {
			t.Errorf("%s: Code %q, erwartet %q", c.name, body.Code, c.code)
		}
	}
}
