package httpapi

import (
	"bytes"
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

func TestEnergyRolesPatchHidesAndShows(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	if err := store.SaveEnergy(settings.EnergyConfig{Hidden: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(registry.New(), nil, store)
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/energy/roles", strings.NewReader(`{"hidden":{"b":false,"c":true}}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("patch status %d: %s", recorder.Code, recorder.Body.String())
	}
	saved, _ := store.LoadEnergy()
	if strings.Join(saved.Hidden, ",") != "a,c" {
		t.Fatalf("hidden = %v", saved.Hidden)
	}
}

func TestEnergyRolesPatchAssigningARoleUnhides(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	if err := store.SaveEnergy(settings.EnergyConfig{Hidden: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(registry.New(), nil, store)
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/energy/roles", strings.NewReader(`{"assignments":{"a":{"role":"pv"}}}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), request)
	saved, _ := store.LoadEnergy()
	if len(saved.Hidden) != 0 {
		t.Fatalf("an assigned entry must not stay hidden: %v", saved.Hidden)
	}
}

func TestEnergyRolesPutWithoutHiddenKeepsIt(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	if err := store.SaveEnergy(settings.EnergyConfig{Hidden: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(registry.New(), nil, store)
	put := func(body string) {
		request := httptest.NewRequest(http.MethodPut, "/api/v1/energy/roles", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("put status %d: %s", recorder.Code, recorder.Body.String())
		}
	}
	put(`{"assignments":{}}`)
	if saved, _ := store.LoadEnergy(); strings.Join(saved.Hidden, ",") != "a" {
		t.Fatalf("a PUT without hidden must keep the set: %v", saved.Hidden)
	}
	put(`{"assignments":{},"hidden":[]}`)
	if saved, _ := store.LoadEnergy(); len(saved.Hidden) != 0 {
		t.Fatalf("a PUT with an empty hidden must clear the set: %v", saved.Hidden)
	}
}

func TestEnergyRolesPatchRejectsUnknownRoles(t *testing.T) {
	router := NewRouter(registry.New(), nil, settings.NewStore(t.TempDir()))
	for _, body := range []string{`{"assignments":{"x":{"role":"nonsense"}}}`} {
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
	if err := manager.SetGuestRoles("admin", []string{auth.RoleViewEnergy, auth.RoleEditEnergy}); err != nil {
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

func TestDeviceMapWritesNeedDeviceMapRolesAndCSRF(t *testing.T) {
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetGuestRoles("admin", []string{auth.RoleViewDeviceMap, auth.RoleEditDeviceMap, auth.RoleEditDeviceMapView}); err != nil {
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
		{http.MethodPut, "/api/v1/device/map", `{"version":3,"nodes":[]}`},
		{http.MethodPost, "/api/v1/device/map/relations", `{"child_id":"a","parent_id":"b"}`},
		{http.MethodDelete, "/api/v1/device/map/relations/x", ``},
		{http.MethodPost, "/api/v1/device/map/relations/x/reverse", ``},
		{http.MethodPost, "/api/v1/device/map/restore", `{"revision":"x"}`},
		{http.MethodPut, "/api/v1/device/map/view", `{}`},
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
	noRole := httptest.NewRequest(http.MethodPut, "/api/v1/device/map", strings.NewReader(`{"version":3,"nodes":[]}`))
	noRole = noRole.WithContext(auth.WithUser(noRole.Context(), auth.User{Username: "viewer"}))
	recorder := httptest.NewRecorder()
	if requireEnergyMutation(recorder, noRole, manager) || recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"energy_forbidden"`) {
		t.Fatalf("a user without edit_energy must get energy_forbidden: %d %s", recorder.Code, recorder.Body.String())
	}

	store := settings.NewStore(t.TempDir())
	if err := store.SaveDeviceMap(settings.DeviceMap{Nodes: []settings.DeviceMapNode{{DeviceID: "first", X: 1, Y: 1}}}); err != nil {
		t.Fatal(err)
	}
	stored, err := store.LoadDeviceMap()
	if err != nil {
		t.Fatal(err)
	}
	moved := stored
	moved.Nodes = append([]settings.DeviceMapNode(nil), stored.Nodes...)
	moved.Nodes[0].X = 9
	movedBody, _ := json.Marshal(moved)
	flipped := stored
	flipped.View.ShowGrid = !stored.View.ShowGrid
	flippedBody, _ := json.Marshal(flipped)
	viewBody, _ := json.Marshal(stored.View)
	viewBody = []byte(strings.Replace(string(viewBody), `"show_grid":false`, `"show_grid":true`, 1))

	// Direct calls: the user comes from the context, the guest session supplies
	// the CSRF token, so only the role check decides the outcome.
	mapHandler := handleDeviceMap(store, manager)
	subHandler := handleDeviceMapSub(store, registry.New(), config.NewManager(t.TempDir()), nil, manager)
	call := func(handler http.HandlerFunc, method, path string, body []byte, roles ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.AddCookie(cookie)
		req.Header.Set("X-CSRF-Token", session.CSRFToken)
		req = req.WithContext(auth.WithUser(req.Context(), auth.User{Username: "tester", Roles: roles}))
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}
	viewOnly := []string{auth.RoleViewDeviceMap, auth.RoleEditDeviceMapView}
	structureOnly := []string{auth.RoleEditDeviceMap}

	if rec := call(mapHandler, http.MethodPut, "/api/v1/device/map", movedBody, viewOnly...); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"device_map_forbidden"`) {
		t.Fatalf("moving a node needs edit_device_map: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(mapHandler, http.MethodPut, "/api/v1/device/map", flippedBody, structureOnly...); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"device_map_view_forbidden"`) {
		t.Fatalf("changing the view needs edit_device_map_view: %d %s", rec.Code, rec.Body.String())
	}
	afterReject, err := store.LoadDeviceMap()
	if err != nil {
		t.Fatal(err)
	}
	if afterReject.View.ShowGrid != stored.View.ShowGrid {
		t.Fatalf("a rejected view change must not be saved: show_grid %v", afterReject.View.ShowGrid)
	}
	if rec := call(subHandler, http.MethodPut, "/api/v1/device/map/view", []byte(`{}`), structureOnly...); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"device_map_view_forbidden"`) {
		t.Fatalf("view endpoint needs edit_device_map_view: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(subHandler, http.MethodPut, "/api/v1/device/map/view", viewBody, viewOnly...); rec.Code != http.StatusOK {
		t.Fatalf("view endpoint with edit_device_map_view: %d %s", rec.Code, rec.Body.String())
	}
	saved, err := store.LoadDeviceMap()
	if err != nil {
		t.Fatal(err)
	}
	if !saved.View.ShowGrid || len(saved.Nodes) != 1 || saved.Nodes[0].X != 1 {
		t.Fatalf("view endpoint must only replace the view: %+v", saved)
	}
}

func TestEnergyRolesRejectDuplicateSwitchChannels(t *testing.T) {
	reg := registry.New()
	for _, entity := range []string{"relay_a", "relay_b"} {
		reg.UpsertEntity(registry.Discovery{
			Device: registry.DeviceInfo{ID: "pm", Name: "Steckdosenleiste"},
			Entity: registry.EntityInfo{UniqueID: entity, Name: entity, Component: "switch", StateTopic: "state/" + entity, CommandTopic: "cmd/" + entity},
		})
	}
	store := settings.NewStore(t.TempDir())
	router := NewRouter(reg, nil, store)
	body := `{"assignments":{"relay_a":{"role":"switch","channel":2},"relay_b":{"role":"switch","channel":2}}}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/energy/roles", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "switch_channel_duplicate") {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if saved, _ := store.LoadEnergy(); len(saved.Assignments) != 0 {
		t.Fatalf("a rejected write must not be saved: %+v", saved.Assignments)
	}
}
