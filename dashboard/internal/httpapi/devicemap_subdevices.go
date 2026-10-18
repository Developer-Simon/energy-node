package httpapi

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/Developer-Simon/energy-node-dashboard/internal/deviceiconname"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/uierror"
	"github.com/Developer-Simon/energy-node-dashboard/internal/webui"
)

// subDeviceBody is the body of POST and PUT on a sub-device. X and Y place
// its node on the map, and POST adds the node only when both are sent.
type subDeviceBody struct {
	Label    string   `json:"label"`
	Icon     string   `json:"icon"`
	DeviceID string   `json:"device_id"`
	EntityID string   `json:"entity_id"`
	Channel  int      `json:"channel"`
	X        *float64 `json:"x"`
	Y        *float64 `json:"y"`
}

// maxSubDeviceChannel is the highest switch channel a sub-device can use.
const maxSubDeviceChannel = 16

// deviceMapWriteMu serialises every load-modify-save of the device map, so
// two writers cannot both load before either saves and lose one write. A
// handler takes it once around its whole read-modify-write. The helpers it
// calls must not take it again, because sync.Mutex does not nest.
var deviceMapWriteMu sync.Mutex

var (
	subDeviceSlugSeparator = regexp.MustCompile(`[^a-z0-9]+`)
	subDeviceTranslit      = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss")
)

// subDeviceSlug is the Go port of DeviceMapModel.slugId in devicemap-model.js:
// lower case, umlauts transliterated, every run of other characters becomes
// one underscore. A label without letters or digits falls back to
// "untergeraet". A taken ID gets the suffix _2, _3 and so on.
func subDeviceSlug(label string, taken map[string]bool) string {
	base := subDeviceSlugSeparator.ReplaceAllString(subDeviceTranslit.Replace(strings.ToLower(label)), "_")
	base = strings.Trim(base, "_")
	if base == "" {
		base = "untergeraet"
	}
	id := settings.SubDevicePrefix + base
	for n := 2; taken[id]; n++ {
		id = fmt.Sprintf("%s%s_%d", settings.SubDevicePrefix, base, n)
	}
	return id
}

// isSubDevicePowerUnit mirrors isPowerUnit in the energy package, which is
// unexported. Only power values can be measured by a sub-device.
func isSubDevicePowerUnit(unit string) bool {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "w", "watt", "watts", "kw", "kilowatt", "kilowatts":
		return true
	default:
		return false
	}
}

func readSubDeviceRequest(w http.ResponseWriter, r *http.Request) (subDeviceBody, bool) {
	var request subDeviceBody
	if err := decodeBody(r, &request); err != nil {
		writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
		return request, false
	}
	request.Label = strings.TrimSpace(request.Label)
	request.Icon = deviceiconname.Canonical(request.Icon)
	if request.DeviceID == "" || request.EntityID == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "device_id and entity_id are required")
		return request, false
	}
	return request, true
}

// checkSubDeviceRequest runs the checks that need the registry and the other
// sub-devices. selfID is the sub-device being changed, empty on create.
func checkSubDeviceRequest(reg *registry.Registry, deviceMap settings.DeviceMap, selfID string, request subDeviceBody) error {
	if request.Channel < 0 || request.Channel > maxSubDeviceChannel {
		return uierror.New("error.sub_device_channel_invalid", fmt.Sprintf("device-map sub-device channel %d is outside 1 to %d", request.Channel, maxSubDeviceChannel), nil)
	}
	known := map[string]bool{}
	for _, icon := range webui.DeviceIconCatalogue() {
		known[icon.Name] = true
	}
	if !known[request.Icon] {
		return uierror.New("error.sub_device_icon_invalid", fmt.Sprintf("device-map sub-device icon %q is unknown", request.Icon), nil)
	}
	for _, device := range reg.Snapshot() {
		for _, entity := range device.Entities {
			if entity.UniqueID != request.EntityID {
				continue
			}
			if device.ID != request.DeviceID {
				return uierror.New("error.sub_device_entity_device_mismatch", fmt.Sprintf("device-map sub-device entity %q does not belong to device %q", request.EntityID, request.DeviceID), nil)
			}
			if !isSubDevicePowerUnit(entity.UnitOfMeasurement) {
				return uierror.New("error.sub_device_entity_invalid", fmt.Sprintf("device-map sub-device entity %q has no power unit", request.EntityID), nil)
			}
		}
	}
	for _, sub := range deviceMap.SubDevices {
		if sub.ID != selfID && sub.EntityID == request.EntityID {
			return uierror.New("error.sub_device_duplicate", fmt.Sprintf("entity %q already has a sub-device", request.EntityID), nil)
		}
	}
	return nil
}

// setSubDeviceEdge replaces the one connected pair of a sub-device. A
// channel of 0 leaves the channel out of the pair.
func setSubDeviceEdge(deviceMap *settings.DeviceMap, id string, request subDeviceBody) {
	props := make([]settings.DeviceMapEdgeProp, 0, len(deviceMap.EdgeProps)+1)
	for _, prop := range deviceMap.EdgeProps {
		if prop.ParentID == id || prop.ChildID == id {
			continue
		}
		props = append(props, prop)
	}
	prop := settings.DeviceMapEdgeProp{ParentID: request.DeviceID, ChildID: id, Type: "energy"}
	if request.Channel > 0 {
		prop.Channel = request.Channel
	}
	deviceMap.EdgeProps = append(props, prop)
}

// applySubDeviceLabels hands the sub-device labels of a saved device map to
// the energy resolver, so the energy snapshot names the entity after its
// sub-device.
func applySubDeviceLabels(resolver *energy.Resolver, m settings.DeviceMap) {
	resolver.SetLabels(m.SubDeviceLabels())
}

// keepSubDevices protects the sub-devices from a whole-map save. The stored
// sub-devices replace the request's, and every stored sub-device node or pair
// that the request does not send is added back. Sub-devices are written only
// by their own endpoints.
func keepSubDevices(value *settings.DeviceMap, stored settings.DeviceMap) {
	value.SubDevices = stored.SubDevices
	nodes := map[string]bool{}
	for _, node := range value.Nodes {
		nodes[node.VirtualID] = true
	}
	for _, node := range stored.Nodes {
		if strings.HasPrefix(node.VirtualID, settings.SubDevicePrefix) && !nodes[node.VirtualID] {
			value.Nodes = append(value.Nodes, node)
		}
	}
	// The stored pairs of the sub-devices win. A request pair that names a
	// stored sub-device is dropped, so a stale pair cannot add a second one.
	stored4sub := map[string]bool{}
	for _, sub := range stored.SubDevices {
		stored4sub[sub.ID] = true
	}
	props := make([]settings.DeviceMapEdgeProp, 0, len(value.EdgeProps))
	for _, prop := range value.EdgeProps {
		if stored4sub[prop.ParentID] || stored4sub[prop.ChildID] {
			continue
		}
		props = append(props, prop)
	}
	value.EdgeProps = props
	for _, prop := range stored.EdgeProps {
		if !strings.HasPrefix(prop.ParentID, settings.SubDevicePrefix) && !strings.HasPrefix(prop.ChildID, settings.SubDevicePrefix) {
			continue
		}
		if !hasEdgePair(value.EdgeProps, prop.ParentID, prop.ChildID) {
			value.EdgeProps = append(value.EdgeProps, prop)
		}
	}
}

// hasEdgePair reports whether props connect a and b, in either order.
func hasEdgePair(props []settings.DeviceMapEdgeProp, a, b string) bool {
	for _, prop := range props {
		if (prop.ParentID == a && prop.ChildID == b) || (prop.ParentID == b && prop.ChildID == a) {
			return true
		}
	}
	return false
}

// saveSubDeviceMap stores the changed map, applies the labels and answers
// with the stored device map.
func saveSubDeviceMap(store *settings.Store, resolver *energy.Resolver, w http.ResponseWriter, deviceMap settings.DeviceMap) {
	if err := store.SaveDeviceMap(deviceMap); err != nil {
		writeErrorDetail(w, http.StatusBadRequest, "device_map_rejected", err)
		return
	}
	stored, err := store.LoadDeviceMap()
	if err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, "device_map_invalid", err)
		return
	}
	applySubDeviceLabels(resolver, stored)
	writeJSON(w, stored)
}

func handleCreateSubDevice(store *settings.Store, reg *registry.Registry, resolver *energy.Resolver, w http.ResponseWriter, r *http.Request) {
	deviceMapWriteMu.Lock()
	defer deviceMapWriteMu.Unlock()
	request, ok := readSubDeviceRequest(w, r)
	if !ok {
		return
	}
	deviceMap, err := store.LoadDeviceMap()
	if err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, "device_map_invalid", err)
		return
	}
	if err := checkSubDeviceRequest(reg, deviceMap, "", request); err != nil {
		writeErrorDetail(w, http.StatusBadRequest, "device_map_rejected", err)
		return
	}
	taken := map[string]bool{}
	for _, sub := range deviceMap.SubDevices {
		taken[sub.ID] = true
	}
	for _, node := range deviceMap.Nodes {
		taken[node.VirtualID] = true
	}
	id := subDeviceSlug(request.Label, taken)
	deviceMap.SubDevices = append(deviceMap.SubDevices, settings.DeviceMapSubDevice{
		ID: id, Label: request.Label, Icon: request.Icon, DeviceID: request.DeviceID, EntityID: request.EntityID,
	})
	if request.X != nil && request.Y != nil {
		deviceMap.Nodes = append(deviceMap.Nodes, settings.DeviceMapNode{VirtualID: id, X: *request.X, Y: *request.Y})
	}
	setSubDeviceEdge(&deviceMap, id, request)
	saveSubDeviceMap(store, resolver, w, deviceMap)
}

func handleUpdateSubDevice(store *settings.Store, reg *registry.Registry, resolver *energy.Resolver, w http.ResponseWriter, r *http.Request, id string) {
	deviceMapWriteMu.Lock()
	defer deviceMapWriteMu.Unlock()
	request, ok := readSubDeviceRequest(w, r)
	if !ok {
		return
	}
	deviceMap, err := store.LoadDeviceMap()
	if err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, "device_map_invalid", err)
		return
	}
	index := -1
	for i, sub := range deviceMap.SubDevices {
		if sub.ID == id {
			index = i
		}
	}
	if index < 0 {
		writeErrorDetail(w, http.StatusNotFound, "sub_device_not_found", uierror.New("error.sub_device_not_found", fmt.Sprintf("device-map sub-device %q does not exist", id), nil))
		return
	}
	if err := checkSubDeviceRequest(reg, deviceMap, id, request); err != nil {
		writeErrorDetail(w, http.StatusBadRequest, "device_map_rejected", err)
		return
	}
	deviceMap.SubDevices[index] = settings.DeviceMapSubDevice{
		ID: id, Label: request.Label, Icon: request.Icon, DeviceID: request.DeviceID, EntityID: request.EntityID,
	}
	setSubDeviceEdge(&deviceMap, id, request)
	saveSubDeviceMap(store, resolver, w, deviceMap)
}

func handleDeleteSubDevice(store *settings.Store, resolver *energy.Resolver, w http.ResponseWriter, id string) {
	deviceMapWriteMu.Lock()
	defer deviceMapWriteMu.Unlock()
	deviceMap, err := store.LoadDeviceMap()
	if err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, "device_map_invalid", err)
		return
	}
	if _, found := deviceMap.SubDevice(id); !found {
		writeErrorDetail(w, http.StatusNotFound, "sub_device_not_found", uierror.New("error.sub_device_not_found", fmt.Sprintf("device-map sub-device %q does not exist", id), nil))
		return
	}
	subs := make([]settings.DeviceMapSubDevice, 0, len(deviceMap.SubDevices))
	for _, sub := range deviceMap.SubDevices {
		if sub.ID != id {
			subs = append(subs, sub)
		}
	}
	deviceMap.SubDevices = subs
	nodes := make([]settings.DeviceMapNode, 0, len(deviceMap.Nodes))
	for _, node := range deviceMap.Nodes {
		if node.VirtualID != id {
			nodes = append(nodes, node)
		}
	}
	deviceMap.Nodes = nodes
	props := make([]settings.DeviceMapEdgeProp, 0, len(deviceMap.EdgeProps))
	for _, prop := range deviceMap.EdgeProps {
		if prop.ParentID != id && prop.ChildID != id {
			props = append(props, prop)
		}
	}
	deviceMap.EdgeProps = props
	saveSubDeviceMap(store, resolver, w, deviceMap)
}
