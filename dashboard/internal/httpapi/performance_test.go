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
	return performanceRouterWithDeps(t, snap, func(deps *PerformanceDeps) { deps.Changes = changes })
}

// performanceRouterWithDeps is the performance router with mutate applied to
// the performance deps before the router is built.
func performanceRouterWithDeps(t *testing.T, snap performance.Snapshot, mutate func(*PerformanceDeps)) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	perf := &PerformanceDeps{
		Provider: stubPerformance{snap: snap},
		Rates: func() performance.RateConfig {
			return performance.RateConfig{LiveUpdateS: 3, TickS: 10, RegulatorS: 10, NodeS: 15, Services: map[string]float64{"shelly": 20}}
		},
		Floors: map[string]float64{"shelly": 2},
	}
	if mutate != nil {
		mutate(perf)
	}
	deps := RouterDependencies{Auth: manager, Performance: perf}
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

// breakdownSnapshot is the test window with a dashboard split and the dashboard unit.
func breakdownSnapshot() performance.Snapshot {
	snap := performanceTestSnapshot()
	snap.Units = append(snap.Units, performance.UnitStats{ID: performance.DashboardUnit, CPUP95: 10})
	snap.ClientsMax = 3
	snap.Dashboard = &performance.DashboardSplit{
		GCShare: 0.1,
		Shares: map[performance.Part]float64{
			performance.PartPush:       0.4,
			performance.PartHTTP:       0.3,
			performance.PartMQTT:       0.1,
			performance.PartNode:       0.1,
			performance.PartBackground: 0.1,
		},
	}
	return snap
}

// getPerformance reads GET /api/v1/performance as a diagnostics viewer.
func getPerformance(t *testing.T, router http.Handler, manager *auth.Manager) []byte {
	t.Helper()
	diagnostics, _ := loginWithRoles(t, router, manager, "breakdown", []string{auth.RoleViewDiagnostics})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, sessionRequest(t, "/api/v1/performance", diagnostics, true))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	return recorder.Body.Bytes()
}

func TestPerformanceBreakdownFields(t *testing.T) {
	router, manager := performanceRouterWithDeps(t, breakdownSnapshot(), func(deps *PerformanceDeps) {
		deps.Automation = func() []performance.AutomationEntry {
			return []performance.AutomationEntry{
				{ID: "nacht", Name: "Nachtladen", Kind: "rule"},
				{ID: "licht", Name: "Licht", Kind: "rule"},
			}
		}
	})
	var body struct {
		Breakdown struct {
			Dashboard *struct {
				Parts []struct {
					ID      string  `json:"id"`
					Pct     float64 `json:"pct"`
					Clients *int    `json:"clients"`
				} `json:"parts"`
			} `json:"dashboard"`
			Automation *struct {
				Groups []struct {
					Row   string `json:"row"`
					Items []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
						Kind string `json:"kind"`
					} `json:"items"`
				} `json:"groups"`
			} `json:"automation"`
		} `json:"breakdown"`
	}
	raw := getPerformance(t, router, manager)
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Breakdown.Dashboard == nil {
		t.Fatalf("dashboard breakdown is null: %s", raw)
	}
	wantIDs := []string{"push", "http", "mqtt", "node", "background", "gc"}
	parts := body.Breakdown.Dashboard.Parts
	if len(parts) != len(wantIDs) {
		t.Fatalf("parts = %+v, want ids %v: %s", parts, wantIDs, raw)
	}
	for i, id := range wantIDs {
		if parts[i].ID != id {
			t.Fatalf("part %d = %s, want %s: %s", i, parts[i].ID, id, raw)
		}
		if (parts[i].Clients != nil) != (id == "push") {
			t.Fatalf("part %s clients = %v, only push carries clients: %s", id, parts[i].Clients, raw)
		}
	}
	if parts[0].Clients == nil || *parts[0].Clients != 3 {
		t.Fatalf("push clients = %v, want 3: %s", parts[0].Clients, raw)
	}
	if body.Breakdown.Automation == nil || len(body.Breakdown.Automation.Groups) == 0 {
		t.Fatalf("automation breakdown missing: %s", raw)
	}
	group := body.Breakdown.Automation.Groups[0]
	if group.Row != "tick" || len(group.Items) != 2 {
		t.Fatalf("group = %+v, want tick with two items: %s", group, raw)
	}
	if group.Items[0].Name != "Licht" || group.Items[1].Name != "Nachtladen" || group.Items[0].Kind != "rule" {
		t.Fatalf("items = %+v, want Licht then Nachtladen: %s", group.Items, raw)
	}
}

func TestPerformanceBreakdownNullWithoutSplit(t *testing.T) {
	router, manager := performanceRouterWith(t, performanceTestSnapshot())
	var body struct {
		Breakdown struct {
			Dashboard  *json.RawMessage `json:"dashboard"`
			Automation *json.RawMessage `json:"automation"`
		} `json:"breakdown"`
	}
	raw := getPerformance(t, router, manager)
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Breakdown.Dashboard != nil || body.Breakdown.Automation != nil {
		t.Fatalf("breakdown must be null without split and groups: %s", raw)
	}
}

func TestPerformanceRegulatorRowFollowsScenarios(t *testing.T) {
	cases := []struct {
		name    string
		entries []performance.AutomationEntry
		want    bool
	}{
		{"rules only", []performance.AutomationEntry{{ID: "licht", Name: "Licht", Kind: "rule"}}, false},
		{"scenario with regulator", []performance.AutomationEntry{{ID: "nacht", Name: "Nacht", Kind: "scenario", Regulator: true}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, manager := performanceRouterWithDeps(t, breakdownSnapshot(), func(deps *PerformanceDeps) {
				deps.Automation = func() []performance.AutomationEntry { return tc.entries }
			})
			var body struct {
				Rows []struct {
					ID string `json:"id"`
				} `json:"rows"`
			}
			raw := getPerformance(t, router, manager)
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range body.Rows {
				if row.ID == "regulator" {
					found = true
				}
			}
			if found != tc.want {
				t.Fatalf("regulator row present = %v, want %v: %s", found, tc.want, raw)
			}
		})
	}
}
