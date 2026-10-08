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
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func TestEnergyRolesPatchMergesAndRemovesAssignments(t *testing.T) {
	reg := registry.New()
	store := settings.NewStore(t.TempDir())
	if err := store.SaveEnergy(settings.EnergyConfig{Assignments: map[string]energy.Assignment{
		"keep": {Role: energy.RolePV}, "drop": {Role: energy.RoleLoad},
	}}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(reg, nil, store)
	body := `{"assignments":{"drop":null,"add":{"role":"wallbox"},"none":{"role":""}}}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/energy/roles", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("patch status %d: %s", recorder.Code, recorder.Body.String())
	}
	saved, _ := store.LoadEnergy()
	if _, ok := saved.Assignments["drop"]; ok {
		t.Fatal("null must remove the override")
	}
	if saved.Assignments["keep"].Role != energy.RolePV || saved.Assignments["add"].Role != energy.RoleWallbox {
		t.Fatalf("assignments = %+v", saved.Assignments)
	}
	if none, ok := saved.Assignments["none"]; !ok || none.Role != "" {
		t.Fatal(`{"role":""} must be stored as an explicit "no role" override`)
	}
}

func TestEnergyRolesPatchRejectsGroupsUntilPhase4AndUnknownRoles(t *testing.T) {
	router := NewRouter(registry.New(), nil, settings.NewStore(t.TempDir()))
	for _, body := range []string{`{"groups":{}}`, `{"assignments":{"x":{"role":"nonsense"}}}`} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPatch, "/api/v1/energy/roles", strings.NewReader(body))
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"energy_roles_rejected"`) {
			t.Fatalf("%s: status %d: %s", body, recorder.Code, recorder.Body.String())
		}
	}
}

func TestEnergyWritesNeedEditEnergyAndCSRF(t *testing.T) {
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: manager})
	guest := httptest.NewRecorder()
	router.ServeHTTP(guest, httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", nil))
	cookie := guest.Result().Cookies()[0]
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(guest.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ method, path, body string }{
		{http.MethodPatch, "/api/v1/energy/roles", `{"assignments":{}}`},
		{http.MethodPut, "/api/v1/energy/roles", `{"assignments":{}}`},
		{http.MethodPut, "/api/v1/energy/interpretation", `{}`},
		{http.MethodPost, "/api/v1/energy/restore", `{"revision":"x"}`},
	}
	for _, c := range cases {
		without := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		without.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, without)
		if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"csrf_failed"`) {
			t.Fatalf("%s %s without CSRF: %d %s", c.method, c.path, recorder.Code, recorder.Body.String())
		}
		with := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		with.AddCookie(cookie)
		with.Header.Set("X-CSRF-Token", session.CSRFToken)
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, with)
		if recorder.Code == http.StatusForbidden {
			t.Fatalf("%s %s with CSRF still forbidden: %s", c.method, c.path, recorder.Body.String())
		}
	}
	noRole := httptest.NewRequest(http.MethodPatch, "/api/v1/energy/roles", strings.NewReader(`{}`))
	noRole = noRole.WithContext(auth.WithUser(noRole.Context(), auth.User{Username: "viewer"}))
	recorder := httptest.NewRecorder()
	if requireEnergyMutation(recorder, noRole, manager) || recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"energy_forbidden"`) {
		t.Fatalf("a user without edit_energy must get energy_forbidden: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestEnergyRestoreAppliesTheRestoredAssignmentsToTheResolver(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "meter"}, Entity: registry.EntityInfo{UniqueID: "p", Name: "Leistung", StateTopic: "s/p", UnitOfMeasurement: "W"}})
	reg.UpdateState("s/p", []byte("100"), false, time.Now().UTC())
	store := settings.NewStore(t.TempDir())
	router := NewRouter(reg, nil, store)
	put := func(role string) {
		request := httptest.NewRequest(http.MethodPut, "/api/v1/energy/roles", strings.NewReader(`{"assignments":{"p":{"role":"`+role+`"}}}`))
		router.ServeHTTP(httptest.NewRecorder(), request)
	}
	put("wallbox")
	put("heat_pump")
	revisions, _ := store.EnergyRevisions()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/energy/restore", strings.NewReader(`{"revision":"`+revisions[0].Name+`"}`))
	router.ServeHTTP(httptest.NewRecorder(), request)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/energy", nil))
	if !strings.Contains(recorder.Body.String(), `"wallbox":100`) {
		t.Fatalf("restored wallbox role not applied: %s", recorder.Body.String())
	}
}
