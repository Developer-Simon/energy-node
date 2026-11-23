package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// fakeLearning stands in for the learning services behind LearningDeps.
type fakeLearning struct {
	value    settings.Settings
	state    learnrec.State
	usage    autolearn.Usage
	minutes  learnrec.MinuteUsage
	driveOK  bool
	chainS   [2]int64
	chainL   [2]int64
	setup    bool
	learned  bool
	resetErr error
	resets   int
}

func (f *fakeLearning) deps() *LearningDeps {
	return &LearningDeps{
		Settings:         func() (settings.Settings, error) { return f.value, nil },
		State:            func() learnrec.State { return f.state },
		Usage:            func() autolearn.Usage { return f.usage },
		ChainSuggestInfo: func() (int64, int) { return f.chainS[0], int(f.chainS[1]) },
		ChainLearnInfo:   func() (int64, int) { return f.chainL[0], int(f.chainL[1]) },
		SetupDone:        func() bool { return f.setup },
		Minutes:          func() learnrec.MinuteUsage { return f.minutes },
		DriveConfigured:  func() bool { return f.driveOK },
		Reset: func() error {
			f.resets++
			if f.resetErr != nil {
				return f.resetErr
			}
			f.learned = false
			f.usage = autolearn.Usage{}
			return nil
		},
	}
}

func learningTestRouter(t *testing.T, deps *LearningDeps) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: manager, Learning: deps})
	return router, manager
}

// learningStatusWire mirrors the documented JSON of GET /api/v1/learning/status.
type learningStatusWire struct {
	Fresh        bool   `json:"fresh"`
	SetupDone    bool   `json:"setup_done"`
	Level        string `json:"level"`
	LevelSetting string `json:"level_setting"`
	Features     map[string]struct {
		On      bool  `json:"on"`
		Since   int64 `json:"since"`
		Devices int   `json:"devices"`
	} `json:"features"`
	Usage struct {
		Bytes         int64 `json:"bytes"`
		WrittenPerDay int64 `json:"written_per_day"`
		Since         int64 `json:"since"`
		Devices       int   `json:"devices"`
	} `json:"usage"`
	Acknowledged []string `json:"acknowledged"`
	New          []string `json:"new"`
}

func TestLearningStatusNeedsViewAutomations(t *testing.T) {
	fake := &fakeLearning{}
	router, manager := learningTestRouter(t, fake.deps())
	cookie, _ := loginWithRoles(t, router, manager, "viewer", []string{auth.RoleViewDevices})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, sessionRequest(t, "/api/v1/learning/status", cookie, true))
	if recorder.Code != http.StatusForbidden || !viewForbiddenCode(recorder) {
		t.Fatalf("status without view_automations: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestLearningStatusShape(t *testing.T) {
	fake := &fakeLearning{
		value: settings.Settings{
			LearningAcknowledged: []string{"chain_learning"},
			LearningLevel:        "eco",
		},
		state:  learnrec.State{FirstStart: 1794000000, Fresh: false},
		usage:  autolearn.Usage{Bytes: 412000, WrittenPerDay: 96000, Since: 1794000000, Devices: 14},
		chainS: [2]int64{1794000000, 9},
		chainL: [2]int64{1794000000, 4},
		setup:  true,
	}
	router, manager := learningTestRouter(t, fake.deps())
	cookie, _ := loginWithRoles(t, router, manager, "automata", []string{auth.RoleViewAutomations})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, sessionRequest(t, "/api/v1/learning/status", cookie, true))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status with view_automations: %d %s", recorder.Code, recorder.Body.String())
	}
	var got learningStatusWire
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Fresh || !got.SetupDone || got.Level != "eco" || got.LevelSetting != "eco" {
		t.Fatalf("header fields = %+v", got)
	}
	if on := got.Features["chain_suggestions"]; !on.On || on.Since != 1794000000 || on.Devices != 9 {
		t.Fatalf("chain_suggestions = %+v", on)
	}
	if on := got.Features["chain_learning"]; !on.On || on.Devices != 4 {
		t.Fatalf("chain_learning = %+v", on)
	}
	if on := got.Features["auto_patterns"]; !on.On || on.Since != 1794000000 || on.Devices != 14 {
		t.Fatalf("auto_patterns = %+v", on)
	}
	if got.Usage.Bytes != 412000 || got.Usage.WrittenPerDay != 96000 || got.Usage.Devices != 14 {
		t.Fatalf("usage = %+v", got.Usage)
	}
	if !reflect.DeepEqual(got.Acknowledged, []string{"chain_learning"}) {
		t.Fatalf("acknowledged = %v", got.Acknowledged)
	}
	if !reflect.DeepEqual(got.New, []string{"chain_suggestions", "auto_patterns"}) {
		t.Fatalf("new = %v", got.New)
	}
}

func TestLearningStatusNewIsEmptyWhenOffOrAcknowledged(t *testing.T) {
	all := settings.LearningFeatures()
	cases := map[string]settings.Settings{
		"off": {
			ChainSuggestionsDisabled:   true,
			ChainLearningDisabled:      true,
			AutomationPatternsDisabled: true,
		},
		"acknowledged": {LearningAcknowledged: all},
	}
	for name, value := range cases {
		got := learningStatus(value, &LearningDeps{}, 1)
		if len(got.New) != 0 {
			t.Errorf("%s: new = %v, want none", name, got.New)
		}
		if got.New == nil {
			t.Errorf("%s: new is nil, want an empty list", name)
		}
	}
}

func TestLearningStatusLocationSet(t *testing.T) {
	withLocation := []byte(`{"rules":[],"settings":{"latitude":52.5,"longitude":13.4}}`)
	withoutLocation := []byte(`{"rules":[],"settings":{"latitude":52.5}}`)
	cases := map[string]struct {
		read func() ([]byte, error)
		want bool
	}{
		"location saved":       {func() ([]byte, error) { return withLocation, nil }, true},
		"latitude only":        {func() ([]byte, error) { return withoutLocation, nil }, false},
		"document unreadable":  {func() ([]byte, error) { return nil, errors.New("missing") }, false},
		"no rule source given": {nil, false},
	}
	for name, tc := range cases {
		got := learningStatus(settings.Settings{}, &LearningDeps{Rules: tc.read}, 1)
		if got.LocationSet != tc.want {
			t.Errorf("%s: location_set = %v, want %v", name, got.LocationSet, tc.want)
		}
	}
}

func TestLearningStatusExposesLocationSet(t *testing.T) {
	fake := &fakeLearning{}
	deps := fake.deps()
	deps.Rules = func() ([]byte, error) {
		return []byte(`{"rules":[],"settings":{"latitude":52.5,"longitude":13.4}}`), nil
	}
	router, manager := learningTestRouter(t, deps)
	cookie, _ := loginWithRoles(t, router, manager, "automata", []string{auth.RoleViewAutomations})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, sessionRequest(t, "/api/v1/learning/status", cookie, true))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: %d %s", recorder.Code, recorder.Body.String())
	}
	var got struct {
		LocationSet *bool `json:"location_set"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.LocationSet == nil || !*got.LocationSet {
		t.Fatalf("location_set = %v, want true", got.LocationSet)
	}
}

func TestLearningStatusLevelDefaultsToAuto(t *testing.T) {
	got := learningStatus(settings.Settings{}, &LearningDeps{}, 4)
	if got.LevelSetting != "auto" || got.Level != "balanced" {
		t.Fatalf("auto on 4 cores: setting %q level %q, want auto balanced", got.LevelSetting, got.Level)
	}
	if got := learningStatus(settings.Settings{}, &LearningDeps{}, 1); got.Level != "eco" {
		t.Fatalf("auto on 1 core: level %q, want eco", got.Level)
	}
}

func TestLearningStatusShowsRememberedDowngrade(t *testing.T) {
	deps := &LearningDeps{State: func() learnrec.State { return learnrec.State{AutoLevel: learnrec.LevelEco} }}
	if got := learningStatus(settings.Settings{}, deps, 4); got.Level != "eco" {
		t.Fatalf("auto after downgrade on 4 cores: level %q, want eco", got.Level)
	}
	if got := learningStatus(settings.Settings{LearningLevel: "full"}, deps, 4); got.Level != "full" {
		t.Fatalf("explicit full after downgrade: level %q, want full", got.Level)
	}
}

func TestLearningResetNeedsEditSetup(t *testing.T) {
	fake := &fakeLearning{learned: true}
	router, manager := learningTestRouter(t, fake.deps())
	cookie, csrf := loginWithRoles(t, router, manager, "viewer", []string{auth.RoleViewAutomations})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/learning/reset", nil)
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"learning_forbidden"`) {
		t.Fatalf("reset without edit_setup: %d %s", recorder.Code, recorder.Body.String())
	}
	if fake.resets != 0 {
		t.Fatalf("reset ran %d times without the right", fake.resets)
	}
}

func TestLearningResetWithEditSetupClearsState(t *testing.T) {
	fake := &fakeLearning{learned: true, usage: autolearn.Usage{Bytes: 5}}
	router, manager := learningTestRouter(t, fake.deps())
	cookie, csrf := loginWithRoles(t, router, manager, "setup", []string{auth.RoleEditSetup})

	without := httptest.NewRequest(http.MethodPost, "/api/v1/learning/reset", nil)
	without.AddCookie(cookie)
	without.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, without)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"csrf_failed"`) {
		t.Fatalf("reset without CSRF: %d %s", recorder.Code, recorder.Body.String())
	}
	if fake.resets != 0 {
		t.Fatalf("reset ran without CSRF")
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/learning/reset", nil)
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != `{"ok":true}` {
		t.Fatalf("reset: %d %s", recorder.Code, recorder.Body.String())
	}
	if fake.learned || fake.usage.Bytes != 0 || fake.resets != 1 {
		t.Fatalf("after reset: learned=%v usage=%+v resets=%d", fake.learned, fake.usage, fake.resets)
	}
}

func TestLearningResetFailureReturnsCatalogKey(t *testing.T) {
	fake := &fakeLearning{resetErr: errors.New("disk full")}
	router, manager := learningTestRouter(t, fake.deps())
	cookie, csrf := loginWithRoles(t, router, manager, "setup", []string{auth.RoleEditSetup})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/learning/reset", nil)
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(body, `"code":"learning_reset_failed"`) || !strings.Contains(body, `"message_key":"error.learning_reset_failed"`) {
		t.Fatalf("failed reset: %d %s", recorder.Code, body)
	}
}

func TestLearningResetRejectsGet(t *testing.T) {
	recorder := httptest.NewRecorder()
	handleLearningReset(nil, (&fakeLearning{}).deps()).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/learning/reset", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET reset: %d, want 405", recorder.Code)
	}
}

func TestLearningWithoutDepsAnswers501(t *testing.T) {
	recorder := httptest.NewRecorder()
	handleLearningStatus(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/learning/status", nil))
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status without deps: %d, want 501", recorder.Code)
	}
}

func TestLearningStatusMinutesWithoutDriveIsRAM(t *testing.T) {
	got := learningStatus(settings.Settings{}, &LearningDeps{}, 1)
	if got.Minutes.Location != "ram" || !got.Minutes.AutoLocation || got.Minutes.DriveConfigured {
		t.Fatalf("minutes without drive = %+v, want ram, auto_location, no drive", got.Minutes)
	}
	if !got.Minutes.On || got.Minutes.Days != learnrec.MinuteDaysDefault {
		t.Fatalf("minutes on/days = %v/%d, want on and %d days", got.Minutes.On, got.Minutes.Days, learnrec.MinuteDaysDefault)
	}
	if slices.Contains(got.New, minutesDriveKey) {
		t.Fatalf("new = %v, want no %s without drive", got.New, minutesDriveKey)
	}
}

func TestLearningStatusMinutesDriveAutoIsNewUntilAcknowledged(t *testing.T) {
	deps := &LearningDeps{DriveConfigured: func() bool { return true }}
	got := learningStatus(settings.Settings{}, deps, 1)
	if got.Minutes.Location != "drive" || !got.Minutes.AutoLocation || !got.Minutes.DriveConfigured {
		t.Fatalf("minutes with drive and auto = %+v, want drive, auto_location", got.Minutes)
	}
	if !slices.Contains(got.New, minutesDriveKey) {
		t.Fatalf("new = %v, want %s", got.New, minutesDriveKey)
	}

	acknowledged := settings.Settings{LearningAcknowledged: []string{minutesDriveKey}}
	if got := learningStatus(acknowledged, deps, 1); slices.Contains(got.New, minutesDriveKey) {
		t.Fatalf("acknowledged: new = %v, want no %s", got.New, minutesDriveKey)
	}
}

func TestLearningStatusMinutesExplicitLocationGivesNoHint(t *testing.T) {
	deps := &LearningDeps{DriveConfigured: func() bool { return true }}
	for _, location := range []string{"ram", "system", "drive"} {
		got := learningStatus(settings.Settings{LearningMinutesLocation: location}, deps, 1)
		if got.Minutes.AutoLocation {
			t.Errorf("location %s: auto_location = true", location)
		}
		if slices.Contains(got.New, minutesDriveKey) {
			t.Errorf("location %s: new = %v, want no %s", location, got.New, minutesDriveKey)
		}
	}
}

func TestLearningStatusMinutesOffGivesNoHint(t *testing.T) {
	deps := &LearningDeps{DriveConfigured: func() bool { return true }}
	got := learningStatus(settings.Settings{LearningMinutesMode: "off"}, deps, 1)
	if got.Minutes.On {
		t.Fatal("mode off: on = true")
	}
	if slices.Contains(got.New, minutesDriveKey) {
		t.Fatalf("mode off: new = %v, want no %s", got.New, minutesDriveKey)
	}
}

func TestLearningStatusMinutesUsageFields(t *testing.T) {
	fake := &fakeLearning{
		driveOK: true,
		minutes: learnrec.MinuteUsage{Bytes: 4096, WrittenPerDay: 512, Since: 1794000000, Keys: 7, RAMDays: 3, DriveDown: true},
	}
	router, manager := learningTestRouter(t, fake.deps())
	cookie, _ := loginWithRoles(t, router, manager, "automata", []string{auth.RoleViewAutomations})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, sessionRequest(t, "/api/v1/learning/status", cookie, true))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: %d %s", recorder.Code, recorder.Body.String())
	}
	var got struct {
		Minutes map[string]any `json:"minutes"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"on": true, "location": "drive", "auto_location": true, "drive_configured": true,
		"drive_down": true, "days": float64(learnrec.MinuteDaysDefault), "ram_days": float64(3),
		"bytes": float64(4096), "written_per_day": float64(512), "since": float64(1794000000), "keys": float64(7),
		"paused": false, "companion": false, "reconcile": false, "ha_days": float64(0),
	}
	if !reflect.DeepEqual(got.Minutes, want) {
		t.Fatalf("minutes = %v\nwant %v", got.Minutes, want)
	}
}

func TestLearningMinutesSettingsNeedEditSetup(t *testing.T) {
	for _, key := range []string{"learning_minutes_mode", "learning_minutes_location", "learning_minutes_days"} {
		if got := settingsFieldRoles[key]; got != auth.RoleEditSetup {
			t.Errorf("%s: role %q, want %q", key, got, auth.RoleEditSetup)
		}
	}
}

func TestLearningStatusMinutesCompanionFields(t *testing.T) {
	deps := &LearningDeps{
		CompanionPaired: func() bool { return true },
		MinutesPaused:   func() bool { return true },
		HAMinuteDays:    func() int { return 3 },
	}
	got := learningStatus(settings.Settings{}, deps, 1)
	if !got.Minutes.Companion || !got.Minutes.Paused || !got.Minutes.Reconcile || got.Minutes.HADays != 3 {
		t.Fatalf("minutes with companion = %+v, want companion, paused, reconcile, ha_days 3", got.Minutes)
	}
	if got.Minutes.On != true || got.Minutes.Location != "ram" {
		t.Fatalf("auto without drive = %+v, want on, ram", got.Minutes)
	}

	off := learningStatus(settings.Settings{LearningHAReconcileDisabled: true}, deps, 1)
	if off.Minutes.Reconcile {
		t.Fatal("reconcile disabled: reconcile = true")
	}

	none := learningStatus(settings.Settings{}, &LearningDeps{}, 1)
	if none.Minutes.Companion || none.Minutes.Paused || none.Minutes.Reconcile || none.Minutes.HADays != 0 {
		t.Fatalf("without companion = %+v, want all companion fields zero", none.Minutes)
	}
}
