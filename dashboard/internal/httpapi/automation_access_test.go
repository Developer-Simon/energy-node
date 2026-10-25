package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func TestOnlyEnabledDiffers(t *testing.T) {
	stored := `{"version":1,"settings":{"a":1},"rules":[{"id":"r1","enabled":true,"name":"x"}],"scenarios":[{"id":"s1","enabled":false}]}`
	cases := map[string]bool{
		`{"version":1,"settings":{"a":1},"rules":[{"id":"r1","enabled":false,"name":"x"}],"scenarios":[{"id":"s1","enabled":true}]}`:  true,
		`{"version":1,"settings":{"a":2},"rules":[{"id":"r1","enabled":false,"name":"x"}],"scenarios":[{"id":"s1","enabled":false}]}`: false,
		`{"version":1,"settings":{"a":1},"rules":[{"id":"r1","enabled":true,"name":"y"}],"scenarios":[{"id":"s1","enabled":false}]}`:  false,
		`{"version":1,"settings":{"a":1},"rules":[],"scenarios":[{"id":"s1","enabled":false}]}`:                                       false,
		`not json`: false,
	}
	for body, want := range cases {
		if got := onlyEnabledDiffers([]byte(stored), []byte(body)); got != want {
			t.Errorf("%s: got %v, want %v", body, got, want)
		}
	}
}

// automationAccessRouter serves the authenticated router on a config directory
// that holds one automation_rules document with one rule and one scenario.
func automationAccessRouter(t *testing.T) (http.Handler, *auth.Manager) {
	t.Helper()
	configDir := t.TempDir()
	document := `{"version":1,"settings":{},"rules":[{"id":"r1","enabled":true,"name":"x"}],"scenarios":[{"id":"s1","enabled":false}]}`
	if err := os.WriteFile(filepath.Join(configDir, "automation_rules.json"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "automation_rules.schema.json"), []byte(`{"type":"object"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(configDir), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: manager})
	return router, manager
}

func TestOperateAutomationsMayOnlyToggle(t *testing.T) {
	router, manager := automationAccessRouter(t)
	operator, csrf := loginWithRoles(t, router, manager, "toggler", []string{auth.RoleViewAutomations, auth.RoleOperateAutomations})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, accountRequest(http.MethodGet, "/api/v1/configurations/automation_rules", "", operator, ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status %d: %s", recorder.Code, recorder.Body.String())
	}

	toggled := `{"version":1,"settings":{},"rules":[{"id":"r1","enabled":false,"name":"x"}],"scenarios":[{"id":"s1","enabled":true}]}`
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, accountRequest(http.MethodPut, "/api/v1/configurations/automation_rules", toggled, operator, csrf))
	if recorder.Code != http.StatusOK {
		t.Fatalf("toggle status %d: %s", recorder.Code, recorder.Body.String())
	}

	renamed := `{"version":1,"settings":{},"rules":[{"id":"r1","enabled":false,"name":"y"}],"scenarios":[{"id":"s1","enabled":true}]}`
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, accountRequest(http.MethodPut, "/api/v1/configurations/automation_rules", renamed, operator, csrf))
	if recorder.Code != http.StatusForbidden || errorCode(recorder) != "automations_forbidden" {
		t.Fatalf("rename status %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestAutomationTestStillNeedsAutomations(t *testing.T) {
	router, manager := automationAccessRouter(t)
	operator, csrf := loginWithRoles(t, router, manager, "tester", []string{auth.RoleViewAutomations, auth.RoleOperateAutomations})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, accountRequest(http.MethodPost, "/api/v1/automations/test", `{}`, operator, csrf))
	if recorder.Code != http.StatusForbidden || errorCode(recorder) != "automations_forbidden" {
		t.Fatalf("test status %d: %s", recorder.Code, recorder.Body.String())
	}
}
