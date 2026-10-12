package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

func TestEnergyStatsReturnsADay(t *testing.T) {
	store := energystats.NewStore(t.TempDir())
	service := &energystats.Service{Store: store, Location: time.UTC, Now: func() time.Time {
		return time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	}}
	rec := httptest.NewRecorder()
	handleEnergyStats(service)(rec, httptest.NewRequest(http.MethodGet, "/api/v1/energy/stats?period=day&at=2026-10-10", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("Status %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Period  string            `json:"period"`
		Buckets []json.RawMessage `json:"buckets"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Period != "day" || len(body.Buckets) != 24 {
		t.Fatalf("Antwort: %s", rec.Body)
	}
}

func TestEnergyStatsErrors(t *testing.T) {
	service := &energystats.Service{Store: energystats.NewStore(t.TempDir()), Location: time.UTC}
	cases := []struct {
		name    string
		service *energystats.Service
		method  string
		query   string
		status  int
		code    string
	}{
		{"ohne Dienst", nil, http.MethodGet, "?period=day", http.StatusNotImplemented, "energy_stats_unavailable"},
		{"falscher Zeitraum", service, http.MethodGet, "?period=quarter", http.StatusBadRequest, "energy_stats_invalid_period"},
		{"falsches Datum", service, http.MethodGet, "?period=day&at=morgen", http.StatusBadRequest, "energy_stats_invalid_period"},
		{"POST", service, http.MethodPost, "?period=day", http.StatusMethodNotAllowed, ""},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		handleEnergyStats(c.service)(rec, httptest.NewRequest(c.method, "/api/v1/energy/stats"+c.query, nil))
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
