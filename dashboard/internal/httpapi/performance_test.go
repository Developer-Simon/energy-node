package httpapi

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
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
	return performanceRouterWithStore(t, settings.NewStore(t.TempDir()), snap, mutate)
}

// performanceRouterWithStore is performanceRouterWithDeps with the settings store
// given, so a test can change a setting the page reads.
func performanceRouterWithStore(t *testing.T, store *settings.Store, snap performance.Snapshot, mutate func(*PerformanceDeps)) (http.Handler, *auth.Manager) {
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
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), store, nil, nil, nil, nil, nil, deps)
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

// learningBlockBody reads the learning block of GET /api/v1/performance.
type learningBlockBody struct {
	Learning *struct {
		Setting  string  `json:"setting"`
		Level    string  `json:"level"`
		Share    float64 `json:"share"`
		Target   float64 `json:"target"`
		PerRunMs int64   `json:"per_run_ms"`
		Top      []struct {
			Detector string `json:"detector"`
			Ms       int64  `json:"ms"`
		} `json:"top"`
		Suggest *string `json:"suggest"`
	} `json:"learning"`
}

// performanceLearningStats is an engine load with four detectors over two rounds.
func performanceLearningStats() autolearn.Stats {
	return autolearn.Stats{
		Rounds:   2,
		Share:    0.021,
		Overruns: 0,
		Took: map[string]time.Duration{
			"switch_time": 420 * time.Millisecond,
			"runtime":     210 * time.Millisecond,
			"coupling":    90 * time.Millisecond,
			"flapping":    40 * time.Millisecond,
		},
	}
}

// learningDayShares fills the seven days before now with share.
func learningDayShares(now time.Time, days int, share float64) map[string]float64 {
	out := map[string]float64{}
	for i := 1; i <= days; i++ {
		out[now.AddDate(0, 0, -i).Format("2006-01-02")] = share
	}
	return out
}

func TestPerformanceLearningBlockTopDescending(t *testing.T) {
	stats := performanceLearningStats()
	router, manager := performanceRouterWithDeps(t, performanceTestSnapshot(), func(deps *PerformanceDeps) {
		deps.Learning = &PerformanceLearning{
			Load:  func() autolearn.Stats { return stats },
			State: func() learnrec.State { return learnrec.State{} },
		}
	})
	raw := getPerformance(t, router, manager)
	var body learningBlockBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Learning == nil {
		t.Fatal("learning block missing")
	}
	// Auto on four cores resolves to balanced, so the target is 0.10.
	if body.Learning.Setting != "auto" || body.Learning.Level != "balanced" || body.Learning.Target != 0.10 {
		t.Fatalf("setting %q level %q target %v", body.Learning.Setting, body.Learning.Level, body.Learning.Target)
	}
	if body.Learning.Share != 0.021 {
		t.Fatalf("share = %v", body.Learning.Share)
	}
	// Four detectors over two rounds: 760 ms in total, 380 ms per round.
	if body.Learning.PerRunMs != 380 {
		t.Fatalf("per_run_ms = %d, want 380", body.Learning.PerRunMs)
	}
	want := []string{"switch_time", "runtime", "coupling"}
	if len(body.Learning.Top) != len(want) {
		t.Fatalf("top has %d entries, want %d", len(body.Learning.Top), len(want))
	}
	for i, name := range want {
		if body.Learning.Top[i].Detector != name {
			t.Fatalf("top[%d] = %q, want %q", i, body.Learning.Top[i].Detector, name)
		}
	}
	for i := 1; i < len(body.Learning.Top); i++ {
		if body.Learning.Top[i].Ms > body.Learning.Top[i-1].Ms {
			t.Fatalf("top not descending: %+v", body.Learning.Top)
		}
	}
	if body.Learning.Suggest != nil {
		t.Fatalf("suggest = %q without a suggestion", *body.Learning.Suggest)
	}
	if strings.Contains(string(raw), `"suggest"`) {
		t.Fatal("suggest key present without a suggestion")
	}
}

func TestPerformanceLearningBlockNullWithoutDeps(t *testing.T) {
	router, manager := performanceRouter(t)
	if !strings.Contains(string(getPerformance(t, router, manager)), `"learning":null`) {
		t.Fatal("learning block is not null without the learning deps")
	}
}

func TestPerformanceLearningSuggestsUpAfterSevenDays(t *testing.T) {
	now := time.Now()
	stats := performanceLearningStats()
	for _, tc := range []struct {
		name    string
		days    int
		suggest bool
	}{
		{"seven quiet days", 7, true},
		{"six quiet days", 6, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := settings.NewStore(t.TempDir())
			value, err := store.LoadSettings()
			if err != nil {
				t.Fatal(err)
			}
			value.LearningLevel = "eco"
			if err := store.SaveSettings(value); err != nil {
				t.Fatal(err)
			}
			router, manager := performanceRouterWithStore(t, store, performanceTestSnapshot(), func(deps *PerformanceDeps) {
				deps.Learning = &PerformanceLearning{
					Load:  func() autolearn.Stats { return stats },
					State: func() learnrec.State { return learnrec.State{DailyShare: learningDayShares(now, tc.days, 0.02)} },
				}
			})
			var body learningBlockBody
			if err := json.Unmarshal(getPerformance(t, router, manager), &body); err != nil {
				t.Fatal(err)
			}
			if body.Learning == nil || body.Learning.Level != "eco" || body.Learning.Target != 0.05 {
				t.Fatalf("learning block %+v, want eco with target 0.05", body.Learning)
			}
			if tc.suggest {
				if body.Learning.Suggest == nil || *body.Learning.Suggest != "balanced" {
					t.Fatalf("suggest = %v, want balanced", body.Learning.Suggest)
				}
			} else if body.Learning.Suggest != nil {
				t.Fatalf("suggest = %q, want none", *body.Learning.Suggest)
			}
		})
	}
}

func TestPerformanceLearningFollowsChangedSetting(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	stats := performanceLearningStats()
	router, manager := performanceRouterWithStore(t, store, performanceTestSnapshot(), func(deps *PerformanceDeps) {
		deps.Learning = &PerformanceLearning{
			Load:  func() autolearn.Stats { return stats },
			State: func() learnrec.State { return learnrec.State{} },
		}
	})
	value, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	value.LearningLevel = "full"
	if err := store.SaveSettings(value); err != nil {
		t.Fatal(err)
	}
	var body learningBlockBody
	if err := json.Unmarshal(getPerformance(t, router, manager), &body); err != nil {
		t.Fatal(err)
	}
	if body.Learning == nil || body.Learning.Setting != "full" || body.Learning.Level != "full" {
		t.Fatalf("learning block %+v, want full after the change", body.Learning)
	}
}

// learningPartBody reads the dashboard breakdown of GET /api/v1/performance,
// with the learning part and its detectors, and the units.
type learningPartBody struct {
	Units []struct {
		ID string `json:"id"`
	} `json:"units"`
	Breakdown struct {
		Dashboard *struct {
			Parts []struct {
				ID        string  `json:"id"`
				Pct       float64 `json:"pct"`
				Detectors []struct {
					ID  string  `json:"id"`
					Pct float64 `json:"pct"`
				} `json:"detectors"`
			} `json:"parts"`
		} `json:"dashboard"`
	} `json:"breakdown"`
}

// learningPart returns the learning part of the dashboard breakdown, nil when absent.
func learningPart(body learningPartBody) *struct {
	ID        string  `json:"id"`
	Pct       float64 `json:"pct"`
	Detectors []struct {
		ID  string  `json:"id"`
		Pct float64 `json:"pct"`
	} `json:"detectors"`
} {
	if body.Breakdown.Dashboard == nil {
		return nil
	}
	for i := range body.Breakdown.Dashboard.Parts {
		if body.Breakdown.Dashboard.Parts[i].ID == "learning" {
			return &body.Breakdown.Dashboard.Parts[i]
		}
	}
	return nil
}

func TestPerformanceLearningPartInDashboard(t *testing.T) {
	stats := performanceLearningStats()
	router, manager := performanceRouterWithDeps(t, breakdownSnapshot(), func(deps *PerformanceDeps) {
		deps.Learning = &PerformanceLearning{
			Load:  func() autolearn.Stats { return stats },
			State: func() learnrec.State { return learnrec.State{} },
		}
	})
	var body learningPartBody
	if err := json.Unmarshal(getPerformance(t, router, manager), &body); err != nil {
		t.Fatal(err)
	}
	for _, unit := range body.Units {
		if unit.ID == "learning" {
			t.Fatal("learning is a unit, it belongs in the dashboard breakdown only")
		}
	}

	part := learningPart(body)
	if part == nil {
		t.Fatalf("no learning part in the dashboard parts: %+v", body.Breakdown.Dashboard)
	}
	// Share 2.1 % of one core on four cores is 0.525 % of the budget.
	if math.Abs(part.Pct-0.525) > 1e-9 {
		t.Fatalf("learning pct = %v, want 0.525", part.Pct)
	}
	if last := body.Breakdown.Dashboard.Parts[len(body.Breakdown.Dashboard.Parts)-1]; last.ID != "learning" {
		t.Fatalf("learning part is not last: %+v", body.Breakdown.Dashboard.Parts)
	}

	// The parts still add up to the dashboard unit, 10 % in the snapshot.
	var sum float64
	for _, p := range body.Breakdown.Dashboard.Parts {
		sum += p.Pct
	}
	if math.Abs(sum-10) > 1e-9 {
		t.Fatalf("dashboard parts sum to %v, want the unit 10", sum)
	}

	want := []string{"switch_time", "runtime", "coupling", "flapping"}
	if len(part.Detectors) != len(want) {
		t.Fatalf("%d detectors, want %d: %+v", len(part.Detectors), len(want), part.Detectors)
	}
	var detectorSum float64
	for i, name := range want {
		if part.Detectors[i].ID != name {
			t.Fatalf("detectors[%d] = %q, want %q", i, part.Detectors[i].ID, name)
		}
		detectorSum += part.Detectors[i].Pct
	}
	if math.Abs(detectorSum-part.Pct) > 1e-9 {
		t.Fatalf("detectors sum to %v, the learning part is %v", detectorSum, part.Pct)
	}
	if want := 0.525 * 420.0 / 760.0; math.Abs(part.Detectors[0].Pct-want) > 1e-9 {
		t.Fatalf("switch_time pct = %v, want %v", part.Detectors[0].Pct, want)
	}
}

func TestPerformanceLearningDetectorsLimitedToEight(t *testing.T) {
	stats := autolearn.Stats{Rounds: 1, Share: 0.01, Took: map[string]time.Duration{}}
	for i := 0; i < 10; i++ {
		stats.Took[string(rune('a'+i))] = time.Duration(i+1) * time.Millisecond
	}
	router, manager := performanceRouterWithDeps(t, breakdownSnapshot(), func(deps *PerformanceDeps) {
		deps.Learning = &PerformanceLearning{
			Load:  func() autolearn.Stats { return stats },
			State: func() learnrec.State { return learnrec.State{} },
		}
	})
	var body learningPartBody
	if err := json.Unmarshal(getPerformance(t, router, manager), &body); err != nil {
		t.Fatal(err)
	}
	part := learningPart(body)
	if part == nil {
		t.Fatal("no learning part")
	}
	if len(part.Detectors) != performanceLearningPartLimit {
		t.Fatalf("%d detectors, want %d", len(part.Detectors), performanceLearningPartLimit)
	}
	for i := 1; i < len(part.Detectors); i++ {
		if part.Detectors[i].Pct > part.Detectors[i-1].Pct {
			t.Fatalf("detectors not descending: %+v", part.Detectors)
		}
	}
	if part.Detectors[0].ID != "j" {
		t.Fatalf("first detector = %q, want the costliest j", part.Detectors[0].ID)
	}
}

func TestPerformanceLearningPartAbsentWithoutDeps(t *testing.T) {
	router, manager := performanceRouterWithDeps(t, breakdownSnapshot(), nil)
	var body learningPartBody
	if err := json.Unmarshal(getPerformance(t, router, manager), &body); err != nil {
		t.Fatal(err)
	}
	if learningPart(body) != nil {
		t.Fatal("learning part present without the learning deps")
	}
	for _, unit := range body.Units {
		if unit.ID == "learning" {
			t.Fatal("learning unit present without the learning deps")
		}
	}
}

func TestPerformanceLearningShareAveragesRecordedDays(t *testing.T) {
	now := time.Now()
	stats := performanceLearningStats()
	shares := map[string]float64{}
	for i := 0; i < 7; i++ {
		shares[now.AddDate(0, 0, -i).Format("2006-01-02")] = 0.04
	}
	shares[now.AddDate(0, 0, -8).Format("2006-01-02")] = 0.5 // outside the window
	router, manager := performanceRouterWithDeps(t, performanceTestSnapshot(), func(deps *PerformanceDeps) {
		deps.Learning = &PerformanceLearning{
			Load:  func() autolearn.Stats { return stats },
			State: func() learnrec.State { return learnrec.State{DailyShare: shares} },
		}
	})
	var body learningBlockBody
	if err := json.Unmarshal(getPerformance(t, router, manager), &body); err != nil {
		t.Fatal(err)
	}
	if body.Learning == nil || math.Abs(body.Learning.Share-0.04) > 1e-9 {
		t.Fatalf("share = %+v, want the recorded mean 0.04 instead of the live 0.021", body.Learning)
	}
}

func TestCarveLearningClampsToTheUnit(t *testing.T) {
	parts := []performancePart{{ID: "push", Pct: 4}, {ID: "gc", Pct: 1}}
	learning := &performanceLearningPart{Pct: 9, Detectors: []performanceDetector{{ID: "runtime", Pct: 9}}}
	out := carveLearning(parts, learning)
	var sum float64
	for _, p := range out {
		sum += p.Pct
	}
	if math.Abs(sum-5) > 1e-9 {
		t.Fatalf("parts sum to %v, want the unit 5: %+v", sum, out)
	}
	last := out[len(out)-1]
	if last.ID != "learning" || math.Abs(last.Pct-5) > 1e-9 || len(out[0:2]) != 2 || out[0].Pct != 0 {
		t.Fatalf("learning clamped to the unit, other parts gone: %+v", out)
	}
	if math.Abs(last.Detectors[0].Pct-5) > 1e-9 {
		t.Fatalf("detector %+v, want 5", last.Detectors[0])
	}
}
