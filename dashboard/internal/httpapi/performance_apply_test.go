package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
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

type recordingReloader struct {
	calls []string
}

func (r *recordingReloader) ReloadService(deviceID string) error {
	r.calls = append(r.calls, deviceID)
	return nil
}

type recordingRates struct {
	calls      int
	poll, mult float64
}

func (r *recordingRates) SetRates(poll, mult float64) {
	r.calls++
	r.poll = poll
	r.mult = mult
}

type applyFixture struct {
	writer     *rateWriter
	store      *settings.Store
	configs    *config.Manager
	configPath string
	rulesPath  string
	reloader   *recordingReloader
	rates      *recordingRates
}

func newApplyFixture(t *testing.T) applyFixture {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.json")
	template, err := os.ReadFile(filepath.Join("..", "appconfig", "energy-node.config.json"))
	if err != nil {
		t.Fatalf("Vorlage lesen: %v", err)
	}
	if err := os.WriteFile(configPath, template, 0o664); err != nil {
		t.Fatal(err)
	}
	configsDir := t.TempDir()
	rulesPath := filepath.Join(configsDir, "automation_rules.json")
	rules := `{"version":1,"settings":{"tick_interval_s":10,"regulator_interval_s":10},"rules":[],"scenarios":[]}`
	if err := os.WriteFile(rulesPath, []byte(rules), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configsDir, "automation_rules.schema.json"), []byte(`{"type":"object"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := settings.NewStore(t.TempDir())
	if err := store.SaveSettings(settings.Default()); err != nil {
		t.Fatalf("Einstellungen anlegen: %v", err)
	}
	fixture := applyFixture{
		store:      store,
		configs:    config.NewManager(configsDir),
		configPath: configPath,
		rulesPath:  rulesPath,
		reloader:   &recordingReloader{},
		rates:      &recordingRates{},
	}
	fixture.writer = &rateWriter{
		Settings:   store,
		Configs:    fixture.configs,
		SystemPath: configPath,
		DataDir:    t.TempDir(),
		Reloader:   fixture.reloader,
		NodeRates:  fixture.rates,
	}
	return fixture
}

func applyTestRows() []performance.Row {
	return []performance.Row{
		{ID: performance.RowLiveUpdate, Current: 3, Floor: 1, Right: "edit_setup", Follows: "half"},
		{ID: performance.RowTick, Current: 10, Floor: 5, Right: "automations", Follows: "half"},
		{ID: performance.RowRegulator, Current: 10, Floor: 5, Right: "automations", Follows: "balance"},
		{ID: performance.RowNode, Current: 5, Floor: 5, Right: "system_actions"},
		{ID: performance.RowShelly, Current: 5, Floor: 2, Right: "system_actions"},
		{ID: performance.RowApsystems, Current: 15, Floor: 12, Right: "system_actions"},
	}
}

func allRights(string) bool { return true }

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestApplyWritesEachPathOnce(t *testing.T) {
	fx := newApplyFixture(t)
	req := ApplyRequest{Rates: map[performance.RowID]float64{
		performance.RowLiveUpdate: 5,
		performance.RowTick:       15,
		performance.RowShelly:     10,
	}}

	if _, err := fx.writer.Apply(context.Background(), req, applyTestRows(), allRights); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	current, err := fx.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if current.LiveUpdateIntervalSeconds != 5 {
		t.Fatalf("live_update_interval_seconds %d, erwartet 5", current.LiveUpdateIntervalSeconds)
	}
	rules := readJSONFile(t, fx.rulesPath)
	if got := rules["settings"].(map[string]any)["tick_interval_s"]; got != float64(15) {
		t.Fatalf("tick_interval_s %v, erwartet 15", got)
	}
	system := readJSONFile(t, fx.configPath)
	shelly := system["services"].(map[string]any)["shelly"].(map[string]any)
	if shelly["poll_interval_s"] != float64(10) {
		t.Fatalf("shelly poll_interval_s %v, erwartet 10", shelly["poll_interval_s"])
	}
	if len(fx.reloader.calls) == 0 || !applyContains(fx.reloader.calls, "shelly") {
		t.Fatalf("Reloader nicht für shelly aufgerufen: %v", fx.reloader.calls)
	}
}

func TestApplyRollsBackOnLaterFailure(t *testing.T) {
	fx := newApplyFixture(t)
	readOnlyConfigDir(t, fx.configPath)
	settingsBefore, err := fx.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	rulesBefore, err := os.ReadFile(fx.rulesPath)
	if err != nil {
		t.Fatal(err)
	}

	req := ApplyRequest{Rates: map[performance.RowID]float64{
		performance.RowLiveUpdate: 5,
		performance.RowTick:       15,
		performance.RowShelly:     10,
	}}
	_, err = fx.writer.Apply(context.Background(), req, applyTestRows(), allRights)
	if err == nil {
		t.Fatal("Apply ohne Fehler, obwohl config.json nicht schreibbar ist")
	}

	settingsAfter, err := fx.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settingsAfter.LiveUpdateIntervalSeconds != settingsBefore.LiveUpdateIntervalSeconds {
		t.Fatalf("settings nicht zurückgesetzt: %d, erwartet %d", settingsAfter.LiveUpdateIntervalSeconds, settingsBefore.LiveUpdateIntervalSeconds)
	}
	rulesAfter, err := os.ReadFile(fx.rulesPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(rulesAfter) != string(rulesBefore) {
		t.Fatalf("automation_rules nicht zurückgesetzt:\n%s", rulesAfter)
	}
}

func TestApplyRejectsBelowFloor(t *testing.T) {
	fx := newApplyFixture(t)
	configBefore, err := os.ReadFile(fx.configPath)
	if err != nil {
		t.Fatal(err)
	}
	req := ApplyRequest{Rates: map[performance.RowID]float64{performance.RowApsystems: 5}}

	_, err = fx.writer.Apply(context.Background(), req, applyTestRows(), allRights)

	assertSaveError(t, err, http.StatusBadRequest, "below_floor")
	configAfter, err := os.ReadFile(fx.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(configAfter) != string(configBefore) {
		t.Fatal("config.json wurde trotz below_floor geschrieben")
	}
}

func TestApplyChecksRightsPerRow(t *testing.T) {
	fx := newApplyFixture(t)
	configBefore, err := os.ReadFile(fx.configPath)
	if err != nil {
		t.Fatal(err)
	}
	settingsBefore, err := fx.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	has := func(right string) bool { return right != "system_actions" }
	req := ApplyRequest{Rates: map[performance.RowID]float64{
		performance.RowLiveUpdate: 5,
		performance.RowShelly:     10,
	}}

	_, err = fx.writer.Apply(context.Background(), req, applyTestRows(), has)

	assertSaveError(t, err, http.StatusForbidden, "forbidden")
	settingsAfter, err := fx.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settingsAfter.LiveUpdateIntervalSeconds != settingsBefore.LiveUpdateIntervalSeconds {
		t.Fatal("settings wurden trotz fehlendem Recht geschrieben")
	}
	configAfter, err := os.ReadFile(fx.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(configAfter) != string(configBefore) {
		t.Fatal("config.json wurde trotz fehlendem Recht geschrieben")
	}
}

func TestApplyTargetOnlyHasNoChange(t *testing.T) {
	fx := newApplyFixture(t)
	target := 40
	resp, err := fx.writer.Apply(context.Background(), ApplyRequest{TargetPct: &target}, applyTestRows(), allRights)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if resp.Change != nil {
		t.Fatalf("Change %v, erwartet nil", resp.Change)
	}
	current, err := fx.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if current.PerformanceTargetPct != 40 {
		t.Fatalf("performance_target_pct %d, erwartet 40", current.PerformanceTargetPct)
	}
	if fx.rates.calls != 0 {
		t.Fatal("NodeRates bei reiner Zielprozent aufgerufen")
	}
}

func TestApplyNodePollCallsSetRates(t *testing.T) {
	fx := newApplyFixture(t)
	req := ApplyRequest{Rates: map[performance.RowID]float64{performance.RowNode: 10}}

	if _, err := fx.writer.Apply(context.Background(), req, applyTestRows(), allRights); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if fx.rates.calls != 1 {
		t.Fatalf("SetRates %d-mal aufgerufen, erwartet 1", fx.rates.calls)
	}
	if fx.rates.poll != 10 || fx.rates.mult != 10 {
		t.Fatalf("SetRates(%v, %v), erwartet (10, 10)", fx.rates.poll, fx.rates.mult)
	}
	system := readJSONFile(t, fx.configPath)
	dashboard := system["dashboard"].(map[string]any)
	if dashboard["node_poll_interval_s"] != float64(10) {
		t.Fatalf("node_poll_interval_s %v, erwartet 10", dashboard["node_poll_interval_s"])
	}
}

func assertSaveError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var saveErr *saveError
	if !errors.As(err, &saveErr) {
		t.Fatalf("Fehler ist kein *saveError: %v", err)
	}
	if saveErr.Status != status || saveErr.Code != code {
		t.Fatalf("Status %d Code %q, erwartet %d %q", saveErr.Status, saveErr.Code, status, code)
	}
}

func applyContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// applyEndpointRouter wires POST /api/v1/performance/apply to the fixture's
// stores and files, behind the authenticated router.
func applyEndpointRouter(t *testing.T) (http.Handler, *auth.Manager) {
	t.Helper()
	fixture := newApplyFixture(t)
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
			Floors:     map[string]float64{"shelly": 2},
			Auth:       manager,
			Settings:   fixture.store,
			Configs:    fixture.configs,
			SystemPath: fixture.configPath,
			DataDir:    fixture.writer.DataDir,
			NodeRates:  fixture.rates,
		},
	}
	router := NewAuthenticatedRouter(registry.New(), fixture.configs, fixture.store, nil, nil, nil, nil, nil, deps)
	return router, manager
}

func postApply(router http.Handler, cookie *http.Cookie, csrfToken, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/performance/apply", strings.NewReader(body))
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

func TestApplyEndpointRequiresCSRF(t *testing.T) {
	router, _ := applyEndpointRouter(t)
	cookie, _ := loginAsAdmin(t, router)

	recorder := postApply(router, cookie, "", `{"rates":{"live_update":5}}`)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("POST ohne CSRF status %d, erwartet 403: %s", recorder.Code, recorder.Body.String())
	}
}

func TestApplyEndpointReturnsExpected(t *testing.T) {
	router, _ := applyEndpointRouter(t)
	cookie, csrfToken := loginAsAdmin(t, router)

	recorder := postApply(router, cookie, csrfToken, `{"rates":{"live_update":5}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["expected_pct"].(float64); !ok {
		t.Fatalf("expected_pct fehlt oder ist keine Zahl: %s", recorder.Body.String())
	}
	// live_update wechselt von 3 auf 5, die Änderung wird also festgehalten.
	change, ok := body["change"].(map[string]any)
	if !ok || change["state"] != "running" {
		t.Fatalf("change = %v (vorhanden %v), erwartet laufende Änderung: %s", body["change"], ok, recorder.Body.String())
	}
}

func TestApplyEndpointChecksRowRights(t *testing.T) {
	router, manager := applyEndpointRouter(t)
	cookie, csrfToken := loginWithRoles(t, router, manager, "einrichter", []string{auth.RoleEditSetup})

	// edit_setup darf die Live-Aktualisierung, aber nicht die Dienstperiode von shelly.
	denied := postApply(router, cookie, csrfToken, `{"rates":{"shelly":30}}`)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("shelly mit edit_setup status %d, erwartet 403: %s", denied.Code, denied.Body.String())
	}
	allowed := postApply(router, cookie, csrfToken, `{"rates":{"live_update":5}}`)
	if allowed.Code != http.StatusOK {
		t.Fatalf("live_update mit edit_setup status %d: %s", allowed.Code, allowed.Body.String())
	}
}

func TestApplyTargetNeedsEditSetup(t *testing.T) {
	fx := newApplyFixture(t)
	settingsBefore, err := fx.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	target := 40
	has := func(right string) bool { return right != "edit_setup" }

	_, err = fx.writer.Apply(context.Background(), ApplyRequest{TargetPct: &target}, applyTestRows(), has)

	assertSaveError(t, err, http.StatusForbidden, "forbidden")
	settingsAfter, err := fx.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settingsAfter.PerformanceTargetPct != settingsBefore.PerformanceTargetPct {
		t.Fatalf("performance_target_pct %d geschrieben, obwohl edit_setup fehlt", settingsAfter.PerformanceTargetPct)
	}
}

func TestApplyRejectsTargetOutOfRange(t *testing.T) {
	for _, target := range []int{19, 96} {
		fx := newApplyFixture(t)
		settingsBefore, err := fx.store.LoadSettings()
		if err != nil {
			t.Fatal(err)
		}
		value := target

		_, err = fx.writer.Apply(context.Background(), ApplyRequest{TargetPct: &value}, applyTestRows(), allRights)

		assertSaveError(t, err, http.StatusBadRequest, "invalid_target")
		settingsAfter, err := fx.store.LoadSettings()
		if err != nil {
			t.Fatal(err)
		}
		if settingsAfter.PerformanceTargetPct != settingsBefore.PerformanceTargetPct {
			t.Fatalf("Ziel %d: performance_target_pct geschrieben", target)
		}
	}
}

func TestApplyRejectsNonPositive(t *testing.T) {
	fx := newApplyFixture(t)
	configBefore, err := os.ReadFile(fx.configPath)
	if err != nil {
		t.Fatal(err)
	}
	settingsBefore, err := fx.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	req := ApplyRequest{Rates: map[performance.RowID]float64{
		performance.RowLiveUpdate: -5,
		performance.RowShelly:     -3,
	}}

	_, err = fx.writer.Apply(context.Background(), req, applyTestRows(), allRights)

	assertSaveError(t, err, http.StatusBadRequest, "below_floor")
	settingsAfter, err := fx.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settingsAfter.LiveUpdateIntervalSeconds != settingsBefore.LiveUpdateIntervalSeconds {
		t.Fatal("settings geschrieben trotz nicht positiver Periode")
	}
	configAfter, err := os.ReadFile(fx.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(configAfter) != string(configBefore) {
		t.Fatal("config.json geschrieben trotz nicht positiver Periode")
	}
}

func TestApplyRoundsLiveUpdate(t *testing.T) {
	fx := newApplyFixture(t)
	req := ApplyRequest{Rates: map[performance.RowID]float64{performance.RowLiveUpdate: 4.6}}

	if _, err := fx.writer.Apply(context.Background(), req, applyTestRows(), allRights); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	current, err := fx.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if current.LiveUpdateIntervalSeconds != 5 {
		t.Fatalf("live_update_interval_seconds %d, erwartet 5 (gerundet)", current.LiveUpdateIntervalSeconds)
	}
}

// applyEndpointRouterWith is applyEndpointRouter with a change store attached.
func applyEndpointRouterWith(t *testing.T, changes ChangeStore) (http.Handler, *auth.Manager) {
	t.Helper()
	fixture := newApplyFixture(t)
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
			Floors:     map[string]float64{"shelly": 2},
			Auth:       manager,
			Settings:   fixture.store,
			Configs:    fixture.configs,
			SystemPath: fixture.configPath,
			DataDir:    fixture.writer.DataDir,
			NodeRates:  fixture.rates,
			Changes:    changes,
		},
	}
	router := NewAuthenticatedRouter(registry.New(), fixture.configs, fixture.store, nil, nil, nil, nil, nil, deps)
	return router, manager
}

func newChangeMonitor(t *testing.T) (*performance.Monitor, string) {
	t.Helper()
	dir := t.TempDir()
	monitor := performance.NewMonitor(nil, nil, performance.Options{})
	if err := monitor.AttachChange(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	return monitor, dir
}

func TestApplyWritesChangeFile(t *testing.T) {
	monitor, dir := newChangeMonitor(t)
	router, _ := applyEndpointRouterWith(t, monitor)
	cookie, csrfToken := loginAsAdmin(t, router)

	recorder := postApply(router, cookie, csrfToken, `{"rates":{"live_update":5}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		ExpectedPct float64             `json:"expected_pct"`
		Change      *performance.Change `json:"change"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Change == nil || body.Change.State != "running" {
		t.Fatalf("change = %+v, want running: %s", body.Change, recorder.Body.String())
	}
	if body.Change.Expected != body.ExpectedPct {
		t.Fatalf("change.expected_pct %v, want %v", body.Change.Expected, body.ExpectedPct)
	}
	wantAvailable := float64(performance.DefaultTarget(4)) - performance.DeductionFor(performanceTestSnapshot()).Pct
	if body.Change.Available != wantAvailable {
		t.Fatalf("change.available_pct %v, want %v", body.Change.Available, wantAvailable)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "performance", "last-change.json"))
	if err != nil {
		t.Fatalf("change file: %v", err)
	}
	var stored performance.Change
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.State != "running" {
		t.Fatalf("stored state %q, want running", stored.State)
	}
	if len(stored.Before) != 1 || stored.Before[performance.RowLiveUpdate] != 3 {
		t.Fatalf("stored before = %v, want only live_update 3", stored.Before)
	}
	if len(stored.After) != 1 || stored.After[performance.RowLiveUpdate] != 5 {
		t.Fatalf("stored after = %v, want only live_update 5", stored.After)
	}
}

func TestApplyUnchangedRecordsNothing(t *testing.T) {
	monitor, dir := newChangeMonitor(t)
	router, _ := applyEndpointRouterWith(t, monitor)
	cookie, csrfToken := loginAsAdmin(t, router)

	recorder := postApply(router, cookie, csrfToken, `{"rates":{"live_update":3}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if change, ok := body["change"]; !ok || change != nil {
		t.Fatalf("change = %v (vorhanden %v), erwartet null: %s", change, ok, recorder.Body.String())
	}
	if monitor.CurrentChange() != nil {
		t.Fatal("unchanged apply recorded a change")
	}
	if _, err := os.Stat(filepath.Join(dir, "performance", "last-change.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("change file exists after unchanged apply: %v", err)
	}
}
