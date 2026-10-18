package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// subDeviceTestRouter registers two plugs with a power sensor each, a
// voltage sensor on plug_b and the entity plug_b_power as a load, so the
// energy snapshot lists it.
func subDeviceTestRouter(t *testing.T) (http.Handler, *settings.Store, *registry.Registry) {
	t.Helper()
	store := settings.NewStore(t.TempDir())
	if err := store.SaveEnergy(settings.EnergyConfig{Assignments: map[string]energy.Assignment{
		"plug_b_power": {Role: energy.RoleLoad},
		"plug_c_power": {Role: energy.RoleLoad},
	}}); err != nil {
		t.Fatal(err)
	}
	reg := registry.New()
	for _, discovery := range []registry.Discovery{
		{Device: registry.DeviceInfo{ID: "plug_b", Name: "Plug B"}, Entity: registry.EntityInfo{UniqueID: "plug_b_power", Name: "Leistung", Component: "sensor", UnitOfMeasurement: "W", StateTopic: "state/plug_b_power"}},
		{Device: registry.DeviceInfo{ID: "plug_b", Name: "Plug B"}, Entity: registry.EntityInfo{UniqueID: "plug_b_volt", Name: "Spannung", Component: "sensor", UnitOfMeasurement: "V", StateTopic: "state/plug_b_volt"}},
		{Device: registry.DeviceInfo{ID: "plug_c", Name: "Plug C"}, Entity: registry.EntityInfo{UniqueID: "plug_c_power", Name: "Leistung", Component: "sensor", UnitOfMeasurement: "kW", StateTopic: "state/plug_c_power"}},
	} {
		reg.UpsertEntity(discovery)
	}
	// The energy snapshot only lists entities that have a state.
	reg.UpdateState("state/plug_b_power", []byte("42"), false, time.Now())
	reg.UpdateState("state/plug_c_power", []byte("7"), false, time.Now())
	return NewRouter(reg, config.NewManager(t.TempDir()), store), store, reg
}

func sendSubDevice(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeSubDeviceMap(t *testing.T, recorder *httptest.ResponseRecorder) settings.DeviceMap {
	t.Helper()
	var value settings.DeviceMap
	if err := json.Unmarshal(recorder.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode device map: %v, body %s", err, recorder.Body.String())
	}
	return value
}

// energyLabelOf returns the label the energy snapshot gives an entity.
func energyLabelOf(t *testing.T, router http.Handler, entityID string) string {
	t.Helper()
	recorder := sendSubDevice(t, router, http.MethodGet, "/api/v1/energy", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("energy status %d: %s", recorder.Code, recorder.Body.String())
	}
	var snapshot energy.Snapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	for _, entity := range snapshot.Entities {
		if entity.EntityID == entityID {
			return entity.Label
		}
	}
	t.Fatalf("entity %s missing from the energy snapshot: %s", entityID, recorder.Body.String())
	return ""
}

func hasEdgeProp(value settings.DeviceMap, parent, child string) (settings.DeviceMapEdgeProp, bool) {
	for _, prop := range value.EdgeProps {
		if prop.ParentID == parent && prop.ChildID == child {
			return prop, true
		}
	}
	return settings.DeviceMapEdgeProp{}, false
}

func TestSubDeviceCreateUpdateDelete(t *testing.T) {
	router, _, _ := subDeviceTestRouter(t)
	const create = `{"label":"Waschmaschine","icon":"energy-node:plug-smart","device_id":"plug_b","entity_id":"plug_b_power","x":100,"y":40}`

	created := sendSubDevice(t, router, http.MethodPost, "/api/v1/device/map/sub-devices", create)
	if created.Code != http.StatusOK {
		t.Fatalf("create status %d: %s", created.Code, created.Body.String())
	}
	value := decodeSubDeviceMap(t, created)
	if len(value.SubDevices) != 1 || value.SubDevices[0].ID != "sub:waschmaschine" {
		t.Fatalf("sub_devices = %+v", value.SubDevices)
	}
	node := false
	for _, n := range value.Nodes {
		if n.VirtualID == "sub:waschmaschine" && n.X == 100 && n.Y == 40 {
			node = true
		}
	}
	if !node {
		t.Fatalf("node at 100/40 missing: %+v", value.Nodes)
	}
	if got := energyLabelOf(t, router, "plug_b_power"); got != "Waschmaschine" {
		t.Fatalf("energy label = %q, want Waschmaschine", got)
	}

	second := sendSubDevice(t, router, http.MethodPost, "/api/v1/device/map/sub-devices",
		`{"label":"Waschmaschine","icon":"energy-node:plug-smart","device_id":"plug_c","entity_id":"plug_c_power"}`)
	if second.Code != http.StatusOK {
		t.Fatalf("second create status %d: %s", second.Code, second.Body.String())
	}
	if value := decodeSubDeviceMap(t, second); value.SubDevices[1].ID != "sub:waschmaschine_2" {
		t.Fatalf("second id = %q, want sub:waschmaschine_2", value.SubDevices[1].ID)
	}
	// The second sub-device has no node, so drop it again before the rest.
	sendSubDevice(t, router, http.MethodDelete, "/api/v1/device/map/sub-devices/sub:waschmaschine_2", "")

	updated := sendSubDevice(t, router, http.MethodPut, "/api/v1/device/map/sub-devices/sub:waschmaschine",
		`{"label":"Waschmaschine","icon":"energy-node:plug-smart","device_id":"plug_b","entity_id":"plug_b_power","channel":2}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status %d: %s", updated.Code, updated.Body.String())
	}
	if prop, ok := hasEdgeProp(decodeSubDeviceMap(t, updated), "plug_b", "sub:waschmaschine"); !ok || prop.Type != "energy" || prop.Channel != 2 {
		t.Fatalf("edge_props after channel 2 = %+v", prop)
	}

	rehung := sendSubDevice(t, router, http.MethodPut, "/api/v1/device/map/sub-devices/sub:waschmaschine",
		`{"label":"Waschmaschine","icon":"energy-node:plug-smart","device_id":"plug_c","entity_id":"plug_c_power","channel":2}`)
	if rehung.Code != http.StatusOK {
		t.Fatalf("rehang status %d: %s", rehung.Code, rehung.Body.String())
	}
	rehungValue := decodeSubDeviceMap(t, rehung)
	if _, ok := hasEdgeProp(rehungValue, "plug_b", "sub:waschmaschine"); ok {
		t.Fatalf("old pair survived the rehang: %+v", rehungValue.EdgeProps)
	}
	if _, ok := hasEdgeProp(rehungValue, "plug_c", "sub:waschmaschine"); !ok {
		t.Fatalf("new pair missing after the rehang: %+v", rehungValue.EdgeProps)
	}
	if got := energyLabelOf(t, router, "plug_c_power"); got != "Waschmaschine" {
		t.Fatalf("energy label after the rehang = %q, want Waschmaschine", got)
	}

	deleted := sendSubDevice(t, router, http.MethodDelete, "/api/v1/device/map/sub-devices/sub:waschmaschine", "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status %d: %s", deleted.Code, deleted.Body.String())
	}
	deletedValue := decodeSubDeviceMap(t, deleted)
	if len(deletedValue.SubDevices) != 0 || len(deletedValue.EdgeProps) != 0 {
		t.Fatalf("delete left sub-devices or pairs behind: %+v", deletedValue)
	}
	for _, n := range deletedValue.Nodes {
		if n.VirtualID == "sub:waschmaschine" {
			t.Fatalf("node survived the delete: %+v", deletedValue.Nodes)
		}
	}
	if got := energyLabelOf(t, router, "plug_b_power"); got != "Plug B / Leistung" {
		t.Fatalf("energy label after delete = %q, want Plug B / Leistung", got)
	}
	if got := energyLabelOf(t, router, "plug_c_power"); got != "Plug C / Leistung" {
		t.Fatalf("energy label of plug_c_power after delete = %q, want Plug C / Leistung", got)
	}
}

func TestSubDeviceRejections(t *testing.T) {
	router, _, _ := subDeviceTestRouter(t)
	const base = "/api/v1/device/map/sub-devices"
	expectKey := func(name string, recorder *httptest.ResponseRecorder, status int, key string) {
		t.Helper()
		if recorder.Code != status || !strings.Contains(recorder.Body.String(), `"message_key":"`+key+`"`) {
			t.Fatalf("%s: status %d, want %d with %s: %s", name, recorder.Code, status, key, recorder.Body.String())
		}
	}

	expectKey("voltage entity", sendSubDevice(t, router, http.MethodPost, base,
		`{"label":"Spannung","icon":"energy-node:plug-smart","device_id":"plug_b","entity_id":"plug_b_volt"}`), http.StatusBadRequest, "error.sub_device_entity_invalid")

	if unknown := sendSubDevice(t, router, http.MethodPost, base,
		`{"label":"Geist","icon":"energy-node:plug-smart","device_id":"plug_c","entity_id":"ghost_power"}`); unknown.Code != http.StatusOK {
		t.Fatalf("unknown entity status %d, want 200: %s", unknown.Code, unknown.Body.String())
	}

	expectKey("unknown icon", sendSubDevice(t, router, http.MethodPost, base,
		`{"label":"Lampe","icon":"nope:nothing","device_id":"plug_b","entity_id":"plug_b_power"}`), http.StatusBadRequest, "error.sub_device_icon_invalid")

	expectKey("empty label", sendSubDevice(t, router, http.MethodPost, base,
		`{"label":"  ","icon":"energy-node:plug-smart","device_id":"plug_b","entity_id":"plug_b_power"}`), http.StatusBadRequest, "error.sub_device_label_invalid")

	expectKey("channel 17", sendSubDevice(t, router, http.MethodPost, base,
		`{"label":"Lampe","icon":"energy-node:plug-smart","device_id":"plug_b","entity_id":"plug_b_power","channel":17}`), http.StatusBadRequest, "error.sub_device_channel_invalid")

	if created := sendSubDevice(t, router, http.MethodPost, base,
		`{"label":"Waschmaschine","icon":"energy-node:plug-smart","device_id":"plug_b","entity_id":"plug_b_power"}`); created.Code != http.StatusOK {
		t.Fatalf("create status %d: %s", created.Code, created.Body.String())
	}
	expectKey("second sub-device on the same entity", sendSubDevice(t, router, http.MethodPost, base,
		`{"label":"Trockner","icon":"energy-node:plug-smart","device_id":"plug_b","entity_id":"plug_b_power"}`), http.StatusBadRequest, "error.sub_device_duplicate")

	expectKey("put unknown id", sendSubDevice(t, router, http.MethodPut, base+"/sub:nope",
		`{"label":"Lampe","icon":"energy-node:plug-smart","device_id":"plug_b","entity_id":"plug_c_power"}`), http.StatusNotFound, "error.sub_device_not_found")
	expectKey("delete unknown id", sendSubDevice(t, router, http.MethodDelete, base+"/sub:nope", ""), http.StatusNotFound, "error.sub_device_not_found")
}

func TestSubDeviceWritesNeedEditEnergyAndCSRF(t *testing.T) {
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
		{http.MethodPost, "/api/v1/device/map/sub-devices", `{"label":"Lampe","icon":"energy-node:plug-smart","device_id":"a","entity_id":"b"}`},
		{http.MethodPut, "/api/v1/device/map/sub-devices/sub:lampe", `{"label":"Lampe","icon":"energy-node:plug-smart","device_id":"a","entity_id":"b"}`},
		{http.MethodDelete, "/api/v1/device/map/sub-devices/sub:lampe", ``},
	}
	for _, c := range cases {
		without := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		without.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, without)
		if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"csrf_failed"`) {
			t.Fatalf("%s %s without CSRF: %d %s", c.method, c.path, recorder.Code, recorder.Body.String())
		}
	}
	// Like the relation endpoints, a user without edit_energy gets 403 from
	// the same guard the routes call.
	for _, c := range cases {
		noRole := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		noRole = noRole.WithContext(auth.WithUser(noRole.Context(), auth.User{Username: "viewer"}))
		recorder := httptest.NewRecorder()
		if requireEnergyMutation(recorder, noRole, manager) || recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"energy_forbidden"`) {
			t.Fatalf("%s %s without edit_energy: %d %s", c.method, c.path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestDeviceMapPutKeepsSubDevices(t *testing.T) {
	router, _, _ := subDeviceTestRouter(t)
	created := sendSubDevice(t, router, http.MethodPost, "/api/v1/device/map/sub-devices",
		`{"label":"Waschmaschine","icon":"energy-node:plug-smart","device_id":"plug_b","entity_id":"plug_b_power","channel":2,"x":100,"y":40}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create status %d: %s", created.Code, created.Body.String())
	}

	// A save that sends no sub-devices and no sub node keeps both.
	put := sendSubDevice(t, router, http.MethodPut, "/api/v1/device/map", `{"nodes":[],"sub_devices":[]}`)
	if put.Code != http.StatusOK {
		t.Fatalf("put status %d: %s", put.Code, put.Body.String())
	}
	value := decodeSubDeviceMap(t, put)
	if len(value.SubDevices) != 1 || value.SubDevices[0].ID != "sub:waschmaschine" {
		t.Fatalf("sub_devices after PUT = %+v", value.SubDevices)
	}
	hasNode := false
	for _, n := range value.Nodes {
		if n.VirtualID == "sub:waschmaschine" && n.X == 100 && n.Y == 40 {
			hasNode = true
		}
	}
	if !hasNode {
		t.Fatalf("sub node missing after PUT: %+v", value.Nodes)
	}
	if prop, ok := hasEdgeProp(value, "plug_b", "sub:waschmaschine"); !ok || prop.Channel != 2 {
		t.Fatalf("sub pair missing after PUT: %+v", value.EdgeProps)
	}

	// Forged sub-devices and sub nodes in the request are ignored.
	forged := sendSubDevice(t, router, http.MethodPut, "/api/v1/device/map",
		`{"nodes":[{"virtual_id":"sub:forged","x":1,"y":1}],"sub_devices":[{"id":"sub:forged","label":"Fake","icon":"energy-node:plug-smart","device_id":"plug_c","entity_id":"plug_c_power"}]}`)
	if forged.Code != http.StatusOK {
		t.Fatalf("forged put status %d: %s", forged.Code, forged.Body.String())
	}
	kept := decodeSubDeviceMap(t, forged)
	if len(kept.SubDevices) != 1 || kept.SubDevices[0].ID != "sub:waschmaschine" {
		t.Fatalf("forged sub_devices were stored: %+v", kept.SubDevices)
	}
	for _, n := range kept.Nodes {
		if n.VirtualID == "sub:forged" {
			t.Fatalf("forged sub node was stored: %+v", kept.Nodes)
		}
	}

	// A stale pair that moves the stored sub-device to another device is
	// dropped, and the stored pair stays the only one.
	stale := sendSubDevice(t, router, http.MethodPut, "/api/v1/device/map",
		`{"edge_props":[{"parent_id":"plug_c","child_id":"sub:waschmaschine","type":"energy","channel":5}]}`)
	if stale.Code != http.StatusOK {
		t.Fatalf("stale put status %d: %s", stale.Code, stale.Body.String())
	}
	staleValue := decodeSubDeviceMap(t, stale)
	pairs := 0
	for _, prop := range staleValue.EdgeProps {
		if prop.ParentID == "sub:waschmaschine" || prop.ChildID == "sub:waschmaschine" {
			pairs++
			if prop.ParentID != "plug_b" || prop.Channel != 2 {
				t.Fatalf("stale pair won over the stored one: %+v", prop)
			}
		}
	}
	if pairs != 1 {
		t.Fatalf("sub:waschmaschine has %d pairs, want 1: %+v", pairs, staleValue.EdgeProps)
	}
}

func TestRestoreKeepsCurrentSubDevices(t *testing.T) {
	router, _, _ := subDeviceTestRouter(t)
	// The first save creates the file without sub-devices, the second save
	// keeps that version as a revision.
	if put := sendSubDevice(t, router, http.MethodPut, "/api/v1/device/map", `{"nodes":[]}`); put.Code != http.StatusOK {
		t.Fatalf("put status %d: %s", put.Code, put.Body.String())
	}
	if created := sendSubDevice(t, router, http.MethodPost, "/api/v1/device/map/sub-devices",
		`{"label":"Waschmaschine","icon":"energy-node:plug-smart","device_id":"plug_b","entity_id":"plug_b_power"}`); created.Code != http.StatusOK {
		t.Fatalf("create status %d: %s", created.Code, created.Body.String())
	}
	if got := energyLabelOf(t, router, "plug_b_power"); got != "Waschmaschine" {
		t.Fatalf("label before restore = %q", got)
	}

	revisions := sendSubDevice(t, router, http.MethodGet, "/api/v1/device/map/revisions", "")
	var list []settings.Revision
	if err := json.Unmarshal(revisions.Body.Bytes(), &list); err != nil || len(list) == 0 {
		t.Fatalf("revisions: %v, body %s", err, revisions.Body.String())
	}
	restored := sendSubDevice(t, router, http.MethodPost, "/api/v1/device/map/restore",
		`{"revision":"`+list[0].Name+`"}`)
	if restored.Code != http.StatusOK {
		t.Fatalf("restore status %d: %s", restored.Code, restored.Body.String())
	}
	// The revision has no sub-devices, but the sub-device created after it
	// is the server's current state and stays.
	value := decodeSubDeviceMap(t, restored)
	if len(value.SubDevices) != 1 || value.SubDevices[0].ID != "sub:waschmaschine" {
		t.Fatalf("restore dropped the current sub-devices: %+v", value.SubDevices)
	}
	if _, ok := hasEdgeProp(value, "plug_b", "sub:waschmaschine"); !ok {
		t.Fatalf("restore dropped the sub-device pair: %+v", value.EdgeProps)
	}
	if got := energyLabelOf(t, router, "plug_b_power"); got != "Waschmaschine" {
		t.Fatalf("label after restore = %q, want Waschmaschine", got)
	}
}

func TestSubDeviceEntityMustBelongToDevice(t *testing.T) {
	router, store, _ := subDeviceTestRouter(t)
	// plug_b_power belongs to plug_b, so a sub-device on plug_c must be refused.
	recorder := sendSubDevice(t, router, http.MethodPost, "/api/v1/device/map/sub-devices",
		`{"label":"Waschmaschine","icon":"energy-node:plug-smart","device_id":"plug_c","entity_id":"plug_b_power"}`)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"message_key":"error.sub_device_entity_device_mismatch"`) {
		t.Fatalf("status %d, want 400 with error.sub_device_entity_device_mismatch: %s", recorder.Code, recorder.Body.String())
	}
	stored, err := store.LoadDeviceMap()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.SubDevices) != 0 || len(stored.EdgeProps) != 0 {
		t.Fatalf("rejected sub-device was stored: %+v", stored)
	}
}

func TestConcurrentDeviceMapWritesKeepEveryWrite(t *testing.T) {
	router, store, reg := subDeviceTestRouter(t)
	const count = 40
	for i := 0; i < count; i++ {
		reg.UpsertEntity(registry.Discovery{
			Device: registry.DeviceInfo{ID: fmt.Sprintf("bulk_%02d", i), Name: "Bulk"},
			Entity: registry.EntityInfo{UniqueID: fmt.Sprintf("bulk_%02d_power", i), Name: "Leistung", Component: "sensor", UnitOfMeasurement: "W", StateTopic: fmt.Sprintf("state/bulk_%02d", i)},
		})
	}
	var wg sync.WaitGroup
	failures := make(chan string, 2*count)
	for i := 0; i < count; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"label":"Sub %02d","icon":"energy-node:plug-smart","device_id":"bulk_%02d","entity_id":"bulk_%02d_power"}`, i, i, i)
			if recorder := sendSubDevice(t, router, http.MethodPost, "/api/v1/device/map/sub-devices", body); recorder.Code != http.StatusOK {
				failures <- recorder.Body.String()
			}
		}(i)
		go func() {
			defer wg.Done()
			if recorder := sendSubDevice(t, router, http.MethodPut, "/api/v1/device/map", `{"nodes":[]}`); recorder.Code != http.StatusOK {
				failures <- recorder.Body.String()
			}
		}()
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Errorf("write failed: %s", failure)
	}
	stored, err := store.LoadDeviceMap()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.SubDevices) != count {
		t.Fatalf("stored %d sub-devices, want %d: a concurrent save overwrote another", len(stored.SubDevices), count)
	}
}
