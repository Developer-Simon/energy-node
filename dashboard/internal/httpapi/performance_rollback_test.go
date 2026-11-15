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
	"github.com/Developer-Simon/energy-node-dashboard/internal/performance"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

// rollbackFixture wires POST /api/v1/performance/rollback to a monitor that
// holds change. The rates the page reports follow rates, so a test can move a
// row away from the value the change set, as a later write would.
type rollbackFixture struct {
	router  http.Handler
	manager *auth.Manager
	apply   applyFixture
	monitor *performance.Monitor
	rates   *performance.RateConfig
}

func newRollbackFixture(t *testing.T, change *performance.Change) rollbackFixture {
	t.Helper()
	apply := newApplyFixture(t)
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	monitor := performance.NewMonitor(nil, nil, performance.Options{})
	if err := monitor.AttachChange(t.TempDir(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if change != nil {
		if err := monitor.RecordChange(*change); err != nil {
			t.Fatal(err)
		}
	}
	rates := &performance.RateConfig{LiveUpdateS: 5, TickS: 10, RegulatorS: 10, NodeS: 10, Services: map[string]float64{"shelly": 20}}
	deps := RouterDependencies{
		Auth: manager,
		Performance: &PerformanceDeps{
			Provider:   stubPerformance{snap: performanceTestSnapshot()},
			Rates:      func() performance.RateConfig { return *rates },
			Floors:     map[string]float64{"shelly": 2},
			Auth:       manager,
			Settings:   apply.store,
			Configs:    apply.configs,
			SystemPath: apply.configPath,
			DataDir:    apply.writer.DataDir,
			NodeRates:  apply.rates,
			Changes:    monitor,
		},
	}
	router := NewAuthenticatedRouter(registry.New(), apply.configs, apply.store, nil, nil, nil, nil, nil, deps)
	return rollbackFixture{router: router, manager: manager, apply: apply, monitor: monitor, rates: rates}
}

// rollbackChange is an applied change that moved live_update 3 to 5 and node 15 to 10.
// The page reports both values, so the change is unchanged until a test edits them.
func rollbackChange() performance.Change {
	return performance.Change{
		At:        time.Now(),
		Before:    map[performance.RowID]float64{performance.RowLiveUpdate: 3, performance.RowNode: 15},
		After:     map[performance.RowID]float64{performance.RowLiveUpdate: 5, performance.RowNode: 10},
		Expected:  50,
		Available: 60,
		State:     "running",
	}
}

func postRollback(router http.Handler, cookie *http.Cookie, csrfToken, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/performance/rollback", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-CSRF-Token", csrfToken)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestRollbackWithoutChangeIs409(t *testing.T) {
	fx := newRollbackFixture(t, nil)
	cookie, csrfToken := loginAsAdmin(t, fx.router)

	recorder := postRollback(fx.router, cookie, csrfToken, `{}`)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "no_change_to_undo" {
		t.Fatalf("code %q, want no_change_to_undo", body.Code)
	}
}

func TestRollbackConflictReturns409(t *testing.T) {
	change := rollbackChange()
	fx := newRollbackFixture(t, &change)
	cookie, csrfToken := loginAsAdmin(t, fx.router)
	// The user moved live_update on from the 5 the change set.
	fx.rates.LiveUpdateS = 10
	settingsBefore, err := fx.apply.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}

	recorder := postRollback(fx.router, cookie, csrfToken, `{"only_unchanged": false}`)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Code      string             `json:"code"`
		Conflicts map[string]float64 `json:"conflicts"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "rollback_conflict" {
		t.Fatalf("code %q, want rollback_conflict", body.Code)
	}
	if len(body.Conflicts) != 1 || body.Conflicts["live_update"] != 10 {
		t.Fatalf("conflicts %v, want only live_update 10", body.Conflicts)
	}

	settingsAfter, err := fx.apply.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settingsAfter.LiveUpdateIntervalSeconds != settingsBefore.LiveUpdateIntervalSeconds {
		t.Fatal("live_update written despite a conflict")
	}
	if fx.apply.rates.calls != 0 {
		t.Fatal("node rate written despite a conflict")
	}
	if state := fx.monitor.CurrentChange().State; state != "running" {
		t.Fatalf("state %q after conflict, want running", state)
	}
}

func TestRollbackOnlyUnchanged(t *testing.T) {
	change := rollbackChange()
	fx := newRollbackFixture(t, &change)
	cookie, csrfToken := loginAsAdmin(t, fx.router)
	fx.rates.LiveUpdateS = 10
	settingsBefore, err := fx.apply.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}

	recorder := postRollback(fx.router, cookie, csrfToken, `{"only_unchanged": true}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Change        *performance.Change `json:"change"`
		LeftUnchanged []string            `json:"left_unchanged"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Change == nil || body.Change.State != "undone" {
		t.Fatalf("change = %+v, want undone: %s", body.Change, recorder.Body.String())
	}
	if len(body.LeftUnchanged) != 1 || body.LeftUnchanged[0] != "live_update" {
		t.Fatalf("left_unchanged %v, want [live_update]", body.LeftUnchanged)
	}

	// The node row was unchanged, so it goes back to its value before the change.
	if fx.apply.rates.calls != 1 || fx.apply.rates.poll != 15 {
		t.Fatalf("SetRates calls %d poll %v, want 1 and 15", fx.apply.rates.calls, fx.apply.rates.poll)
	}
	settingsAfter, err := fx.apply.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settingsAfter.LiveUpdateIntervalSeconds != settingsBefore.LiveUpdateIntervalSeconds {
		t.Fatal("conflicting live_update was rolled back")
	}
	if state := fx.monitor.CurrentChange().State; state != "undone" {
		t.Fatalf("state %q, want undone", state)
	}
}

func TestRollbackRestoresAllAndMarksUndone(t *testing.T) {
	change := rollbackChange()
	fx := newRollbackFixture(t, &change)
	cookie, csrfToken := loginAsAdmin(t, fx.router)

	recorder := postRollback(fx.router, cookie, csrfToken, `{}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"left_unchanged":[]`) {
		t.Fatalf("left_unchanged is not an empty list: %s", recorder.Body.String())
	}
	settingsAfter, err := fx.apply.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settingsAfter.LiveUpdateIntervalSeconds != 3 {
		t.Fatalf("live_update %d, want 3", settingsAfter.LiveUpdateIntervalSeconds)
	}
	if fx.apply.rates.calls != 1 || fx.apply.rates.poll != 15 {
		t.Fatalf("SetRates calls %d poll %v, want 1 and 15", fx.apply.rates.calls, fx.apply.rates.poll)
	}
	if state := fx.monitor.CurrentChange().State; state != "undone" {
		t.Fatalf("state %q, want undone", state)
	}

	// A second rollback has nothing left to undo.
	again := postRollback(fx.router, cookie, csrfToken, `{}`)
	if again.Code != http.StatusConflict {
		t.Fatalf("second rollback status %d, want 409: %s", again.Code, again.Body.String())
	}
}

func TestRollbackNeedsRightsPerRow(t *testing.T) {
	change := rollbackChange()
	fx := newRollbackFixture(t, &change)
	// edit_setup covers live_update but not the node row, which the change also touched.
	cookie, csrfToken := loginWithRoles(t, fx.router, fx.manager, "einrichter", []string{auth.RoleEditSetup})
	settingsBefore, err := fx.apply.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}

	recorder := postRollback(fx.router, cookie, csrfToken, `{}`)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "forbidden" {
		t.Fatalf("code %q, want forbidden", body.Code)
	}
	settingsAfter, err := fx.apply.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settingsAfter.LiveUpdateIntervalSeconds != settingsBefore.LiveUpdateIntervalSeconds {
		t.Fatal("live_update written although the rollback was refused")
	}
	if state := fx.monitor.CurrentChange().State; state != "running" {
		t.Fatalf("state %q after refusal, want running", state)
	}
}
