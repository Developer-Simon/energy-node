package energy

import "testing"

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
