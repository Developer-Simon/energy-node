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

// subDeviceTestRouter registers two plugs with a power sensor each, a
// voltage sensor on plug_b and the entity plug_b_power as a load, so the
// energy snapshot lists it.
func subDeviceTestRouter(t *testing.T) (http.Handler, *settings.Store, *registry.Registry) {
	t.Helper()
	store := settings.NewStore(t.TempDir())
	if err := store.SaveEnergy(settings.EnergyConfig{Assignments: map[string]energy.Assignment{
		"plug_b_power": {Role: energy.RoleLoad},
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
}
