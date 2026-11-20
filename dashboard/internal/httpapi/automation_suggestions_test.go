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

// fakeSuggestions keeps findings, the dismissed and postponed IDs and records the calls.
type fakeSuggestions struct {
	all       []autolearn.Finding
	dismissed map[string]bool
	later     map[string]bool
	calls     []string
}

func newFakeSuggestions(all ...autolearn.Finding) *fakeSuggestions {
	return &fakeSuggestions{all: all, dismissed: map[string]bool{}, later: map[string]bool{}}
}

func (f *fakeSuggestions) Visible(_ time.Time, exists func(string) bool) []autolearn.Finding {
	var out []autolearn.Finding
	for _, finding := range f.all {
		if f.dismissed[finding.ID] || f.later[finding.ID] {
			continue
		}
		if finding.Device != "" && exists != nil && !exists(finding.Device) {
			continue
		}
		out = append(out, finding)
	}
	return out
}

func (f *fakeSuggestions) Known(id string) bool {
	for _, finding := range f.all {
		if finding.ID == id {
			return true
		}
	}
	return false
}

func (f *fakeSuggestions) Dismiss(id string, _ time.Time) error {
	f.calls = append(f.calls, "dismiss:"+id)
	f.dismissed[id] = true
	return nil
}

func (f *fakeSuggestions) Later(id string, _ time.Time) error {
	f.calls = append(f.calls, "later:"+id)
	f.later[id] = true
	return nil
}

func (f *fakeSuggestions) Undo(id string) error {
	f.calls = append(f.calls, "undo:"+id)
	delete(f.dismissed, id)
	delete(f.later, id)
	return nil
}

// suggestionRouter builds the authenticated router with the suggestion deps,
// a registry holding devices dev-a and hidden, and hidden in the hidden-devices list.
func suggestionRouter(t *testing.T, source automationSuggestionSource, learning *LearningDeps) (http.Handler, *auth.Manager) {
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
		Auth:                  manager,
		DeviceFilter:          hidden,
		Learning:              learning,
		AutomationSuggestions: source,
	})
	return router, manager
}

// guestCookie logs in a guest with the given roles and returns the session cookie and CSRF token.
func guestCookie(t *testing.T, router http.Handler, manager *auth.Manager, roles []string) (*http.Cookie, string) {
	t.Helper()
	if err := manager.SetGuestRoles("admin", roles); err != nil {
		t.Fatal(err)
	}
	guest := httptest.NewRecorder()
	router.ServeHTTP(guest, httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", nil))
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(guest.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	return guest.Result().Cookies()[0], session.CSRFToken
}

func suggestionFinding(id, device string) autolearn.Finding {
	return autolearn.Finding{ID: id, Kind: "switchtime", Group: "habit", Device: device, Severity: "info", Confidence: "high"}
}

func suggestionLearning(enabled bool, since time.Time, devices int) *LearningDeps {
	return &LearningDeps{
		Settings: func() (settings.Settings, error) {
			return settings.Settings{AutomationPatternsDisabled: !enabled}, nil
		},
		Usage: func() autolearn.Usage {
			return autolearn.Usage{Since: since.Unix(), Devices: devices}
		},
	}
}

func TestAutomationSuggestionsGetNeedsViewRole(t *testing.T) {
	source := newFakeSuggestions(suggestionFinding("switchtime:a", "dev-a"))
	router, manager := suggestionRouter(t, source, suggestionLearning(true, time.Now(), 2))

	cookie, _ := guestCookie(t, router, manager, []string{auth.RoleViewOverview})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/automations/suggestions", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("GET without view_automations: %d %s", recorder.Code, recorder.Body.String())
	}

	cookie, _ = guestCookie(t, router, manager, []string{auth.RoleViewAutomations})
	request = httptest.NewRequest(http.MethodGet, "/api/v1/automations/suggestions", nil)
	request.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET with view_automations: %d %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Enabled     bool                `json:"enabled"`
		Learning    map[string]any      `json:"learning"`
		Suggestions []autolearn.Finding `json:"suggestions"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Enabled || len(body.Suggestions) != 1 || body.Suggestions[0].ID != "switchtime:a" {
		t.Fatalf("body = %+v", body)
	}
	if body.Learning["devices"] != float64(2) {
		t.Fatalf("learning = %v, want devices 2", body.Learning)
	}
}

func TestAutomationSuggestionsLearningDays(t *testing.T) {
	source := newFakeSuggestions()
	router, manager := suggestionRouter(t, source, suggestionLearning(true, time.Now().Add(-9*24*time.Hour), 14))
	cookie, _ := guestCookie(t, router, manager, []string{auth.RoleViewAutomations})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/automations/suggestions", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	var body struct {
		Learning struct {
			Since   int64 `json:"since"`
			Devices int   `json:"devices"`
			Days    int   `json:"days"`
		} `json:"learning"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Learning.Devices != 14 || body.Learning.Days != 9 || body.Learning.Since == 0 {
		t.Fatalf("learning = %+v, want devices 14 and days 9", body.Learning)
	}
}

func TestAutomationSuggestionsHiddenDeviceIsMissing(t *testing.T) {
	source := newFakeSuggestions(suggestionFinding("switchtime:a", "dev-a"), suggestionFinding("switchtime:h", "hidden"))
	router, manager := suggestionRouter(t, source, suggestionLearning(true, time.Now(), 2))
	cookie, _ := guestCookie(t, router, manager, []string{auth.RoleViewAutomations})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/automations/suggestions", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if strings.Contains(recorder.Body.String(), "switchtime:h") {
		t.Fatalf("hidden device suggestion in answer: %s", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "switchtime:a") {
		t.Fatalf("visible device suggestion missing: %s", recorder.Body.String())
	}
}

func TestAutomationSuggestionsDisabledIsEmpty(t *testing.T) {
	source := newFakeSuggestions(suggestionFinding("switchtime:a", "dev-a"))
	router, manager := suggestionRouter(t, source, suggestionLearning(false, time.Now(), 2))
	cookie, _ := guestCookie(t, router, manager, []string{auth.RoleViewAutomations})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/automations/suggestions", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	var body struct {
		Enabled     bool  `json:"enabled"`
		Suggestions []any `json:"suggestions"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Enabled || body.Suggestions == nil || len(body.Suggestions) != 0 {
		t.Fatalf("disabled body = %s", recorder.Body.String())
	}
}

func TestAutomationSuggestionActionsNeedAutomationsRole(t *testing.T) {
	source := newFakeSuggestions(suggestionFinding("switchtime:a", "dev-a"))
	router, manager := suggestionRouter(t, source, suggestionLearning(true, time.Now(), 2))

	// A view only user may not dismiss, even with a valid CSRF token.
	cookie, csrf := guestCookie(t, router, manager, []string{auth.RoleViewAutomations})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/automations/suggestions/switchtime:a/dismiss", nil)
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"suggestion_forbidden"`) {
		t.Fatalf("view only dismiss: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(source.calls) != 0 {
		t.Fatalf("dismissed by a view only user: %v", source.calls)
	}

	// With the role and CSRF the dismiss, later and undo answer ok.
	cookie, csrf = guestCookie(t, router, manager, []string{auth.RoleViewAutomations, auth.RoleAutomations})
	for _, action := range []string{"dismiss", "later", "undo"} {
		request = httptest.NewRequest(http.MethodPost, "/api/v1/automations/suggestions/switchtime:a/"+action, nil)
		request.AddCookie(cookie)
		request.Header.Set("X-CSRF-Token", csrf)
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != `{"ok":true}` {
			t.Fatalf("%s: %d %s", action, recorder.Code, recorder.Body.String())
		}
	}
	if strings.Join(source.calls, ",") != "dismiss:switchtime:a,later:switchtime:a,undo:switchtime:a" {
		t.Fatalf("calls = %v", source.calls)
	}
}

func TestAutomationSuggestionActionUnknownID(t *testing.T) {
	source := newFakeSuggestions(suggestionFinding("switchtime:a", "dev-a"))
	router, manager := suggestionRouter(t, source, suggestionLearning(true, time.Now(), 2))
	cookie, csrf := guestCookie(t, router, manager, []string{auth.RoleViewAutomations, auth.RoleAutomations})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/automations/suggestions/switchtime:missing/dismiss", nil)
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"suggestion_unknown"`) {
		t.Fatalf("unknown ID: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(source.calls) != 0 {
		t.Fatalf("unknown ID changed state: %v", source.calls)
	}
}
