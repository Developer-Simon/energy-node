package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/chainsuggest"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

var errSaveFailed = errors.New("save failed")

// fakeChainSuggester records the dismissed pairs and answers fixed suggestions.
type fakeChainSuggester struct {
	suggestions []chainsuggest.Suggestion
	dismissed   [][2]string
	dismissErr  error
	disabled    bool
	calls       int
}

func (f *fakeChainSuggester) Enabled() bool { return !f.disabled }

func (f *fakeChainSuggester) Suggestions(energy.Model, func(string, string) bool) []chainsuggest.Suggestion {
	f.calls++
	return f.suggestions
}

func (f *fakeChainSuggester) Dismiss(a, b string) error {
	if f.dismissErr != nil {
		return f.dismissErr
	}
	f.dismissed = append(f.dismissed, [2]string{a, b})
	return nil
}

func TestChainSuggestionsGet(t *testing.T) {
	reg := registry.New()
	store := settings.NewStore(t.TempDir())
	if err := store.SaveDeviceMap(settings.DeviceMap{EdgeProps: []settings.DeviceMapEdgeProp{{ParentID: "steckdose", ChildID: "speicher", Type: "energy"}}}); err != nil {
		t.Fatal(err)
	}
	suggester := &fakeChainSuggester{suggestions: []chainsuggest.Suggestion{{A: "plug", B: "ac", DeviceA: "steckdose", DeviceB: "speicher", R: 0.99, Edge: true}}}
	router := NewRouterWithDependencies(reg, config.NewManager(t.TempDir()), store, nil, nil, nil, nil, nil, RouterDependencies{ChainSuggest: suggester})

	recorder := doChainRequest(router, http.MethodGet, energyChainSuggestionsPath, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Suggestions []chainsuggest.Suggestion `json:"suggestions"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Suggestions) != 1 || body.Suggestions[0].A != "plug" || !body.Suggestions[0].Edge {
		t.Fatalf("suggestions = %+v", body.Suggestions)
	}
	if !strings.Contains(recorder.Body.String(), `"disabled":false`) {
		t.Fatalf("answer = %s, want disabled false", recorder.Body.String())
	}

	// An empty collector answers an empty list, never null.
	suggester.suggestions = nil
	recorder = doChainRequest(router, http.MethodGet, energyChainSuggestionsPath, "")
	if !strings.Contains(recorder.Body.String(), `"suggestions":[]`) {
		t.Fatalf("empty answer = %s, want an empty list", recorder.Body.String())
	}

	// Without a collector the route answers 501 like the chain endpoints.
	bare := NewRouter(reg, nil, store)
	if recorder := doChainRequest(bare, http.MethodGet, energyChainSuggestionsPath, ""); recorder.Code != http.StatusNotImplemented {
		t.Fatalf("GET without collector status %d, want 501", recorder.Code)
	}
}

func TestChainSuggestionsDismissNeedsEditEnergy(t *testing.T) {
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetGuestRoles("admin", []string{auth.RoleViewEnergy, auth.RoleEditEnergy}); err != nil {
		t.Fatal(err)
	}
	suggester := &fakeChainSuggester{}
	store := settings.NewStore(t.TempDir())

	// A user who sees the energy tab but may not edit it must not dismiss.
	viewOnly := httptest.NewRequest(http.MethodPost, energyChainSuggestionsDismissPath, strings.NewReader(`{"a":"plug","b":"ac"}`))
	viewOnly = viewOnly.WithContext(auth.WithUser(viewOnly.Context(), auth.User{Username: "tester", Roles: []string{auth.RoleViewEnergy}}))
	recorder := httptest.NewRecorder()
	handleEnergyChainSuggestionDismiss(manager, suggester).ServeHTTP(recorder, viewOnly)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"energy_forbidden"`) {
		t.Fatalf("view only: %d %s", recorder.Code, recorder.Body.String())
	}

	// The full router with edit_energy: a POST without CSRF is refused, with it 204.
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), store, nil, nil, nil, nil, nil, RouterDependencies{Auth: manager, ChainSuggest: suggester})
	guest := httptest.NewRecorder()
	router.ServeHTTP(guest, httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", nil))
	cookie := guest.Result().Cookies()[0]
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(guest.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}

	without := httptest.NewRequest(http.MethodPost, energyChainSuggestionsDismissPath, strings.NewReader(`{"a":"plug","b":"ac"}`))
	without.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, without)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"csrf_failed"`) {
		t.Fatalf("POST without CSRF: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(suggester.dismissed) != 0 {
		t.Fatalf("dismissed without CSRF: %v", suggester.dismissed)
	}

	with := httptest.NewRequest(http.MethodPost, energyChainSuggestionsDismissPath, strings.NewReader(`{"a":"plug","b":"ac"}`))
	with.AddCookie(cookie)
	with.Header.Set("X-CSRF-Token", session.CSRFToken)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, with)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("POST with CSRF: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(suggester.dismissed) != 1 || suggester.dismissed[0] != [2]string{"plug", "ac"} {
		t.Fatalf("dismissed = %v, want plug-ac", suggester.dismissed)
	}
}

func TestChainSuggestionsDismissRejectsMissingIDs(t *testing.T) {
	suggester := &fakeChainSuggester{}
	router := NewRouterWithDependencies(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{ChainSuggest: suggester})

	for _, body := range []string{`{"a":"plug"}`, `{"a":"","b":"ac"}`, `{"a":"plug","b":"plug"}`, `{}`} {
		recorder := doChainRequest(router, http.MethodPost, energyChainSuggestionsDismissPath, body)
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"chain_suggestion_invalid"`) {
			t.Fatalf("%s: %d %s", body, recorder.Code, recorder.Body.String())
		}
	}
	if len(suggester.dismissed) != 0 {
		t.Fatalf("invalid bodies dismissed %v", suggester.dismissed)
	}
}

func TestChainSuggestionsDismissSaveFailureIsServerError(t *testing.T) {
	suggester := &fakeChainSuggester{dismissErr: errSaveFailed}
	router := NewRouterWithDependencies(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{ChainSuggest: suggester})

	recorder := doChainRequest(router, http.MethodPost, energyChainSuggestionsDismissPath, `{"a":"plug","b":"ac"}`)
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), `"code":"chain_suggestion_failed"`) {
		t.Fatalf("save failure: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestChainSuggestionsGetDisabled(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	suggester := &fakeChainSuggester{disabled: true, suggestions: []chainsuggest.Suggestion{{A: "plug", B: "ac"}}}
	router := NewRouterWithDependencies(registry.New(), config.NewManager(t.TempDir()), store, nil, nil, nil, nil, nil, RouterDependencies{ChainSuggest: suggester})

	recorder := doChainRequest(router, http.MethodGet, energyChainSuggestionsPath, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := strings.TrimSpace(recorder.Body.String()); got != `{"suggestions":[],"disabled":true}` {
		t.Fatalf("body = %s, want the disabled answer", got)
	}
	if suggester.calls != 0 {
		t.Fatalf("Suggestions called %d times while disabled, want 0", suggester.calls)
	}
}
