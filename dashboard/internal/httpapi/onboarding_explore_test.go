package httpapi

import (
	"encoding/json"
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

// newExploreRouterWithTargets serves the update state with the given web UI
// data-tour targets.
func newExploreRouterWithTargets(t *testing.T, targets func() map[string]bool) (http.Handler, *auth.Manager) {
	t.Helper()
	return newOnboardingRouterWithTargets(t, filepath.Join(t.TempDir(), "users.json"), t.TempDir(), t.TempDir(), exploreState(t, true), targets)
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
	if view.Explore.CurrentVersion != "v0.10.0" || !slices.Equal(exploreIDs(view), []string{"h190", "h191", "h150"}) {
		t.Fatalf("explore = %+v", view.Explore)
	}
	if !view.Explore.BaselineSeen || !slices.Equal(view.User.Seen, []string{"h190", "h191", "h150"}) {
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
	if !slices.Equal(exploreIDs(view), []string{"h190", "h191", "h150"}) {
		t.Fatalf("items = %v, want the highlights of the last three minors", exploreIDs(view))
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
	if !view.Explore.BaselineSeen || !slices.Equal(view.User.Seen, []string{"h190", "h191", "h150"}) {
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
	adminTours := map[string]bool{}
	for _, item := range getOnboarding(t, router, adminCookie).Explore.Items {
		adminTours[item.ID] = item.Tour != nil
	}
	// h150 is a v0.9.0 highlight without any tour, so it is text only for everyone.
	if !adminTours["h190"] || !adminTours["h191"] || adminTours["h150"] {
		t.Fatalf("admin tours = %v, want h190 and h191 with tours and h150 as text only", adminTours)
	}
}

func TestExploreItemsCarryTheMinorOfTheirRelease(t *testing.T) {
	router, _ := newExploreRouter(t, true)
	cookie, _ := sessionFor(t, router, "admin", usersAdminPassword)
	minors := map[string]string{}
	for _, item := range getOnboarding(t, router, cookie).Explore.Items {
		minors[item.ID] = item.Minor
	}
	// h150 is a v0.9.0 highlight: the second-latest minor is listed too.
	if minors["h190"] != "0.10" || minors["h191"] != "0.10" || minors["h150"] != "0.9" {
		t.Fatalf("minors = %v, want h190 and h191 at 0.10 and h150 at 0.9", minors)
	}
}

func TestUnknownTourTargetIsStale(t *testing.T) {
	// The web UI knows only the checklist target: h190 points at "favorites",
	// which no template carries any more.
	router, _ := newExploreRouterWithTargets(t, func() map[string]bool {
		return map[string]bool{"checklist": true}
	})
	cookie, _ := sessionFor(t, router, "admin", usersAdminPassword)
	stale := map[string]bool{}
	for _, item := range getOnboarding(t, router, cookie).Explore.Items {
		if item.Tour == nil {
			continue // h150 has no tour at all
		}
		stale[item.ID] = item.Tour.Stale
	}
	if !stale["h190"] || stale["h191"] {
		t.Fatalf("stale = %v, want h190 stale and h191 not", stale)
	}
}

func TestWithoutKnownTargetsNothingIsStale(t *testing.T) {
	router, _ := newExploreRouter(t, true)
	cookie, _ := sessionFor(t, router, "admin", usersAdminPassword)
	for _, item := range getOnboarding(t, router, cookie).Explore.Items {
		if item.Tour != nil && item.Tour.Stale {
			t.Fatalf("item %s: tour is stale without known targets", item.ID)
		}
	}
}

// attachChangelog has two dashboard releases and a bootstrap release dated
// between them: bootstrap v0.2.5 belongs under v0.10.8, v0.2.6 under v0.10.33.
const attachChangelog = `{"schema_version":1,"components":[
 {"id":"dashboard","label":"Dashboard","kind":"app","version":"v0.10.33","releases":[
  {"version":"v0.10.33","date":"2026-10-31","groups":[{"type":"feat","entries":[{"text":"dashboard new","pr":300,"breaking":false,"highlight":true}]}]},
  {"version":"v0.10.8","date":"2026-10-22","groups":[{"type":"fix","entries":[{"text":"dashboard eight","pr":208,"breaking":false,"highlight":true}]}]}]},
 {"id":"bootstrap","label":"Bootstrap","kind":"tool","version":"v0.2.6","releases":[
  {"version":"v0.2.6","date":"2026-10-31","groups":[{"type":"fix","entries":[{"text":"bootstrap six","pr":260,"breaking":false,"highlight":true}]}]},
  {"version":"v0.2.5","date":"2026-10-20","groups":[{"type":"feat","entries":[{"text":"bootstrap five","pr":250,"breaking":false,"highlight":true}]}]}]}]}`

func TestExploreItemsSitUnderTheirDashboardRelease(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"changelog.json":          attachChangelog,
		"installed-manifest.json": `{"version":"v0.10.33","components":{"dashboard":"v0.10.33","bootstrap":"v0.2.6"}}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	router, _ := newOnboardingRouterWithState(t, filepath.Join(t.TempDir(), "users.json"), t.TempDir(), t.TempDir(), dir)
	cookie, _ := sessionFor(t, router, "admin", usersAdminPassword)
	recorder := usersCall(router, http.MethodGet, "/api/v1/onboarding", "", cookie, "", true)
	wantUsersStatus(t, recorder, http.StatusOK)
	var body struct {
		Explore struct {
			CurrentVersion string `json:"current_version"`
			Items          []struct {
				ID               string `json:"id"`
				Version          string `json:"version"`
				Minor            string `json:"minor"`
				Component        string `json:"component"`
				ComponentLabel   string `json:"component_label"`
				ComponentVersion string `json:"component_version"`
			} `json:"items"`
		} `json:"explore"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Explore.CurrentVersion != "v0.10.33" {
		t.Fatalf("current_version = %q, want v0.10.33", body.Explore.CurrentVersion)
	}
	type item struct{ ID, Version, Minor, Component, ComponentLabel, ComponentVersion string }
	var got []item
	for _, it := range body.Explore.Items {
		got = append(got, item{it.ID, it.Version, it.Minor, it.Component, it.ComponentLabel, it.ComponentVersion})
	}
	want := []item{
		{"h300", "v0.10.33", "0.10", "dashboard", "Dashboard", "v0.10.33"},
		{"h260", "v0.10.33", "0.10", "bootstrap", "Bootstrap", "v0.2.6"},
		{"h208", "v0.10.8", "0.10", "dashboard", "Dashboard", "v0.10.8"},
		// h250 (bootstrap v0.2.5, 2026-10-20) is dated before the oldest window
		// release v0.10.8 (2026-10-22): it belongs to no card and is left out.
	}
	if !slices.Equal(got, want) {
		t.Fatalf("items = %+v\nwant %+v", got, want)
	}
}
