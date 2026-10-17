package energy

import (
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/uierror"
)

func testModel() Model {
	return Model{
		Categories: map[string]Category{
			"werkstatt": {Label: "Werkstatt", Base: CategoryConsumer, Color: "cat_1", Icon: "mdi:home"},
			"bkw":       {Label: "BKW", Base: CategoryProducer, Color: "cat_2", Icon: "mdi:solar-panel"},
		},
		Groups: map[string]Group{
			"garage": {Label: "Garage", Members: GroupMembers{Devices: []string{"wallbox"}, Groups: []string{"bank"}}, Role: CustomRole("werkstatt")},
			"bank":   {Label: "Bank", Members: GroupMembers{Devices: []string{"saege"}}},
			"frei":   {Label: "Frei", Members: GroupMembers{Devices: []string{"pv"}}},
		},
	}
}

func TestRoleGroupIndexFindsTheNearestGroupWithARole(t *testing.T) {
	index := testModel().RoleGroupIndex()
	if index["wallbox"] != "custom:werkstatt" || index["saege"] != "custom:werkstatt" {
		t.Fatalf("index = %v", index)
	}
	if _, ok := index["pv"]; ok {
		t.Fatal("a group without a role must not remap its members")
	}
}

func TestRoleGroupIndexAndGroupStatesSurviveACycle(t *testing.T) {
	m := Model{Groups: map[string]Group{
		"a": {Members: GroupMembers{Groups: []string{"b"}, Devices: []string{"d"}}},
		"b": {Members: GroupMembers{Groups: []string{"a"}}},
	}}
	_ = m.RoleGroupIndex()
	_ = m.GroupStates(map[string]float64{"d": 1})
}

func TestTowardFollowsTheRoleOrTheCategoryBase(t *testing.T) {
	m := testModel()
	cases := []struct {
		role  Role
		value float64
		want  float64
	}{
		{RolePV, 500, -500}, {RoleWallbox, -300, 300}, {RoleBattery, -200, -200}, {RoleGrid, 100, 100},
		{CustomRole("werkstatt"), 350, 350}, {CustomRole("bkw"), 600, -600},
	}
	for _, c := range cases {
		got, ok := Toward(c.role, c.value, m)
		if !ok || got != c.want {
			t.Errorf("Toward(%s, %v) = %v, %v, want %v", c.role, c.value, got, ok, c.want)
		}
	}
	if _, ok := Toward(RoleBatterySoC, 50, m); ok {
		t.Error("battery_soc is no power role")
	}
}

func TestGroupStatesSumMembersRecursively(t *testing.T) {
	states := testModel().GroupStates(map[string]float64{"wallbox": 1100, "saege": 350, "pv": -600})
	byID := map[string]GroupState{}
	for _, state := range states {
		byID[state.ID] = state
	}
	if byID["garage"].Value != 1450 || !byID["garage"].HasValue {
		t.Fatalf("garage = %+v", byID["garage"])
	}
	if byID["frei"].Value != -600 {
		t.Fatalf("frei = %+v", byID["frei"])
	}
	if states[0].ID != "bank" {
		t.Fatalf("states not sorted by id: %v", states)
	}
}

func TestResolverModelIsACopy(t *testing.T) {
	resolver := NewResolver(nil)
	m := testModel()
	resolver.SetModel(m)
	m.Groups["garage"].Members.Devices[0] = "changed"
	if resolver.Model().Groups["garage"].Members.Devices[0] != "wallbox" {
		t.Fatal("SetModel must deep-copy")
	}
}

func TestValidateMemberRolesRejectsALoadInAGroupWithARole(t *testing.T) {
	m := testModel()
	devices := []registry.DeviceView{powerDevice("wallbox", "wallbox_power", "1100"), powerDevice("saege", "saege_power", "300")}
	valid := map[string]Assignment{"wallbox_power": {Role: RoleWallbox}, "saege_power": {Role: CustomRole("werkstatt")}}
	if err := ValidateMemberRoles(devices, valid, m); err != nil {
		t.Fatalf("valid members rejected: %v", err)
	}
	bad := map[string]Assignment{"wallbox_power": {Role: RoleWallbox}, "saege_power": {Role: RoleLoad}}
	typed, isUI := uierror.From(ValidateMemberRoles(devices, bad, m))
	if !isUI || typed.Key != "error.energy_roles_rejected.group_member_role" || typed.Params["entity"] != "saege_power" {
		t.Fatalf("err = %+v", typed)
	}
}

func switchEntity(id string) registry.EntityView {
	return registry.EntityView{UniqueID: id, Name: id, Component: "switch", Commandable: true}
}

func TestValidateSwitchRolesChecksChannelsAndComponentAgainstTheRegistry(t *testing.T) {
	devices := []registry.DeviceView{
		{ID: "pm", Name: "pm", Entities: []registry.EntityView{
			switchEntity("s1"), switchEntity("s2"),
			{UniqueID: "p", Name: "Leistung", Component: "sensor", UnitOfMeasurement: "W"},
		}},
		{ID: "other", Name: "other", Entities: []registry.EntityView{switchEntity("o1")}},
	}
	cases := []struct {
		name        string
		assignments map[string]Assignment
		wantKey     string
		wantParams  map[string]any
	}{
		{
			name:        "two channels on one device",
			assignments: map[string]Assignment{"s1": {Role: RoleSwitch, Channel: 1}, "s2": {Role: RoleSwitch, Channel: 2}},
		},
		{
			name:        "zero counts as channel one",
			assignments: map[string]Assignment{"s1": {Role: RoleSwitch, Channel: 0}, "s2": {Role: RoleSwitch, Channel: 1}},
			wantKey:     "error.energy_roles_rejected.switch_channel_duplicate",
			wantParams:  map[string]any{"device": "pm", "channel": 1},
		},
		{
			name:        "same channel on two devices is fine",
			assignments: map[string]Assignment{"s1": {Role: RoleSwitch, Channel: 3}, "o1": {Role: RoleSwitch, Channel: 3}},
		},
		{
			name:        "a sensor cannot carry the switch role",
			assignments: map[string]Assignment{"p": {Role: RoleSwitch, Channel: 1}},
			wantKey:     "error.energy_roles_rejected.switch_role_invalid",
			wantParams:  map[string]any{"entity": "p"},
		},
		{
			name:        "an entity missing from the registry is ignored",
			assignments: map[string]Assignment{"gone": {Role: RoleSwitch, Channel: 1}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateSwitchRoles(devices, c.assignments)
			if c.wantKey == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			typed, isUI := uierror.From(err)
			if !isUI || typed.Key != c.wantKey {
				t.Fatalf("err = %v, want key %s", err, c.wantKey)
			}
			for key, want := range c.wantParams {
				if typed.Params[key] != want {
					t.Fatalf("param %s = %v, want %v", key, typed.Params[key], want)
				}
			}
		})
	}
}
