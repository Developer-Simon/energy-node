package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// efficiencyTestService has one measurement chain and a fixed clock, so the
// answers are the same on every run.
func efficiencyTestService(t *testing.T) *energystats.Service {
	t.Helper()
	return &energystats.Service{
		Store:      energystats.NewStore(t.TempDir()),
		ChainStore: energystats.NewChainStore(t.TempDir(), nil),
		Chains: func() []energystats.ChainInfo {
			return []energystats.ChainInfo{{ID: "balkon", Chain: energy.Chain{Label: "Balkon", Points: []string{"plug", "ac"}, Counting: "plug"}}}
		},
		Location: time.UTC,
		Now: func() time.Time {
			return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
		},
	}
}

// efficiencyRouter is the authenticated router with the energy stats service
// wired in, so the view gates of the new routes are the ones that run.
func efficiencyRouter(t *testing.T) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	deps := RouterDependencies{Auth: manager, EnergyStats: efficiencyTestService(t)}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, deps)
	return router, manager
}

func TestEfficiencyEndpointNeedsViewEfficiency(t *testing.T) {
	router, manager := efficiencyRouter(t)
	const path = "/api/v1/energy/efficiency"

	statsOnly, _ := loginWithRoles(t, router, manager, "statistik", []string{auth.RoleViewStats})
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, sessionRequest(t, path, statsOnly, true))
	if !viewForbiddenCode(denied) {
		t.Fatalf("view_stats alone: status %d, want view_forbidden: %s", denied.Code, denied.Body.String())
	}

	efficiency, _ := loginWithRoles(t, router, manager, "wirkungsgrad", []string{auth.RoleViewEfficiency})
	allowed := httptest.NewRecorder()
	router.ServeHTTP(allowed, sessionRequest(t, path, efficiency, true))
	if allowed.Code != http.StatusOK {
		t.Fatalf("view_efficiency: status %d: %s", allowed.Code, allowed.Body.String())
	}
	var body struct {
		Chains []struct {
			ID string `json:"id"`
		} `json:"chains"`
	}
	if err := json.Unmarshal(allowed.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Chains) != 1 || body.Chains[0].ID != "balkon" {
		t.Fatalf("chains = %+v, want the balkon chain: %s", body.Chains, allowed.Body.String())
	}
}

func TestChainStatsEndpointNeedsViewStats(t *testing.T) {
	router, manager := efficiencyRouter(t)
	const path = "/api/v1/energy/stats/chains?period=day&at=2026-11-06"

	efficiencyOnly, _ := loginWithRoles(t, router, manager, "effizienz", []string{auth.RoleViewEfficiency})
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, sessionRequest(t, path, efficiencyOnly, true))
	if !viewForbiddenCode(denied) {
		t.Fatalf("view_efficiency alone: status %d, want view_forbidden: %s", denied.Code, denied.Body.String())
	}

	statsOnly, _ := loginWithRoles(t, router, manager, "statistik2", []string{auth.RoleViewStats})
	allowed := httptest.NewRecorder()
	router.ServeHTTP(allowed, sessionRequest(t, path, statsOnly, true))
	if allowed.Code != http.StatusOK {
		t.Fatalf("view_stats: status %d: %s", allowed.Code, allowed.Body.String())
	}
	var body struct {
		Period string `json:"period"`
		Chains []struct {
			ID      string `json:"id"`
			Buckets []any  `json:"buckets"`
		} `json:"chains"`
	}
	if err := json.Unmarshal(allowed.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Period != "day" || len(body.Chains) != 1 || len(body.Chains[0].Buckets) != 24 {
		t.Fatalf("answer = %s, want one chain with 24 hour buckets", allowed.Body.String())
	}
}

func TestEfficiencyEndpointWithoutServiceReturns501(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"efficiency": handleEnergyEfficiency(nil, nil),
		"chains":     handleEnergyChainStats(nil),
	}
	for name, handler := range cases {
		recorder := httptest.NewRecorder()
		handler(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/energy/"+name, nil))
		if recorder.Code != http.StatusNotImplemented {
			t.Fatalf("%s without service: status %d, want 501", name, recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), `"code":"energy_stats_unavailable"`) {
			t.Fatalf("%s without service: body %s", name, recorder.Body.String())
		}
	}
}

func TestChainStatsRejectsInvalidPeriod(t *testing.T) {
	recorder := httptest.NewRecorder()
	handleEnergyChainStats(efficiencyTestService(t))(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/energy/stats/chains?period=quarter", nil))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"energy_stats_invalid_period"`) {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body.String())
	}
}

func TestEfficiencyReportsLearningDisabled(t *testing.T) {
	service := efficiencyTestService(t)
	cases := map[string]struct {
		learningOn func() bool
		want       string
	}{
		"learning off": {func() bool { return false }, "true"},
		"learning on":  {func() bool { return true }, "false"},
		"no monitor":   {nil, "false"},
	}
	for name, tc := range cases {
		recorder := httptest.NewRecorder()
		handleEnergyEfficiency(service, tc.learningOn)(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/energy/efficiency", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", name, recorder.Code, recorder.Body.String())
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if string(body["learning_disabled"]) != tc.want {
			t.Errorf("%s: learning_disabled = %s, want %s", name, body["learning_disabled"], tc.want)
		}
		if _, ok := body["chains"]; !ok {
			t.Errorf("%s: chains missing from %s", name, recorder.Body.String())
		}
	}
}
