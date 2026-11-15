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
		MeasuredFor:  2 * time.Hour,
		CPUP95:       32.1,
		Cores:        4,
		ClientsMax:   2,
		ClientsSeen:  true,
		RAMFreePct:   61,
		RAMFreeKnown: true,
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
	return performanceRouterWith(t, performanceTestSnapshot())
}

func performanceRouterWith(t *testing.T, snap performance.Snapshot) (http.Handler, *auth.Manager) {
	t.Helper()
	return performanceRouterWithChanges(t, snap, nil)
}

// performanceRouterWithChanges is performanceRouterWith with a change store attached.
func performanceRouterWithChanges(t *testing.T, snap performance.Snapshot, changes ChangeStore) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	deps := RouterDependencies{
		Auth: manager,
		Performance: &PerformanceDeps{
			Provider: stubPerformance{snap: snap},
			Rates: func() performance.RateConfig {
				return performance.RateConfig{LiveUpdateS: 3, TickS: 10, RegulatorS: 10, NodeS: 15, Services: map[string]float64{"shelly": 20}}
			},
			Floors:  map[string]float64{"shelly": 2},
			Changes: changes,
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

// Ohne RAM-Probe und ohne Temperatur liefert die Seite null statt 0, damit sie
// nicht als "0 % frei" oder "0 Grad" erscheint.
func TestPerformanceUnknownDeviceValuesAreNull(t *testing.T) {
	snap := performanceTestSnapshot()
	snap.RAMFreeKnown = false
	snap.RAMFreePct = 0
	snap.TempC = nil
	router, manager := performanceRouterWith(t, snap)

	diagnostics, _ := loginWithRoles(t, router, manager, "diagnose", []string{auth.RoleViewDiagnostics})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, sessionRequest(t, "/api/v1/performance", diagnostics, true))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Device struct {
			TempC      *float64 `json:"temp_c"`
			RAMFreePct *float64 `json:"ram_free_pct"`
		} `json:"device"`
		Deduction struct {
			Reasons []string `json:"reasons"`
		} `json:"deduction"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Device.RAMFreePct != nil || body.Device.TempC != nil {
		t.Fatalf("device = %+v, want null ram_free_pct and temp_c: %s", body.Device, recorder.Body.String())
	}
	for _, reason := range body.Deduction.Reasons {
		if reason == "performance.deduction.ram" {
			t.Fatalf("warmup must not deduct for RAM: %s", recorder.Body.String())
		}
	}
	if strings.Contains(recorder.Body.String(), `"null"`) {
		t.Fatalf("the string null leaked into the body: %s", recorder.Body.String())
	}
}

func TestPerformanceReturnsStoredChange(t *testing.T) {
	monitor := performance.NewMonitor(nil, nil, performance.Options{})
	if err := monitor.AttachChange(t.TempDir(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := monitor.RecordChange(performance.Change{
		At:        time.Now(),
		Before:    map[performance.RowID]float64{performance.RowShelly: 5},
		After:     map[performance.RowID]float64{performance.RowShelly: 20},
		Expected:  40,
		Available: 70,
		State:     "running",
	}); err != nil {
		t.Fatal(err)
	}
	router, manager := performanceRouterWithChanges(t, performanceTestSnapshot(), monitor)

	diagnostics, _ := loginWithRoles(t, router, manager, "diagnose", []string{auth.RoleViewDiagnostics})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, sessionRequest(t, "/api/v1/performance", diagnostics, true))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Change *struct {
			State string             `json:"state"`
			After map[string]float64 `json:"after"`
		} `json:"change"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Change == nil || body.Change.State != "running" || body.Change.After["shelly"] != 20 {
		t.Fatalf("change = %+v, want running with shelly 20: %s", body.Change, recorder.Body.String())
	}
}
