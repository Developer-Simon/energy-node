package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func serveWithUser(t *testing.T, handler http.Handler, user *auth.User) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
	if user != nil {
		request = request.WithContext(auth.WithUser(context.Background(), *user))
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestGuardedMuxPanicsOnUnknownPattern(t *testing.T) {
	g := &guardedMux{mux: http.NewServeMux()}
	defer func() {
		recovered, _ := recover().(string)
		if recovered != "httpapi: route without access group: /api/v1/nope" {
			t.Fatalf("panic = %q", recovered)
		}
	}()
	g.HandleFunc("/api/v1/nope", func(w http.ResponseWriter, r *http.Request) {})
	t.Fatal("registering a pattern without access group must panic")
}

func TestGuardedMuxRegistersKnownPattern(t *testing.T) {
	routeTable["/api/v1/probe"] = routeAccess{group: groupPage, view: auth.RoleViewDiagnostics}
	t.Cleanup(func() { delete(routeTable, "/api/v1/probe") })

	g := &guardedMux{mux: http.NewServeMux()}
	g.HandleFunc("/api/v1/probe", okHandler().ServeHTTP)
	if len(g.patterns) != 1 || g.patterns[0] != "/api/v1/probe" {
		t.Fatalf("patterns = %v", g.patterns)
	}
	recorder := serveWithUser(t, g.ServeMux(), &auth.User{Roles: []string{auth.RoleViewStats}})
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}

func TestViewGateRejectsMissingPagePermission(t *testing.T) {
	gate := viewGate(routeAccess{group: groupPage, view: auth.RoleViewDiagnostics}, okHandler())

	without := serveWithUser(t, gate, &auth.User{Roles: []string{auth.RoleViewStats}})
	if without.Code != http.StatusForbidden {
		t.Fatalf("without view_diagnostics: status %d", without.Code)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(without.Body.Bytes(), &body); err != nil || body.Code != "view_forbidden" {
		t.Fatalf("body = %s (%v)", without.Body.String(), err)
	}

	with := serveWithUser(t, gate, &auth.User{Roles: []string{auth.RoleViewDiagnostics}})
	if with.Code != http.StatusOK {
		t.Fatalf("with view_diagnostics: status %d", with.Code)
	}

	anonymous := serveWithUser(t, gate, nil)
	if anonymous.Code != http.StatusOK {
		t.Fatalf("no user in context must pass, status %d", anonymous.Code)
	}
}

func TestViewGateAnyViewNeedsOneViewPermission(t *testing.T) {
	gate := viewGate(routeAccess{group: groupAnyView}, okHandler())

	if code := serveWithUser(t, gate, &auth.User{}).Code; code != http.StatusForbidden {
		t.Fatalf("user without views: status %d", code)
	}
	if code := serveWithUser(t, gate, &auth.User{Roles: []string{auth.RoleViewStats}}).Code; code != http.StatusOK {
		t.Fatalf("user with view_stats: status %d", code)
	}
}

func TestConfigurationViewFollowsTheName(t *testing.T) {
	cases := map[string]string{
		"/api/v1/configurations/automation_rules":        auth.RoleViewAutomations,
		"/api/v1/configurations/automation_rules/schema": auth.RoleViewAutomations,
		"/api/v1/configurations/shelly":                  auth.RoleViewConfig,
		"/api/v1/configurations/shelly/automation_rules": auth.RoleViewConfig,
		"/api/v1/configurations/":                        auth.RoleViewConfig,
	}
	for path, want := range cases {
		if got := configurationView(httptest.NewRequest(http.MethodGet, path, nil)); got != want {
			t.Errorf("configurationView(%s) = %s, want %s", path, got, want)
		}
	}
}

func TestViewGateResolvesPathDependentPermission(t *testing.T) {
	gate := viewGate(routeAccess{group: groupPage, resolve: configurationView}, okHandler())

	serveAt := func(path string, user auth.User) int {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request = request.WithContext(auth.WithUser(context.Background(), user))
		recorder := httptest.NewRecorder()
		gate.ServeHTTP(recorder, request)
		return recorder.Code
	}
	automations := auth.User{Roles: []string{auth.RoleViewAutomations}}
	if code := serveAt("/api/v1/configurations/automation_rules", automations); code != http.StatusOK {
		t.Fatalf("automation_rules with view_automations: status %d", code)
	}
	if code := serveAt("/api/v1/configurations/shelly", automations); code != http.StatusForbidden {
		t.Fatalf("shelly with only view_automations: status %d", code)
	}
}

// registeredPatternPattern matches a route registration in the source, for
// example g.HandleFunc("/api/v1/devices", … or mux.Handle("/static/", ….
var registeredPatternPattern = regexp.MustCompile(`\b(?:g|mux)\.Handle(?:Func)?\("([^"]+)"`)

func registeredPatternsInSource(t *testing.T) map[string]bool {
	t.Helper()
	patterns := map[string]bool{}
	for _, file := range []string{"httpapi.go", "history_exchange.go"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range registeredPatternPattern.FindAllStringSubmatch(string(data), -1) {
			patterns[match[1]] = true
		}
	}
	return patterns
}

func TestEveryRegisteredRouteHasAnAccessGroup(t *testing.T) {
	registered := registeredPatternsInSource(t)
	if len(registered) == 0 {
		t.Fatal("no route registrations found, the scan is broken")
	}
	for pattern := range registered {
		if _, ok := routeTable[pattern]; !ok {
			t.Errorf("route %s has no access group in routeTable", pattern)
		}
	}
	for pattern := range routeTable {
		if !registered[pattern] {
			t.Errorf("routeTable entry %s is stale, no route registers it", pattern)
		}
	}
}

// sessionRequest builds a GET request with the session cookie. secure selects
// the HTTPS header and the matching cookie name.
func sessionRequest(t *testing.T, path string, cookie *http.Cookie, secure bool) *http.Request {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	// Streaming handlers such as /api/v1/events return only when the request ends.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	t.Cleanup(cancel)
	request = request.WithContext(ctx)
	if secure {
		request.Header.Set("X-Forwarded-Proto", "https")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	return request
}

// viewForbiddenCode reports whether a response is the view gate's 403.
func viewForbiddenCode(recorder *httptest.ResponseRecorder) bool {
	var body struct {
		Code string `json:"code"`
	}
	return recorder.Code == http.StatusForbidden && json.Unmarshal(recorder.Body.Bytes(), &body) == nil && body.Code == "view_forbidden"
}

// assertPassedGate fails when a logged-in request was refused by the view gate
// (403 view_forbidden) or lost its session (401). Any other status means the
// handler ran; callers that know the handler's normal status assert it too.
func assertPassedGate(t *testing.T, recorder *httptest.ResponseRecorder, path string) {
	t.Helper()
	if viewForbiddenCode(recorder) {
		t.Errorf("%s refused by the view gate: %s", path, recorder.Body.String())
	}
	if recorder.Code == http.StatusUnauthorized {
		t.Errorf("%s answered 401, the session was lost: %s", path, recorder.Body.String())
	}
}

func TestConfigurationsGateFollowsTheName(t *testing.T) {
	router, manager := newRestrictedTestRouter(t, nil)
	cookie, _ := loginWithRoles(t, router, manager, "automata", []string{auth.RoleViewAutomations})

	allowed := httptest.NewRecorder()
	router.ServeHTTP(allowed, sessionRequest(t, "/api/v1/configurations/automation_rules", cookie, true))
	if viewForbiddenCode(allowed) {
		t.Fatalf("automation_rules with view_automations was gated: %s", allowed.Body.String())
	}
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, sessionRequest(t, "/api/v1/configurations/shelly", cookie, true))
	if !viewForbiddenCode(denied) {
		t.Fatalf("shelly with only view_automations: status %d, want view_forbidden", denied.Code)
	}
}

func TestOverviewOnlyAccountKeepsSharedLiveData(t *testing.T) {
	router, manager := newRestrictedTestRouter(t, nil)
	cookie, _ := loginWithRoles(t, router, manager, "olga", []string{auth.RoleViewOverview})
	// The wanted status is the handler's normal answer on an empty dashboard.
	// /api/v1/events is a stream that the request timeout ends, so it reports
	// the 200 it wrote before that.
	want := map[string]int{
		"/api/v1/events":           http.StatusOK,
		"/api/v1/energy":           http.StatusOK,
		"/api/v1/history/entities": http.StatusOK,
		"/api/v1/energy/stats":     http.StatusNotImplemented, // no energy stats service in this test
		"/api/v1/device/map":       http.StatusOK,
	}
	for path, status := range want {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, sessionRequest(t, path, cookie, true))
		assertPassedGate(t, recorder, path)
		if recorder.Code != status {
			t.Errorf("%s: status %d, want %d: %s", path, recorder.Code, status, recorder.Body.String())
		}
	}
}

func TestAccountWithoutAnyViewIsRejectedOnSharedData(t *testing.T) {
	router, manager := newRestrictedTestRouter(t, nil)
	cookie, _ := loginWithRoles(t, router, manager, "nora", nil)

	devices := httptest.NewRecorder()
	router.ServeHTTP(devices, sessionRequest(t, "/api/v1/devices", cookie, true))
	if !viewForbiddenCode(devices) {
		t.Fatalf("/api/v1/devices without any view: status %d, want view_forbidden", devices.Code)
	}
	settings := httptest.NewRecorder()
	router.ServeHTTP(settings, sessionRequest(t, "/api/v1/settings", cookie, true))
	if settings.Code != http.StatusOK {
		t.Fatalf("/api/v1/settings without any view: status %d, want 200", settings.Code)
	}
}

func TestHistoryExchangeIgnoresViewPermissions(t *testing.T) {
	router, manager := newRestrictedTestRouter(t, nil)
	cookie, _ := loginWithRoles(t, router, manager, "heinz", []string{auth.RoleViewStats})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, sessionRequest(t, "/api/v1/history/exchange", cookie, true))
	if viewForbiddenCode(recorder) {
		t.Fatalf("history exchange gated without view_history: %s", recorder.Body.String())
	}
}

func TestNoUserMeansNoViewGate(t *testing.T) {
	router := NewRouterWithDependencies(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, sessionRequest(t, "/api/v1/diagnostics", nil, false))
	if recorder.Code != http.StatusOK {
		t.Fatalf("without auth: status %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
}

// TestMigratedGuestPassesEveryViewGate: a guest created on a users.json in the
// field format (no version, guest roles from before the permission model) gets
// the migrated view roles and is never refused by a view gate.
func TestMigratedGuestPassesEveryViewGate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	// Version 4 with a customised guest list that holds every view role, so
	// the migration keeps it and the sweep covers all view gates.
	fixture := `{"version":4,"guest_roles":["view_overview","view_devices","view_device_analysis","view_history","view_stats",` +
		`"view_energy","view_device_map","view_config","view_automations","view_diagnostics"],` +
		`"users":[{"username":"admin","roles":["system_actions","tune_live_updates","automations"]},` +
		`{"username":"guest-abc","guest":true,"roles":["automations","edit_layout","check_updates","edit_energy","edit_device_map","edit_device_map_view"]}]}`
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := auth.NewManager(path, "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	deps := RouterDependencies{Auth: manager}
	reg, configs, store := registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())
	router := NewAuthenticatedRouter(reg, configs, store, nil, nil, nil, nil, nil, deps)
	// The same wiring without the session middleware, used to read which
	// pattern each path is registered under.
	mux := NewRouterWithDependencies(reg, configs, store, nil, nil, nil, nil, nil, deps)

	guest := httptest.NewRecorder()
	router.ServeHTTP(guest, httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", nil))
	if guest.Code != http.StatusOK {
		t.Fatalf("guest login status %d: %s", guest.Code, guest.Body.String())
	}
	cookies := guest.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("guest cookies = %+v", cookies)
	}
	session := httptest.NewRecorder()
	router.ServeHTTP(session, sessionRequest(t, "/api/v1/auth/session", cookies[0], false))
	if session.Code != http.StatusOK {
		t.Fatalf("guest session not recognized: status %d: %s", session.Code, session.Body.String())
	}

	patterns := make([]string, 0, len(routeTable))
	for pattern := range routeTable {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	for _, pattern := range patterns {
		// Open routes have no view gate, so the sweep leaves them out; their
		// own checks belong to the handler tests.
		if routeTable[pattern].group == groupOpen {
			continue
		}
		path := pattern
		if strings.HasSuffix(path, "/") {
			path += "x"
		}
		// Without this check a pattern missing from the wiring would be
		// answered by the "/" catch-all and pass the sweep unseen.
		probe := httptest.NewRequest(http.MethodGet, path, nil)
		if _, matched := mux.Handler(probe); matched != pattern {
			t.Errorf("%s is not registered in the test router, the sweep would reach %q", pattern, matched)
			continue
		}
		recorder := httptest.NewRecorder()
		func() {
			// A handler that panics on the test's empty dependencies was
			// reached, so the gate let the request through.
			defer func() { _ = recover() }()
			router.ServeHTTP(recorder, sessionRequest(t, path, cookies[0], false))
		}()
		assertPassedGate(t, recorder, path)
	}
}
