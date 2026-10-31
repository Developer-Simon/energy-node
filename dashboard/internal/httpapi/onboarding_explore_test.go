package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
)

// exploreChangelog has two dashboard highlights of v0.10.0: h190 with a tour
// step that needs view_devices, h191 with a step for everyone. v0.9.0 is
// older than every range below.
const exploreChangelog = `{"schema_version":1,"components":[{"id":"dashboard","label":"Dashboard","version":"v0.10.0","releases":[
  {"version":"v0.10.0","groups":[{"type":"feat","entries":[
    {"text":"favourites","pr":190,"breaking":false,"highlight":true,
     "tour":{"steps":[{"target":"favorites","device":"any","need":"view_devices","title":{"en":"F"},"text":{"en":"F"}}]}},
    {"text":"checklist","pr":191,"breaking":false,"highlight":true,
     "tour":{"steps":[{"target":"checklist","device":"any","title":{"en":"C"},"text":{"en":"C"}}]}}]}]},
  {"version":"v0.9.0","groups":[{"type":"feat","entries":[
    {"text":"older","pr":150,"breaking":false,"highlight":true}]}]}]}]}`

// exploreState writes an installer state with exploreChangelog. With update
// set it adds a previous-manifest.json of v0.9.0, as after an update.
func exploreState(t *testing.T, update bool) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"changelog.json":          exploreChangelog,
		"installed-manifest.json": `{"version":"v0.10.0","components":{"dashboard":"v0.10.0"}}`,
	}
	if update {
		files["previous-manifest.json"] = `{"version":"v0.9.0","components":{"dashboard":"v0.9.0"}}`
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func newExploreRouter(t *testing.T, update bool) (http.Handler, *auth.Manager) {
	t.Helper()
	return newOnboardingRouterWithState(t, filepath.Join(t.TempDir(), "users.json"), t.TempDir(), t.TempDir(), exploreState(t, update))
}

func exploreIDs(view onboardingView) []string {
	ids := []string{}
	for _, item := range view.Explore.Items {
		ids = append(ids, item.ID)
	}
	return ids
}

func TestFreshInstallHasNothingUnseen(t *testing.T) {
	router, _ := newExploreRouter(t, false)
	cookie, _ := sessionFor(t, router, "admin", usersAdminPassword)
	view := getOnboarding(t, router, cookie)
	if view.Explore.CurrentVersion != "v0.10.0" || !slices.Equal(exploreIDs(view), []string{"h190", "h191"}) {
		t.Fatalf("explore = %+v", view.Explore)
	}
	if !view.Explore.BaselineSeen || !slices.Equal(view.User.Seen, []string{"h190", "h191"}) {
		t.Fatalf("fresh install: seen = %v, baseline = %v", view.User.Seen, view.Explore.BaselineSeen)
	}
	if want := []string{"setup", "explore"}; !slices.Equal(view.Queue, want) {
		t.Fatalf("first admin queue = %v, want %v", view.Queue, want)
	}
}

func TestExistingInstallUnseenStartsAtOnboardingRelease(t *testing.T) {
	router, _ := newExploreRouter(t, true)
	cookie, _ := sessionFor(t, router, "admin", usersAdminPassword)
	view := getOnboarding(t, router, cookie)
	if view.Explore.BaselineSeen || len(view.User.Seen) != 0 {
		t.Fatalf("update: seen = %v, baseline = %v, want nothing seen", view.User.Seen, view.Explore.BaselineSeen)
	}
	if !slices.Equal(exploreIDs(view), []string{"h190", "h191"}) {
		t.Fatalf("items = %v, want the highlights of this update only", exploreIDs(view))
	}
	// The seed is stored once: a second call keeps the same state.
	if again := getOnboarding(t, router, cookie); len(again.User.Seen) != 0 || again.Explore.BaselineSeen {
		t.Fatalf("second call = %+v", again.User)
	}
}

func TestNewUserGetsWelcomeAndEverythingSeen(t *testing.T) {
	router, manager := newExploreRouter(t, true)
	adminCookie, _ := sessionFor(t, router, "admin", usersAdminPassword)
	getOnboarding(t, router, adminCookie) // starts the onboarding
	cookie, _ := loginWithRoles(t, router, manager, "newcomer", []string{auth.RoleViewDevices})
	view := getOnboarding(t, router, cookie)
	if !view.Explore.BaselineSeen || !slices.Equal(view.User.Seen, []string{"h190", "h191"}) {
		t.Fatalf("new user: seen = %v, baseline = %v", view.User.Seen, view.Explore.BaselineSeen)
	}
	if want := []string{"welcome"}; !slices.Equal(view.Queue, want) {
		t.Fatalf("new user queue = %v, want %v", view.Queue, want)
	}
}

func TestWelcomeReplacesExploreInQueue(t *testing.T) {
	router, manager := newExploreRouter(t, true)
	adminCookie, adminCSRF := sessionFor(t, router, "admin", usersAdminPassword)
	getOnboarding(t, router, adminCookie)
	wantUsersStatus(t, usersCall(router, http.MethodPost, "/api/v1/onboarding/setup/complete", "", adminCookie, adminCSRF, true), http.StatusOK)

	secondCookie, secondCSRF := loginWithRoles(t, router, manager, "second", []string{auth.RoleUserManagement})
	if got, want := getOnboarding(t, router, secondCookie).Queue, []string{"welcome"}; !slices.Equal(got, want) {
		t.Fatalf("new admin after the setup: queue = %v, want %v", got, want)
	}
	wantUsersStatus(t, usersCall(router, http.MethodPut, "/api/v1/onboarding/me", `{"welcomed":true}`, secondCookie, secondCSRF, true), http.StatusOK)
	if got, want := getOnboarding(t, router, secondCookie).Queue, []string{"explore"}; !slices.Equal(got, want) {
		t.Fatalf("after the welcome: queue = %v, want %v", got, want)
	}
	guestCookie, _ := guestSession(t, router)
	if got, want := getOnboarding(t, router, guestCookie).Queue, []string{"welcome"}; !slices.Equal(got, want) {
		t.Fatalf("guest queue = %v, want %v", got, want)
	}
}

func TestRightsFilterLeavesTextOnlyAsSeen(t *testing.T) {
	router, manager := newExploreRouter(t, true)
	cookie, _ := loginWithRoles(t, router, manager, "reader", []string{auth.RoleViewLogs})
	tours := map[string]bool{}
	for _, item := range getOnboarding(t, router, cookie).Explore.Items {
		tours[item.ID] = item.Tour != nil
	}
	if tours["h190"] || !tours["h191"] {
		t.Fatalf("reader tours = %v, want h190 as text only (needs view_devices) and h191 with its step", tours)
	}
	adminCookie, _ := sessionFor(t, router, "admin", usersAdminPassword)
	for _, item := range getOnboarding(t, router, adminCookie).Explore.Items {
		if item.Tour == nil {
			t.Fatalf("admin lost the tour of %s", item.ID)
		}
	}
}
