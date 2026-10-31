package httpapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
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
	} `json:"setup"`
	User struct {
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
	manager, err := auth.NewManager(usersPath, "admin", usersAdminPassword)
	if err != nil {
		t.Fatal(err)
	}
	hub := companion.NewHub()
	manager.SetHAVerifier(hub)
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{
		Auth:       manager,
		Onboarding: onboarding.NewStore(onboardingDir),
	})
	return router, manager
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
