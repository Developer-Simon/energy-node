package energy

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/uierror"
)

// CategoryBase is what a user-defined category counts as in the balance.
type CategoryBase string

const (
	CategoryConsumer CategoryBase = "consumer"
	CategoryProducer CategoryBase = "producer"
	CategoryStorage  CategoryBase = "storage"
)

const customRolePrefix = "custom:"

// Category is a user-defined role from energy.json ("categories"). Color is
// one of cat_1..cat_6, Icon a name from the device icon catalogue.
type Category struct {
	Label string       `json:"label"`
	Base  CategoryBase `json:"base"`
	Color string       `json:"color"`
	Icon  string       `json:"icon"`
}

type GroupMembers struct {
	Devices []string `json:"devices"`
	Groups  []string `json:"groups"`
}

// Group is an energy group from energy.json ("groups"). Role is empty or
// custom:<id> of a consumer category: then the members' consumption counts
// once under that category instead of under their own roles.
type Group struct {
	Label   string       `json:"label"`
	Members GroupMembers `json:"members"`
	Role    Role         `json:"role,omitempty"`
}

// Model holds the categories and groups the resolver and the balance work
// with. Validation lives in internal/settings, this type trusts its input
// but never loops forever on a cycle.
type Model struct {
	Categories map[string]Category
	Groups     map[string]Group
}

type GroupState struct {
	ID       string       `json:"id"`
	Label    string       `json:"label"`
	Value    float64      `json:"value"`
	HasValue bool         `json:"has_value"`
	Members  GroupMembers `json:"members"`
	Role     Role         `json:"role,omitempty"`
}

func CustomRole(id string) Role { return Role(customRolePrefix + id) }

func (r Role) CategoryID() (string, bool) {
	if id, ok := strings.CutPrefix(string(r), customRolePrefix); ok && id != "" {
		return id, true
	}
	return "", false
}

func (m Model) clone() Model {
	out := Model{Categories: make(map[string]Category, len(m.Categories)), Groups: make(map[string]Group, len(m.Groups))}
	for id, category := range m.Categories {
		out.Categories[id] = category
	}
	for id, group := range m.Groups {
		group.Members = GroupMembers{
			Devices: append([]string{}, group.Members.Devices...),
			Groups:  append([]string{}, group.Members.Groups...),
		}
		out.Groups[id] = group
	}
	return out
}

func (m Model) base(role Role) (CategoryBase, bool) {
	id, ok := role.CategoryID()
	if !ok {
		return "", false
	}
	category, ok := m.Categories[id]
	return category.Base, ok
}

// parents maps "device:<id>" and "group:<id>" to the group containing them.
func (m Model) parents() map[string]string {
	parents := map[string]string{}
	for id, group := range m.Groups {
		for _, device := range group.Members.Devices {
			parents["device:"+device] = id
		}
		for _, child := range group.Members.Groups {
			parents["group:"+child] = id
		}
	}
	return parents
}

func (m Model) RoleGroupIndex() map[string]Role {
	parents := m.parents()
	index := map[string]Role{}
	for key := range parents {
		device, ok := strings.CutPrefix(key, "device:")
		if !ok {
			continue
		}
		current := key
		for steps := 0; steps <= len(m.Groups); steps++ {
			id, found := parents[current]
			if !found {
				break
			}
			if role := m.Groups[id].Role; role != "" {
				index[device] = role
				break
			}
			current = "group:" + id
		}
	}
	return index
}

func (m Model) Remappable(role Role) bool {
	if role == RoleWallbox || role == RoleHeatPump {
		return true
	}
	base, ok := m.base(role)
	return ok && base == CategoryConsumer
}

// ValidateMemberRoles checks the members of every group with a role: their
// power entities may only carry wallbox, heat_pump, a consumer category or
// no role, since the group books their consumption under its own category.
// It needs the registry to know which entities a device has, so it runs in
// the HTTP layer, not in internal/settings.
func ValidateMemberRoles(devices []registry.DeviceView, assignments map[string]Assignment, m Model) error {
	resolver := NewResolver(assignments)
	resolver.SetModel(m)
	index := m.RoleGroupIndex()
	for _, device := range devices {
		groupRole, inRoleGroup := index[device.ID]
		if !inRoleGroup {
			continue
		}
		for _, entity := range device.Entities {
			assignment, ok := resolver.Resolve(entity)
			if !ok || assignment.Role == "" || m.Remappable(assignment.Role) {
				continue
			}
			categoryID, _ := groupRole.CategoryID()
			return uierror.New("error.energy_roles_rejected.group_member_role",
				fmt.Sprintf("energy: %s in a group with role %s has role %s", entity.UniqueID, groupRole, assignment.Role),
				map[string]any{"entity": entity.UniqueID, "role": string(assignment.Role), "category": m.Categories[categoryID].Label})
		}
	}
	return nil
}

func Toward(role Role, value float64, m Model) (float64, bool) {
	switch role {
	case RolePV, RoleBatteryDischarge, RoleGridExport:
		return -math.Abs(value), true
	case RoleBattery, RoleGrid:
		return value, true
	case RoleBatteryCharge, RoleGridImport, RoleLoad, RoleWallbox, RoleHeatPump:
		return math.Abs(value), true
	}
	if base, ok := m.base(role); ok {
		switch base {
		case CategoryProducer:
			return -math.Abs(value), true
		case CategoryStorage:
			return value, true
		default:
			return math.Abs(value), true
		}
	}
	return 0, false
}

func (m Model) GroupStates(towardByDevice map[string]float64) []GroupState {
	copied := m.clone()
	var sum func(id string, visited map[string]bool) (float64, bool)
	sum = func(id string, visited map[string]bool) (float64, bool) {
		if visited[id] {
			return 0, false
		}
		visited[id] = true
		group := m.Groups[id]
		total, has := 0.0, false
		for _, device := range group.Members.Devices {
			if value, ok := towardByDevice[device]; ok {
				total += value
				has = true
			}
		}
		for _, child := range group.Members.Groups {
			if value, ok := sum(child, visited); ok {
				total += value
				has = true
			}
		}
		return total, has
	}
	states := make([]GroupState, 0, len(m.Groups))
	for id, group := range m.Groups {
		value, has := sum(id, map[string]bool{})
		states = append(states, GroupState{ID: id, Label: group.Label, Value: value, HasValue: has, Members: copied.Groups[id].Members, Role: group.Role})
	}
	sort.Slice(states, func(i, j int) bool { return states[i].ID < states[j].ID })
	return states
}
