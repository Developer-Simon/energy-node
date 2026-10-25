package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
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

func TestViewGateResolvesPathDependentPermission(t *testing.T) {
	resolve := func(r *http.Request) string {
		if r.URL.Path == "/api/v1/configurations/automation_rules" {
			return auth.RoleViewAutomations
		}
		return auth.RoleViewConfig
	}
	gate := viewGate(routeAccess{group: groupPage, resolve: resolve}, okHandler())

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
