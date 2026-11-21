package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/devicefilter"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// fakeLearned returns fixed entries.
type fakeLearned struct {
	entries []autolearn.LearnedEntry
}

func (f *fakeLearned) Entries() []autolearn.LearnedEntry {
	return f.entries
}

// learnedRouter builds the authenticated router with the learned values and
// the device filter of suggestionRouter, hiding the device "hidden".
func learnedRouter(t *testing.T, source learnedValueSource, learning *LearningDeps) (http.Handler, *auth.Manager) {
	t.Helper()
	return learnedRouterWithSeason(t, source, learning, nil)
}

// learnedRouterWithSeason is learnedRouter with the season store that serves the
// yearly course of the values.
func learnedRouterWithSeason(t *testing.T, source learnedValueSource, learning *LearningDeps, season learnedSeasonSource) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	reg := registry.New()
	for _, device := range []string{"dev-a", "hidden"} {
		reg.UpsertEntity(registry.Discovery{
			Device: registry.DeviceInfo{ID: device, Name: device},
			Entity: registry.EntityInfo{UniqueID: device + "-1", Name: device, StateTopic: "s/" + device},
		})
	}
	hidden := devicefilter.NewStore(t.TempDir())
	if err := hidden.Ignore(registry.DeviceView{ID: "hidden"}); err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(reg, config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{
		Auth:          manager,
		DeviceFilter:  hidden,
		Learning:      learning,
		LearnedValues: source,
		LearnedSeason: season,
	})
	return router, manager
}

// learnedBody is the answer of GET /api/v1/automations/learned.
type learnedBody struct {
	Enabled bool               `json:"enabled"`
	Values  []learnedValueView `json:"values"`
}

func getLearned(t *testing.T, router http.Handler, cookie *http.Cookie) (*httptest.ResponseRecorder, learnedBody) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/automations/learned", nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	var body learnedBody
	if recorder.Code == http.StatusOK {
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
	}
	return recorder, body
}

func learnedEntries() []autolearn.LearnedEntry {
	return []autolearn.LearnedEntry{
		{Key: "device/dev-a/usual_off", Device: "dev-a", Kind: "time", Value: "22:30", N: 12, At: 1794000000, Prev: "22:25", ChangedAt: 1793650000},
		{Key: "site/day_start", Kind: "time", Value: "06:00", N: 30, At: 1794000000, ChangedAt: 1793650000},
		{Key: "device/hidden/usual_on", Device: "hidden", Kind: "time", Value: "07:00", N: 9, At: 1794000000, ChangedAt: 1793650000},
	}
}

func TestAutomationLearnedNeedsViewAutomations(t *testing.T) {
	router, manager := learnedRouter(t, &fakeLearned{entries: learnedEntries()}, suggestionLearning(true, time.Now(), 2))

	cookie, _ := guestCookie(t, router, manager, []string{auth.RoleViewOverview})
	if recorder, _ := getLearned(t, router, cookie); recorder.Code != http.StatusForbidden {
		t.Fatalf("GET without view_automations: %d %s", recorder.Code, recorder.Body.String())
	}

	cookie, _ = guestCookie(t, router, manager, []string{auth.RoleViewAutomations})
	if recorder, _ := getLearned(t, router, cookie); recorder.Code != http.StatusOK {
		t.Fatalf("GET with view_automations: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAutomationLearnedListsDeviceAndSiteValues(t *testing.T) {
	router, manager := learnedRouter(t, &fakeLearned{entries: learnedEntries()}, suggestionLearning(true, time.Now(), 2))
	cookie, _ := guestCookie(t, router, manager, []string{auth.RoleViewAutomations})

	recorder, body := getLearned(t, router, cookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", recorder.Code, recorder.Body.String())
	}
	if !body.Enabled {
		t.Fatalf("enabled = false, want true: %s", recorder.Body.String())
	}
	if len(body.Values) != 2 {
		t.Fatalf("values = %s, want the device value and the site value", recorder.Body.String())
	}
	device := body.Values[0]
	if device.Key != "device/dev-a/usual_off" || device.Device != "dev-a" || device.Value != "22:30" || device.N != 12 || device.Prev != "22:25" {
		t.Fatalf("device value = %+v", device)
	}
	site := body.Values[1]
	if site.Key != "site/day_start" || site.Device != "" || site.Value != "06:00" {
		t.Fatalf("site value = %+v", site)
	}
	if strings.Contains(recorder.Body.String(), "device/hidden/") {
		t.Fatalf("hidden device value in answer: %s", recorder.Body.String())
	}
}

func TestAutomationLearnedEmptyListIsNotNull(t *testing.T) {
	router, manager := learnedRouter(t, &fakeLearned{}, suggestionLearning(true, time.Now(), 2))
	cookie, _ := guestCookie(t, router, manager, []string{auth.RoleViewAutomations})

	recorder, _ := getLearned(t, router, cookie)
	if !strings.Contains(recorder.Body.String(), `"values":[]`) {
		t.Fatalf("empty list body = %s", recorder.Body.String())
	}
}

func TestAutomationLearnedDisabledKeepsValues(t *testing.T) {
	router, manager := learnedRouter(t, &fakeLearned{entries: learnedEntries()}, suggestionLearning(false, time.Now(), 2))
	cookie, _ := guestCookie(t, router, manager, []string{auth.RoleViewAutomations})

	recorder, body := getLearned(t, router, cookie)
	if body.Enabled {
		t.Fatalf("enabled = true with Muster lernen off: %s", recorder.Body.String())
	}
	if len(body.Values) != 2 {
		t.Fatalf("values = %s, want the values kept while learning is off", recorder.Body.String())
	}
}

func TestAutomationLearnedHabitsOffIsDisabled(t *testing.T) {
	learning := &LearningDeps{
		Settings: func() (settings.Settings, error) {
			return settings.Settings{AutomationHabitsDisabled: true}, nil
		},
	}
	router, manager := learnedRouter(t, &fakeLearned{entries: learnedEntries()}, learning)
	cookie, _ := guestCookie(t, router, manager, []string{auth.RoleViewAutomations})

	recorder, body := getLearned(t, router, cookie)
	if body.Enabled {
		t.Fatalf("enabled = true with the habit group off: %s", recorder.Body.String())
	}
}

func TestAutomationLearnedWithoutSourceIsNotImplemented(t *testing.T) {
	router, manager := learnedRouter(t, nil, suggestionLearning(true, time.Now(), 2))
	cookie, _ := guestCookie(t, router, manager, []string{auth.RoleViewAutomations})

	if recorder, _ := getLearned(t, router, cookie); recorder.Code != http.StatusNotImplemented {
		t.Fatalf("without source: %d %s", recorder.Code, recorder.Body.String())
	}
}

// weekKeyBack is the ISO week key of the week n weeks before now.
func weekKeyBack(now time.Time, n int) string {
	y, w := now.AddDate(0, 0, -7*n).ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// yearSeasonStore holds the given number of weeks, each with the same learned
// clock time (minutes of the day) for the key of learnedEntries.
func yearSeasonStore(t *testing.T, weeks int, minutes float64) *autolearn.SeasonStore {
	t.Helper()
	store := autolearn.LoadSeasonStore(filepath.Join(t.TempDir(), "season.json"), nil)
	now := time.Now()
	for n := 0; n < weeks; n++ {
		store.Put(autolearn.Week{Key: weekKeyBack(now, n), Source: "days", Learned: map[string]float64{"device/dev-a/usual_off": minutes}})
	}
	return store
}

func TestAutomationLearnedYearHasWeeksOfTheValue(t *testing.T) {
	season := yearSeasonStore(t, 30, 22*60+30)
	router, manager := learnedRouterWithSeason(t, &fakeLearned{entries: learnedEntries()}, suggestionLearning(true, time.Now(), 2), season)
	cookie, _ := guestCookie(t, router, manager, []string{auth.RoleViewAutomations})

	recorder, body := getLearned(t, router, cookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(body.Values) != 2 {
		t.Fatalf("values = %s", recorder.Body.String())
	}
	if got := len(body.Values[0].Year); got != 30 {
		t.Fatalf("year entries = %d, want 30: %s", got, recorder.Body.String())
	}
	if got := body.Values[0].Year[29].Value; got != "22:30" {
		t.Fatalf("year value = %v, want the clock text 22:30", got)
	}
	if body.Values[1].Year != nil {
		t.Fatalf("site value without weeks has a year: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"year":null`) {
		t.Fatalf("year is null: %s", recorder.Body.String())
	}
}

func TestAutomationLearnedYearKeepsFiveWeeks(t *testing.T) {
	season := yearSeasonStore(t, 5, 6*60)
	router, manager := learnedRouterWithSeason(t, &fakeLearned{entries: learnedEntries()}, suggestionLearning(true, time.Now(), 2), season)
	cookie, _ := guestCookie(t, router, manager, []string{auth.RoleViewAutomations})

	recorder, body := getLearned(t, router, cookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", recorder.Code, recorder.Body.String())
	}
	if got := len(body.Values[0].Year); got != 5 {
		t.Fatalf("year entries = %d, want 5 (the eight week rule is UI only): %s", got, recorder.Body.String())
	}
	if got := body.Values[0].Year[0].Value; got != "06:00" {
		t.Fatalf("year value = %v, want 06:00", got)
	}
}

func TestAutomationLearnedYearIsCappedAtFiftyThree(t *testing.T) {
	season := yearSeasonStore(t, 60, 480)
	router, manager := learnedRouterWithSeason(t, &fakeLearned{entries: learnedEntries()}, suggestionLearning(true, time.Now(), 2), season)
	cookie, _ := guestCookie(t, router, manager, []string{auth.RoleViewAutomations})

	_, body := getLearned(t, router, cookie)
	if got := len(body.Values[0].Year); got > 53 {
		t.Fatalf("year entries = %d, want at most 53", got)
	}
}
