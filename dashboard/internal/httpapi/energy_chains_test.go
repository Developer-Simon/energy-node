package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energydiscovery"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/webui"
)

var chainIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// addChainEntity registers one entity of a device with a state topic, so the
// energy snapshot can read its value.
func addChainEntity(reg *registry.Registry, device, unique, name, unit string, value string) {
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: device, Name: device},
		Entity: registry.EntityInfo{UniqueID: unique, Name: name, StateTopic: "s/" + unique, UnitOfMeasurement: unit},
	})
	if value != "" {
		reg.UpdateState("s/"+unique, []byte(value), false, time.Now().UTC())
	}
}

// chainTestSetup builds the usual test registry: plug is a plain power point,
// ac is detected as battery by its name.
func chainTestSetup(t *testing.T) (*registry.Registry, *settings.Store, http.Handler) {
	t.Helper()
	reg := registry.New()
	addChainEntity(reg, "steckdose", "plug", "Balkon Plug", "W", "100")
	addChainEntity(reg, "speicher", "ac", "Speicher AC", "W", "40")
	addChainEntity(reg, "steckdose", "other", "Heizung", "W", "5")
	store := settings.NewStore(t.TempDir())
	return reg, store, NewRouter(reg, nil, store)
}

func doChainRequest(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// chainIDFromBody returns the only chain key of a saved EnergyConfig response
// that does not equal one of the known IDs.
func chainIDFromBody(t *testing.T, body string, known ...string) string {
	t.Helper()
	var value settings.EnergyConfig
	if err := json.Unmarshal([]byte(body), &value); err != nil {
		t.Fatalf("response is no EnergyConfig: %v: %s", err, body)
	}
	for id := range value.Chains {
		found := false
		for _, k := range known {
			if k == id {
				found = true
			}
		}
		if !found {
			return id
		}
	}
	t.Fatalf("no new chain in %s", body)
	return ""
}

func TestChainCreateMovesRolesAndAppliesResolver(t *testing.T) {
	reg := registry.New()
	addChainEntity(reg, "steckdose", "plug", "Balkon Plug", "W", "100")
	addChainEntity(reg, "speicher", "ac", "Speicher AC", "W", "40")
	store := settings.NewStore(t.TempDir())
	if err := store.SaveEnergy(settings.EnergyConfig{Assignments: map[string]energy.Assignment{"plug": {Role: energy.RoleBattery}}}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(reg, nil, store)

	recorder := doChainRequest(router, http.MethodPost, "/api/v1/energy/chains", `{"label":"Balkon Speicher","points":["plug","ac"]}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("create status %d: %s", recorder.Code, recorder.Body.String())
	}
	id := chainIDFromBody(t, recorder.Body.String())
	saved, _ := store.LoadEnergy()
	if saved.Chains[id].Counting != "plug" {
		t.Fatalf("counting defaults to the first point: %+v", saved.Chains[id])
	}
	if saved.Assignments["ac"].Role != "" {
		t.Fatalf("ac must lose its heuristic role through an empty override: %+v", saved.Assignments["ac"])
	}
	values := httptest.NewRecorder()
	router.ServeHTTP(values, httptest.NewRequest(http.MethodGet, "/api/v1/energy", nil))
	var snapshot struct {
		Values map[string]float64 `json:"values"`
	}
	if err := json.Unmarshal(values.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Values["battery"] != 100 {
		t.Fatalf("only the counting point may count: battery = %v", snapshot.Values["battery"])
	}
}

func TestChainCreateTakesDetectedRole(t *testing.T) {
	_, store, router := chainTestSetup(t)
	recorder := doChainRequest(router, http.MethodPost, "/api/v1/energy/chains", `{"label":"Akku","points":["plug","ac"]}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("create status %d: %s", recorder.Code, recorder.Body.String())
	}
	saved, _ := store.LoadEnergy()
	if saved.Assignments["plug"].Role != energy.RoleBattery {
		t.Fatalf("the detected battery role of ac must move to the counting point plug: %+v", saved.Assignments)
	}
}

func TestChainIDsAreUniqueSlugs(t *testing.T) {
	reg := registry.New()
	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		addChainEntity(reg, "dev", id, "Leistung "+id, "W", "")
	}
	store := settings.NewStore(t.TempDir())
	router := NewRouter(reg, nil, store)

	first := doChainRequest(router, http.MethodPost, "/api/v1/energy/chains", `{"label":"Balkon Speicher","points":["a","b"]}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first status %d: %s", first.Code, first.Body.String())
	}
	firstID := chainIDFromBody(t, first.Body.String())
	second := doChainRequest(router, http.MethodPost, "/api/v1/energy/chains", `{"label":"Balkon Speicher","points":["c","d"]}`)
	secondID := chainIDFromBody(t, second.Body.String(), firstID)
	odd := doChainRequest(router, http.MethodPost, "/api/v1/energy/chains", `{"label":"!!!","points":["e","f"]}`)
	oddID := chainIDFromBody(t, odd.Body.String(), firstID, secondID)

	if firstID != "balkon_speicher" || secondID != "balkon_speicher_2" || oddID != "messkette" {
		t.Fatalf("ids = %q %q %q", firstID, secondID, oddID)
	}
	if !chainIDPattern.MatchString(firstID) || !chainIDPattern.MatchString(secondID) || !chainIDPattern.MatchString(oddID) {
		t.Fatal("chain ids must match ^[a-z0-9_]+$")
	}
}

func TestChainRejectsNonPowerAndOwnDevice(t *testing.T) {
	reg := registry.New()
	addChainEntity(reg, "steckdose", "plug", "Balkon Plug", "W", "")
	addChainEntity(reg, "steckdose", "prozent", "Akkustand", "%", "")
	addChainEntity(reg, energydiscovery.DeviceIdentifier, "own", "Eigene Leistung", "W", "")
	store := settings.NewStore(t.TempDir())
	router := NewRouter(reg, nil, store)

	for _, points := range []string{`["plug","prozent"]`, `["plug","own"]`} {
		recorder := doChainRequest(router, http.MethodPost, "/api/v1/energy/chains", `{"label":"Test","points":`+points+`}`)
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"energy_roles_rejected"`) || !strings.Contains(recorder.Body.String(), "chain_point_not_power") {
			t.Fatalf("%s: status %d: %s", points, recorder.Code, recorder.Body.String())
		}
	}
	if saved, _ := store.LoadEnergy(); len(saved.Chains) != 0 {
		t.Fatalf("a rejected chain must not be saved: %+v", saved.Chains)
	}
}

func TestChainUpdateCountingAndDelete(t *testing.T) {
	_, store, router := chainTestSetup(t)
	created := doChainRequest(router, http.MethodPost, "/api/v1/energy/chains", `{"label":"Balkon","points":["plug","ac"],"counting":"plug"}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create status %d: %s", created.Code, created.Body.String())
	}
	id := chainIDFromBody(t, created.Body.String())

	updated := doChainRequest(router, http.MethodPut, "/api/v1/energy/chains/"+id, `{"label":"Balkon","points":["plug","ac"],"counting":"ac"}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status %d: %s", updated.Code, updated.Body.String())
	}
	saved, _ := store.LoadEnergy()
	if saved.Chains[id].Counting != "ac" || saved.Assignments["ac"].Role != energy.RoleBattery || saved.Assignments["plug"].Role != "" {
		t.Fatalf("the role must move to the new counting point: chains %+v assignments %+v", saved.Chains, saved.Assignments)
	}

	deleted := doChainRequest(router, http.MethodDelete, "/api/v1/energy/chains/"+id, "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status %d: %s", deleted.Code, deleted.Body.String())
	}
	saved, _ = store.LoadEnergy()
	if len(saved.Chains) != 0 {
		t.Fatalf("chain must be gone: %+v", saved.Chains)
	}
	if strings.Join(saved.RoleNeeded, ",") != "plug" {
		t.Fatalf("reference points must land in role_needed: %v", saved.RoleNeeded)
	}
	if saved.Assignments["ac"].Role != energy.RoleBattery {
		t.Fatalf("the counting point keeps its role: %+v", saved.Assignments["ac"])
	}

	missing := doChainRequest(router, http.MethodPut, "/api/v1/energy/chains/gibt_es_nicht", `{"label":"X","points":["plug","ac"]}`)
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "chain_not_found") || !strings.Contains(missing.Body.String(), "error.chain_not_found") {
		t.Fatalf("unknown chain: status %d: %s", missing.Code, missing.Body.String())
	}
}

func TestChainRequiresEditEnergyAndCSRF(t *testing.T) {
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetGuestRoles("admin", []string{auth.RoleViewEnergy, auth.RoleEditEnergy}); err != nil {
		t.Fatal(err)
	}
	store := settings.NewStore(t.TempDir())
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), store, nil, nil, nil, nil, nil, RouterDependencies{Auth: manager})
	guest := httptest.NewRecorder()
	router.ServeHTTP(guest, httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", nil))
	cookie := guest.Result().Cookies()[0]
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(guest.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}

	without := httptest.NewRequest(http.MethodPost, "/api/v1/energy/chains", strings.NewReader(`{"label":"X","points":["a","b"]}`))
	without.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, without)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"csrf_failed"`) {
		t.Fatalf("POST without CSRF: %d %s", recorder.Code, recorder.Body.String())
	}

	// A user with only edit_device_map must not write chains.
	editOnly := httptest.NewRequest(http.MethodPost, "/api/v1/energy/chains", strings.NewReader(`{"label":"X","points":["a","b"]}`))
	editOnly = editOnly.WithContext(auth.WithUser(editOnly.Context(), auth.User{Username: "tester", Roles: []string{auth.RoleViewEnergy, auth.RoleEditDeviceMap}}))
	recorder = httptest.NewRecorder()
	chainsHandler := handleEnergyChains(store, energy.NewResolver(nil), manager, registry.New())
	chainsHandler(recorder, editOnly)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"energy_forbidden"`) {
		t.Fatalf("edit_device_map alone: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestRolesPutCannotTouchChains(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	if err := store.SaveEnergy(settings.EnergyConfig{
		Assignments: map[string]energy.Assignment{"plug": {Role: energy.RoleBattery}},
		Chains:      map[string]energy.Chain{"balkon": {Label: "Balkon", Points: []string{"plug", "ac"}, Counting: "plug"}},
		RoleNeeded:  []string{"x"},
	}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(registry.New(), nil, store)

	for _, body := range []string{
		`{"assignments":{},"chains":{},"role_needed":[]}`,
		`{"assignments":{},"chains":{"fremd":{"label":"Fremd","points":["x","y"],"counting":"x"}},"role_needed":[]}`,
	} {
		if recorder := doChainRequest(router, http.MethodPut, "/api/v1/energy/roles", body); recorder.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", body, recorder.Code, recorder.Body.String())
		}
		saved, _ := store.LoadEnergy()
		if len(saved.Chains) != 1 || saved.Chains["balkon"].Counting != "plug" || strings.Join(saved.RoleNeeded, ",") != "x" {
			t.Fatalf("%s: chains or role_needed changed: %+v %v", body, saved.Chains, saved.RoleNeeded)
		}
	}

	if recorder := doChainRequest(router, http.MethodPut, "/api/v1/energy/roles", `{"assignments":{"ac":{"role":"battery"}}}`); recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	saved, _ := store.LoadEnergy()
	if saved.Assignments["ac"].Role != "" {
		t.Fatalf("a reference point must not carry a role: %+v", saved.Assignments["ac"])
	}
}

func TestRolesPatchDismissesRoleNeeded(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	if err := store.SaveEnergy(settings.EnergyConfig{RoleNeeded: []string{"ac", "plug"}}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(registry.New(), nil, store)
	if recorder := doChainRequest(router, http.MethodPatch, "/api/v1/energy/roles", `{"role_needed":{"ac":false}}`); recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	saved, _ := store.LoadEnergy()
	if strings.Join(saved.RoleNeeded, ",") != "plug" {
		t.Fatalf("role_needed = %v", saved.RoleNeeded)
	}
}

func TestChainGroupRoleConflictIsRejected(t *testing.T) {
	reg := registry.New()
	addChainEntity(reg, "wallbox", "wb", "Speicher WB", "W", "")
	addChainEntity(reg, "steckdose", "plug", "Balkon Plug", "W", "")
	icon := webui.DeviceIconCatalogue()[0].Name
	store := settings.NewStore(t.TempDir())
	if err := store.SaveEnergy(settings.EnergyConfig{
		Assignments: map[string]energy.Assignment{},
		Categories:  map[string]energy.Category{"werkstatt": {Label: "Werkstatt", Base: energy.CategoryConsumer, Icon: icon}},
		Groups: map[string]energy.Group{"garage": {
			Label:   "Garage",
			Role:    energy.CustomRole("werkstatt"),
			Members: energy.GroupMembers{Devices: []string{"wallbox"}, Groups: []string{}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(reg, nil, store)

	recorder := doChainRequest(router, http.MethodPost, "/api/v1/energy/chains", `{"label":"Wallbox Kette","points":["plug","wb"],"counting":"wb"}`)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "group_member_role") {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if saved, _ := store.LoadEnergy(); len(saved.Chains) != 0 || saved.Assignments["wb"].Role != "" {
		t.Fatalf("a rejected chain must not be saved: chains %+v assignments %+v", saved.Chains, saved.Assignments)
	}
}

func TestPutChainKeepsAdjacentTolerances(t *testing.T) {
	_, store, router := chainTestSetup(t)
	created := doChainRequest(router, http.MethodPost, "/api/v1/energy/chains", `{"label":"Balkon","points":["plug","ac","other"],"counting":"plug"}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create status %d: %s", created.Code, created.Body.String())
	}
	id := chainIDFromBody(t, created.Body.String())

	saved, err := store.LoadEnergy()
	if err != nil {
		t.Fatal(err)
	}
	ten, five := 10.0, 5.0
	chain := saved.Chains[id]
	chain.Tolerances = []energy.ChainTolerance{
		{A: "plug", B: "ac", W: &ten},
		{A: "ac", B: "other", Pct: &five},
	}
	saved.Chains[id] = chain
	if err := store.SaveEnergy(saved); err != nil {
		t.Fatal(err)
	}

	// The point "other" moves between plug and ac: plug-ac is no longer adjacent.
	updated := doChainRequest(router, http.MethodPut, "/api/v1/energy/chains/"+id, `{"label":"Balkon","points":["plug","other","ac"]}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status %d: %s", updated.Code, updated.Body.String())
	}
	var value settings.EnergyConfig
	if err := json.Unmarshal(updated.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	got := value.Chains[id].Tolerances
	if len(got) != 1 || got[0].A != "ac" || got[0].B != "other" {
		t.Fatalf("tolerances = %+v, want only the adjacent ac-other", got)
	}
}
