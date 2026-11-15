package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/performance"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// stubPerformance returns one fixed measurement window.
type stubPerformance struct{ snap performance.Snapshot }

func (s stubPerformance) Snapshot(time.Time) performance.Snapshot { return s.snap }

func performanceTestSnapshot() performance.Snapshot {
	shellyRound := 0.4
	return performance.Snapshot{
		MeasuredFor: 2 * time.Hour,
		CPUP95:      32.1,
		Cores:       4,
		ClientsMax:  2,
		ClientsSeen: true,
		RAMFreePct:  61,
		Units: []performance.UnitStats{
			{ID: "shelly", CPUP95: 5, RAMBytes: 23068672, HasRAM: true, RoundS: &shellyRound, MsgPerMin: 126},
			{ID: "automation.service", CPUP95: 1, Fixed: true},
		},
	}
}

// performanceRouter is the authenticated router with the performance page wired
// to a stub window.
func performanceRouter(t *testing.T) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	deps := RouterDependencies{
		Auth: manager,
		Performance: &PerformanceDeps{
			Provider: stubPerformance{snap: performanceTestSnapshot()},
			Rates: func() performance.RateConfig {
				return performance.RateConfig{LiveUpdateS: 3, TickS: 10, RegulatorS: 10, NodeS: 15, Services: map[string]float64{"shelly": 20}}
			},
			Floors: map[string]float64{"shelly": 2},
		},
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, deps)
	return router, manager
}

func TestPerformanceRequiresViewDiagnostics(t *testing.T) {
	router, manager := performanceRouter(t)
	const path = "/api/v1/performance"

	editOnly, _ := loginWithRoles(t, router, manager, "einrichter", []string{auth.RoleEditSetup})
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, sessionRequest(t, path, editOnly, true))
	if !viewForbiddenCode(denied) {
		t.Fatalf("edit_setup alone: status %d, want view_forbidden: %s", denied.Code, denied.Body.String())
	}

	diagnostics, _ := loginWithRoles(t, router, manager, "diagnose", []string{auth.RoleViewDiagnostics})
	allowed := httptest.NewRecorder()
	router.ServeHTTP(allowed, sessionRequest(t, path, diagnostics, true))
	if allowed.Code != http.StatusOK {
		t.Fatalf("view_diagnostics: status %d: %s", allowed.Code, allowed.Body.String())
	}
	var body struct {
		MeasuredS   int              `json:"measured_s"`
		MinMeasureS int              `json:"min_measure_s"`
		Cores       int              `json:"cores"`
		TargetPct   int              `json:"target_pct"`
		DefaultPct  int              `json:"default_target_pct"`
		TodayPct    float64          `json:"today_pct"`
		BasePct     float64          `json:"base_pct"`
		OverSinceS  *float64         `json:"over_since_s"`
		Change      *json.RawMessage `json:"change"`
	}
	if err := json.Unmarshal(allowed.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.MeasuredS != 7200 || body.MinMeasureS != 21600 || body.Cores != 4 {
		t.Fatalf("window = %+v: %s", body, allowed.Body.String())
	}
	if body.TargetPct != 70 || body.DefaultPct != 70 || body.TodayPct != 32.1 {
		t.Fatalf("budget = %+v: %s", body, allowed.Body.String())
	}
	// base = 32.1 - (live_update 0 with no dashboard unit + shelly 5 + 126 msg/min share)
	if body.BasePct < 0 || body.BasePct > 32.1 {
		t.Fatalf("base_pct = %v out of range", body.BasePct)
	}
	if body.OverSinceS != nil || body.Change != nil {
		t.Fatalf("over_since_s and change must be null: %s", allowed.Body.String())
	}
}

func TestPerformanceRowsFollowRights(t *testing.T) {
	router, manager := performanceRouter(t)
	const path = "/api/v1/performance"

	setup, _ := loginWithRoles(t, router, manager, "einrichtung", []string{auth.RoleViewDiagnostics, auth.RoleEditSetup})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, sessionRequest(t, path, setup, true))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Rows []struct {
			ID       string `json:"id"`
			Editable bool   `json:"editable"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Rows) == 0 {
		t.Fatalf("no rows: %s", recorder.Body.String())
	}
	for _, row := range body.Rows {
		if want := row.ID == "live_update"; row.Editable != want {
			t.Fatalf("row %s editable = %v, want %v", row.ID, row.Editable, want)
		}
	}
}

func TestPerformanceWithoutProviderAnswers501(t *testing.T) {
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: manager})
	diagnostics, _ := loginWithRoles(t, router, manager, "diagnose2", []string{auth.RoleViewDiagnostics})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, sessionRequest(t, "/api/v1/performance", diagnostics, true))
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status %d, want 501: %s", recorder.Code, recorder.Body.String())
	}
}
