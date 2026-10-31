package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/companion"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/onboarding"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// onboardingView is the GET /api/v1/onboarding response.
type onboardingView struct {
	Role  string `json:"role"`
	Setup struct {
		Done  bool              `json:"done"`
		Steps map[string]string `json:"steps"`
		Items []struct {
			ID        string `json:"id"`
			Area      string `json:"area"`
			Installer bool   `json:"installer"`
			State     string `json:"state"`
		} `json:"items"`
	} `json:"setup"`
	Queue []string `json:"queue"`
	User  struct {
		Seen     []string `json:"seen"`
		Welcomed bool     `json:"welcomed"`
		Run      *struct {
			Mode  string `json:"mode"`
			From  string `json:"from"`
			Index int    `json:"index"`
		} `json:"run"`
		Queue []string `json:"queue"`
	} `json:"user"`
}

// newOnboardingTestRouter builds a router whose users live in usersPath and
// whose onboarding progress lives in onboardingDir.
func newOnboardingTestRouter(t *testing.T, usersPath, onboardingDir string) (http.Handler, *auth.Manager) {
	t.Helper()
	return newOnboardingRouterWithSettings(t, usersPath, onboardingDir, t.TempDir())
}

// newOnboardingRouterWithSettings is newOnboardingTestRouter with the settings
// of the installation in settingsDir, so a test can prepare them first.
func newOnboardingRouterWithSettings(t *testing.T, usersPath, onboardingDir, settingsDir string) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.NewManager(usersPath, "admin", usersAdminPassword)
	if err != nil {
		t.Fatal(err)
	}
	hub := companion.NewHub()
	manager.SetHAVerifier(hub)
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(settingsDir), nil, nil, nil, nil, nil, RouterDependencies{
		Auth:       manager,
		Onboarding: onboarding.NewStore(onboardingDir),
	})
	return router, manager
}

// itemState returns the state of one setup item, or "" when it is missing or open.
func itemState(view onboardingView, id string) string {
	for _, item := range view.Setup.Items {
		if item.ID == id {
			return item.State
		}
	}
	return ""
}

func newTestOnboardingRouter(t *testing.T) (http.Handler, *auth.Manager) {
	t.Helper()
	return newOnboardingTestRouter(t, filepath.Join(t.TempDir(), "users.json"), t.TempDir())
}

// guestSession starts a guest session over HTTPS and returns its cookie and
// CSRF token.
func guestSession(t *testing.T, router http.Handler) (*http.Cookie, string) {
	t.Helper()
	recorder := usersCall(router, http.MethodPost, "/api/v1/auth/guest", "", nil, "", true)
	wantUsersStatus(t, recorder, http.StatusOK)
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	return recorder.Result().Cookies()[0], session.CSRFToken
}

func getOnboarding(t *testing.T, router http.Handler, cookie *http.Cookie) onboardingView {
	t.Helper()
	recorder := usersCall(router, http.MethodGet, "/api/v1/onboarding", "", cookie, "", true)
	wantUsersStatus(t, recorder, http.StatusOK)
	var view onboardingView
	if err := json.Unmarshal(recorder.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	return view
}

func TestOnboardingGetRoles(t *testing.T) {
	router, manager := newTestOnboardingRouter(t)
	adminCookie, _ := sessionFor(t, router, "admin", usersAdminPassword)
	readerCookie, _ := loginWithRoles(t, router, manager, "reader", []string{auth.RoleViewLogs})
	guestCookie, _ := guestSession(t, router)

	admin := getOnboarding(t, router, adminCookie)
	if admin.Role != "admin" {
		t.Fatalf("admin role = %q", admin.Role)
	}
	if admin.Setup.Done || admin.Setup.Steps == nil {
		t.Fatalf("fresh setup = %+v", admin.Setup)
	}
	if admin.User.Seen == nil || admin.User.Queue == nil {
		t.Fatalf("seen and queue must be empty lists, not null: %+v", admin.User)
	}
	if admin.User.Run != nil || admin.User.Welcomed {
		t.Fatalf("fresh user = %+v", admin.User)
	}

	if role := getOnboarding(t, router, readerCookie).Role; role != "user" {
		t.Fatalf("reader role = %q, want user", role)
	}
	if role := getOnboarding(t, router, guestCookie).Role; role != "guest" {
		t.Fatalf("guest role = %q, want guest", role)
	}
}

func TestStepRequiresAdmin(t *testing.T) {
	router, manager := newTestOnboardingRouter(t)
	adminCookie, adminCSRF := sessionFor(t, router, "admin", usersAdminPassword)
	readerCookie, readerCSRF := loginWithRoles(t, router, manager, "reader", []string{auth.RoleViewLogs})
	guestCookie, guestCSRF := guestSession(t, router)
	body := `{"id":"theme","state":"done"}`

	wantUsersError(t, usersCall(router, http.MethodPut, "/api/v1/onboarding/step", body, guestCookie, guestCSRF, true), http.StatusForbidden, "onboarding_forbidden")
	wantUsersError(t, usersCall(router, http.MethodPut, "/api/v1/onboarding/step", body, readerCookie, readerCSRF, true), http.StatusForbidden, "onboarding_forbidden")
	wantUsersStatus(t, usersCall(router, http.MethodPut, "/api/v1/onboarding/step", body, adminCookie, adminCSRF, true), http.StatusOK)
}

func TestStepRejectsUnknownState(t *testing.T) {
	router, _ := newTestOnboardingRouter(t)
	cookie, csrf := sessionFor(t, router, "admin", usersAdminPassword)
	wantUsersError(t, usersCall(router, http.MethodPut, "/api/v1/onboarding/step", `{"id":"theme","state":"maybe"}`, cookie, csrf, true), http.StatusBadRequest, "onboarding_invalid")
	wantUsersError(t, usersCall(router, http.MethodPut, "/api/v1/onboarding/step", `{"state":"done"}`, cookie, csrf, true), http.StatusBadRequest, "onboarding_invalid")
}

func TestSetupCompleteIsGlobal(t *testing.T) {
	router, manager := newTestOnboardingRouter(t)
	firstCookie, firstCSRF := sessionFor(t, router, "admin", usersAdminPassword)
	wantUsersStatus(t, usersCall(router, http.MethodPost, "/api/v1/onboarding/setup/complete", "", firstCookie, firstCSRF, true), http.StatusOK)

	secondCookie, _ := loginWithRoles(t, router, manager, "second", []string{auth.RoleUserManagement})
	view := getOnboarding(t, router, secondCookie)
	if !view.Setup.Done {
		t.Fatal("second admin sees setup.done = false after the first admin completed it")
	}
	if view.Role != "admin" {
		t.Fatalf("second role = %q, want admin", view.Role)
	}
}

func TestMeOnlyTouchesOwnAccount(t *testing.T) {
	router, manager := newTestOnboardingRouter(t)
	adminCookie, adminCSRF := sessionFor(t, router, "admin", usersAdminPassword)
	otherCookie, _ := loginWithRoles(t, router, manager, "other", []string{auth.RoleViewLogs})

	body := `{"seen_add":["intro","intro"],"welcomed":true,"queue":["theme"],"run":{"mode":"full","from":"start","index":2}}`
	wantUsersStatus(t, usersCall(router, http.MethodPut, "/api/v1/onboarding/me", body, adminCookie, adminCSRF, true), http.StatusOK)

	admin := getOnboarding(t, router, adminCookie)
	if !admin.User.Welcomed || len(admin.User.Seen) != 1 || admin.User.Seen[0] != "intro" {
		t.Fatalf("admin user = %+v, want welcomed with one seen intro", admin.User)
	}
	if len(admin.User.Queue) != 1 || admin.User.Queue[0] != "theme" {
		t.Fatalf("admin queue = %v", admin.User.Queue)
	}
	if admin.User.Run == nil || admin.User.Run.Mode != "full" || admin.User.Run.Index != 2 {
		t.Fatalf("admin run = %+v", admin.User.Run)
	}

	other := getOnboarding(t, router, otherCookie)
	if other.User.Welcomed || len(other.User.Seen) != 0 || len(other.User.Queue) != 0 || other.User.Run != nil {
		t.Fatalf("other user changed by the admin: %+v", other.User)
	}

	// null ends the run, the other fields stay as they are
	wantUsersStatus(t, usersCall(router, http.MethodPut, "/api/v1/onboarding/me", `{"run":null}`, adminCookie, adminCSRF, true), http.StatusOK)
	admin = getOnboarding(t, router, adminCookie)
	if admin.User.Run != nil || !admin.User.Welcomed || len(admin.User.Seen) != 1 {
		t.Fatalf("after run null: %+v", admin.User)
	}
}

func TestRunSurvivesRestart(t *testing.T) {
	usersPath := filepath.Join(t.TempDir(), "users.json")
	stateDir := t.TempDir()
	router, _ := newOnboardingTestRouter(t, usersPath, stateDir)
	cookie, csrf := sessionFor(t, router, "admin", usersAdminPassword)
	wantUsersStatus(t, usersCall(router, http.MethodPut, "/api/v1/onboarding/me", `{"run":{"mode":"full","from":"start","index":3}}`, cookie, csrf, true), http.StatusOK)

	restarted, _ := newOnboardingTestRouter(t, usersPath, stateDir)
	restartedCookie, _ := sessionFor(t, restarted, "admin", usersAdminPassword)
	view := getOnboarding(t, restarted, restartedCookie)
	if view.User.Run == nil || view.User.Run.Index != 3 || view.User.Run.From != "start" {
		t.Fatalf("run after restart = %+v", view.User.Run)
	}
}

func TestStateChangeNeedsCSRF(t *testing.T) {
	router, _ := newTestOnboardingRouter(t)
	cookie, _ := sessionFor(t, router, "admin", usersAdminPassword)
	wantUsersError(t, usersCall(router, http.MethodPut, "/api/v1/onboarding/step", `{"id":"theme","state":"done"}`, cookie, "", true), http.StatusForbidden, "csrf_failed")
	wantUsersError(t, usersCall(router, http.MethodPut, "/api/v1/onboarding/me", `{"welcomed":true}`, cookie, "wrong", true), http.StatusForbidden, "csrf_failed")
	wantUsersError(t, usersCall(router, http.MethodPost, "/api/v1/onboarding/setup/complete", "", cookie, "", true), http.StatusForbidden, "csrf_failed")
}

func TestUnauthenticated401(t *testing.T) {
	router, _ := newTestOnboardingRouter(t)
	wantUsersError(t, usersCall(router, http.MethodGet, "/api/v1/onboarding", "", nil, "", true), http.StatusUnauthorized, "authentication_required")
	wantUsersError(t, usersCall(router, http.MethodPut, "/api/v1/onboarding/step", `{"id":"theme","state":"done"}`, nil, "", true), http.StatusUnauthorized, "authentication_required")
}

// onboardingLimitCase is one request against the input limits. fill prepares
// the stored progress before the request.
type onboardingLimitCase struct {
	name   string
	method string
	path   string
	body   map[string]any
	fill   func(t *testing.T, store *onboarding.Store)
}

func fillSetupSteps(t *testing.T, store *onboarding.Store, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		if err := store.SetStep(fmt.Sprintf("step%03d", i), onboarding.StepDone); err != nil {
			t.Fatal(err)
		}
	}
}

func fillSeen(t *testing.T, store *onboarding.Store, count int) {
	t.Helper()
	seen := make([]string, count)
	for i := range seen {
		seen[i] = fmt.Sprintf("seen%03d", i)
	}
	if err := store.UpdateUser("admin", func(own *onboarding.UserProgress) { own.Seen = seen }); err != nil {
		t.Fatal(err)
	}
}

func repeatedIDs(prefix string, count int) []string {
	ids := make([]string, count)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s%03d", prefix, i)
	}
	return ids
}

func runOnboardingLimitCase(t *testing.T, tc onboardingLimitCase, wantStatus int, wantCode string) {
	t.Helper()
	dir := t.TempDir()
	router, _ := newOnboardingTestRouter(t, filepath.Join(t.TempDir(), "users.json"), dir)
	store := onboarding.NewStore(dir)
	if err := store.SetStep("baseline", onboarding.StepDone); err != nil {
		t.Fatal(err)
	}
	if tc.fill != nil {
		tc.fill(t, store)
	}
	cookie, csrf := sessionFor(t, router, "admin", usersAdminPassword)
	stored := filepath.Join(dir, "onboarding.json")
	before, err := os.ReadFile(stored)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(tc.body)
	if err != nil {
		t.Fatal(err)
	}
	recorder := usersCall(router, tc.method, tc.path, string(body), cookie, csrf, true)
	if wantStatus == http.StatusOK {
		wantUsersStatus(t, recorder, http.StatusOK)
		return
	}
	wantUsersError(t, recorder, wantStatus, wantCode)
	after, err := os.ReadFile(stored)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("stored progress changed by a rejected request:\nbefore %s\nafter  %s", before, after)
	}
}

const (
	limitStepPath = "/api/v1/onboarding/step"
	limitMePath   = "/api/v1/onboarding/me"
)

func TestOnboardingInputLimitsRejected(t *testing.T) {
	cases := []onboardingLimitCase{
		{name: "step id empty", method: http.MethodPut, path: limitStepPath, body: map[string]any{"id": "", "state": "done"}},
		{name: "step id 65 bytes", method: http.MethodPut, path: limitStepPath, body: map[string]any{"id": strings.Repeat("a", 65), "state": "done"}},
		{name: "new step id with 200 steps stored", method: http.MethodPut, path: limitStepPath, body: map[string]any{"id": "theme", "state": "done"}, fill: func(t *testing.T, store *onboarding.Store) {
			fillSetupSteps(t, store, 199)
		}},
		{name: "seen_add entry empty", method: http.MethodPut, path: limitMePath, body: map[string]any{"seen_add": []string{""}}},
		{name: "seen_add entry 65 bytes", method: http.MethodPut, path: limitMePath, body: map[string]any{"seen_add": []string{strings.Repeat("a", 65)}}},
		{name: "seen_add 101 entries", method: http.MethodPut, path: limitMePath, body: map[string]any{"seen_add": repeatedIDs("i", 101)}},
		{name: "seen would reach 501", method: http.MethodPut, path: limitMePath, body: map[string]any{"seen_add": []string{"new"}}, fill: func(t *testing.T, store *onboarding.Store) {
			fillSeen(t, store, 500)
		}},
		{name: "queue 6 entries", method: http.MethodPut, path: limitMePath, body: map[string]any{"queue": repeatedIDs("q", 6)}},
		{name: "queue entry empty", method: http.MethodPut, path: limitMePath, body: map[string]any{"queue": []string{""}}},
		{name: "queue entry 65 bytes", method: http.MethodPut, path: limitMePath, body: map[string]any{"queue": []string{strings.Repeat("q", 65)}}},
		{name: "run mode 33 bytes", method: http.MethodPut, path: limitMePath, body: map[string]any{"run": map[string]any{"mode": strings.Repeat("m", 33), "from": "start", "index": 0}}},
		{name: "run index -1", method: http.MethodPut, path: limitMePath, body: map[string]any{"run": map[string]any{"mode": "full", "from": "start", "index": -1}}},
		{name: "run index 1001", method: http.MethodPut, path: limitMePath, body: map[string]any{"run": map[string]any{"mode": "full", "from": "start", "index": 1001}}},
		{name: "run from 65 bytes", method: http.MethodPut, path: limitMePath, body: map[string]any{"run": map[string]any{"mode": "full", "from": strings.Repeat("f", 65), "index": 0}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runOnboardingLimitCase(t, tc, http.StatusBadRequest, "onboarding_invalid")
		})
	}
}

func TestOnboardingInputLimitsAtBoundary(t *testing.T) {
	cases := []onboardingLimitCase{
		{name: "step id 64 bytes", method: http.MethodPut, path: limitStepPath, body: map[string]any{"id": strings.Repeat("a", 64), "state": "later"}},
		{name: "step id known with 200 steps stored", method: http.MethodPut, path: limitStepPath, body: map[string]any{"id": "step000", "state": "later"}, fill: func(t *testing.T, store *onboarding.Store) {
			fillSetupSteps(t, store, 199)
		}},
		{name: "200th step id", method: http.MethodPut, path: limitStepPath, body: map[string]any{"id": "theme", "state": "done"}, fill: func(t *testing.T, store *onboarding.Store) {
			fillSetupSteps(t, store, 198)
		}},
		{name: "seen_add 100 entries", method: http.MethodPut, path: limitMePath, body: map[string]any{"seen_add": repeatedIDs("i", 100)}},
		{name: "seen reaches exactly 500", method: http.MethodPut, path: limitMePath, body: map[string]any{"seen_add": []string{"new"}}, fill: func(t *testing.T, store *onboarding.Store) {
			fillSeen(t, store, 499)
		}},
		{name: "seen_add 64 byte id", method: http.MethodPut, path: limitMePath, body: map[string]any{"seen_add": []string{strings.Repeat("s", 64)}}},
		{name: "queue 5 entries", method: http.MethodPut, path: limitMePath, body: map[string]any{"queue": repeatedIDs("q", 5)}},
		{name: "queue entry 64 bytes", method: http.MethodPut, path: limitMePath, body: map[string]any{"queue": []string{strings.Repeat("q", 64)}}},
		{name: "run at the limits", method: http.MethodPut, path: limitMePath, body: map[string]any{"run": map[string]any{"mode": strings.Repeat("m", 32), "from": strings.Repeat("f", 64), "index": 1000}}},
		{name: "run index 0", method: http.MethodPut, path: limitMePath, body: map[string]any{"run": map[string]any{"mode": "full", "from": "start", "index": 0}}},
		{name: "run without a source", method: http.MethodPut, path: limitMePath, body: map[string]any{"run": map[string]any{"mode": "full", "from": "", "index": 0}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runOnboardingLimitCase(t, tc, http.StatusOK, "")
		})
	}
}

func TestQueueAdminGetsSetupFirst(t *testing.T) {
	router, _ := newTestOnboardingRouter(t)
	cookie, _ := sessionFor(t, router, "admin", usersAdminPassword)

	view := getOnboarding(t, router, cookie)
	if want := []string{"setup", "explore"}; !slices.Equal(view.Queue, want) {
		t.Fatalf("admin queue = %v, want %v", view.Queue, want)
	}
	if len(view.Setup.Items) == 0 {
		t.Fatal("setup items are missing")
	}
}

func TestQueueNeverContainsSetupForGuest(t *testing.T) {
	router, manager := newTestOnboardingRouter(t)
	guestCookie, _ := guestSession(t, router)
	readerCookie, _ := loginWithRoles(t, router, manager, "reader", []string{auth.RoleViewLogs})

	for name, cookie := range map[string]*http.Cookie{"guest": guestCookie, "user": readerCookie} {
		view := getOnboarding(t, router, cookie)
		if slices.Contains(view.Queue, "setup") {
			t.Fatalf("%s queue = %v, must not contain setup", name, view.Queue)
		}
	}
}

func TestSecondAdminDoesNotRestartSetup(t *testing.T) {
	router, manager := newTestOnboardingRouter(t)
	firstCookie, firstCSRF := sessionFor(t, router, "admin", usersAdminPassword)
	wantUsersStatus(t, usersCall(router, http.MethodPost, "/api/v1/onboarding/setup/complete", "", firstCookie, firstCSRF, true), http.StatusOK)

	secondCookie, _ := loginWithRoles(t, router, manager, "second", []string{auth.RoleUserManagement})
	view := getOnboarding(t, router, secondCookie)
	if want := []string{"explore"}; !slices.Equal(view.Queue, want) {
		t.Fatalf("second admin queue = %v, want %v", view.Queue, want)
	}
}

func TestExistingInstallBehavesLikeFresh(t *testing.T) {
	// No onboarding.json, but the settings were changed before the upgrade.
	settingsDir := t.TempDir()
	changed := settings.Default()
	changed.UpdateCheckDisabled = true
	if err := settings.NewStore(settingsDir).SaveSettings(changed); err != nil {
		t.Fatal(err)
	}
	router, _ := newOnboardingRouterWithSettings(t, filepath.Join(t.TempDir(), "users.json"), t.TempDir(), settingsDir)
	cookie, _ := sessionFor(t, router, "admin", usersAdminPassword)

	view := getOnboarding(t, router, cookie)
	if view.Setup.Done {
		t.Fatal("existing install without onboarding.json must not count as set up")
	}
	if state := itemState(view, "updates"); state != "done" {
		t.Fatalf("updates item = %q, want done from the changed settings", state)
	}
	if state := itemState(view, "theme"); state != "" {
		t.Fatalf("theme item = %q, want open", state)
	}
	if want := []string{"setup", "explore"}; !slices.Equal(view.Queue, want) {
		t.Fatalf("admin queue = %v, want %v", view.Queue, want)
	}
}
