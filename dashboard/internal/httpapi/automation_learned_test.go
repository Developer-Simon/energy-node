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
	})
	return router, manager
}

// learnedBody is the answer of GET /api/v1/automations/learned.
type learnedBody struct {
	Enabled bool                     `json:"enabled"`
	Values  []autolearn.LearnedEntry `json:"values"`
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
