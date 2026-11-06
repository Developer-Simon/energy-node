package energy

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func TestApplyChainChange(t *testing.T) {
	type want struct {
		assignments map[string]Assignment
		roleNeeded  []string
	}
	cases := []struct {
		name        string
		assignments map[string]Assignment
		effective   map[string]Assignment
		prev        *Chain
		next        *Chain
		want        want
	}{
		{
			name:      "create takes the counting point's role",
			effective: map[string]Assignment{"plug": {Role: RoleLoad}, "ac": {Role: RoleBattery}},
			next:      &Chain{Points: []string{"plug", "ac"}, Counting: "plug"},
			want: want{
				assignments: map[string]Assignment{"plug": {Role: RoleLoad}, "ac": {}},
			},
		},
		{
			name:      "create without counting role takes the first role along the points",
			effective: map[string]Assignment{"ac": {Role: RoleBattery}},
			next:      &Chain{Points: []string{"plug", "ac", "dc"}, Counting: "plug"},
			want: want{
				assignments: map[string]Assignment{"plug": {Role: RoleBattery}, "ac": {}, "dc": {}},
			},
		},
		{
			name:        "create without any role stays without role",
			assignments: map[string]Assignment{"plug": {}, "ac": {}},
			effective:   map[string]Assignment{},
			next:        &Chain{Points: []string{"plug", "ac"}, Counting: "plug"},
			want: want{
				assignments: map[string]Assignment{"plug": {}, "ac": {}},
			},
		},
		{
			name: "counting change moves only the role",
			assignments: map[string]Assignment{
				"plug": {Role: RoleBattery, Scale: 1},
				"ac":   {Scale: 2, Invert: true},
			},
			effective: map[string]Assignment{
				"plug": {Role: RoleBattery, Scale: 1},
				"ac":   {Scale: 2, Invert: true},
			},
			prev: &Chain{Points: []string{"plug", "ac"}, Counting: "plug"},
			next: &Chain{Points: []string{"plug", "ac"}, Counting: "ac"},
			want: want{
				assignments: map[string]Assignment{
					"ac":   {Role: RoleBattery, Scale: 2, Invert: true},
					"plug": {Scale: 1},
				},
			},
		},
		{
			name: "removing a reference point leaves it without role and asks for one",
			assignments: map[string]Assignment{
				"plug": {Role: RoleBattery},
				"ac":   {},
				"dc":   {Role: RolePV},
			},
			effective: map[string]Assignment{
				"plug": {Role: RoleBattery},
				"ac":   {},
				"dc":   {Role: RolePV},
			},
			prev: &Chain{Points: []string{"plug", "ac", "dc"}, Counting: "plug"},
			next: &Chain{Points: []string{"plug", "ac"}, Counting: "plug"},
			want: want{
				assignments: map[string]Assignment{
					"plug": {Role: RoleBattery},
					"ac":   {},
					"dc":   {},
				},
				roleNeeded: []string{"dc"},
			},
		},
		{
			name: "removing the counting point hands its role on",
			assignments: map[string]Assignment{
				"plug": {Role: RoleBattery},
				"ac":   {},
				"dc":   {},
			},
			effective: map[string]Assignment{
				"plug": {Role: RoleBattery},
				"ac":   {},
				"dc":   {},
			},
			prev: &Chain{Points: []string{"plug", "ac", "dc"}, Counting: "plug"},
			next: &Chain{Points: []string{"ac", "dc"}, Counting: "ac"},
			want: want{
				assignments: map[string]Assignment{
					"ac":   {Role: RoleBattery},
					"dc":   {},
					"plug": {},
				},
				roleNeeded: []string{"plug"},
			},
		},
		{
			name: "deleting keeps the counting role",
			assignments: map[string]Assignment{
				"plug": {Role: RoleBattery},
				"ac":   {},
			},
			effective: map[string]Assignment{
				"plug": {Role: RoleBattery},
				"ac":   {},
			},
			prev: &Chain{Points: []string{"plug", "ac"}, Counting: "plug"},
			want: want{
				assignments: map[string]Assignment{
					"plug": {Role: RoleBattery},
					"ac":   {},
				},
				roleNeeded: []string{"ac"},
			},
		},
		{
			name:        "an added point with a role does not replace the chain role",
			assignments: map[string]Assignment{"plug": {Role: RoleBattery}, "ac": {}},
			effective: map[string]Assignment{
				"plug": {Role: RoleBattery},
				"ac":   {},
				"neu":  {Role: RoleLoad},
			},
			prev: &Chain{Points: []string{"plug", "ac"}, Counting: "plug"},
			next: &Chain{Points: []string{"plug", "ac", "neu"}, Counting: "plug"},
			want: want{
				assignments: map[string]Assignment{
					"plug": {Role: RoleBattery},
					"ac":   {},
					"neu":  {},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var input map[string]Assignment
			if tc.assignments != nil {
				input = make(map[string]Assignment, len(tc.assignments))
				for k, v := range tc.assignments {
					input[k] = v
				}
			}
			got, roleNeeded := ApplyChainChange(tc.assignments, tc.effective, tc.prev, tc.next)
			if !reflect.DeepEqual(got, tc.want.assignments) {
				t.Fatalf("assignments = %+v, want %+v", got, tc.want.assignments)
			}
			if len(roleNeeded) != len(tc.want.roleNeeded) || (len(roleNeeded) > 0 && !reflect.DeepEqual(roleNeeded, tc.want.roleNeeded)) {
				t.Fatalf("roleNeeded = %v, want %v", roleNeeded, tc.want.roleNeeded)
			}
			if !reflect.DeepEqual(tc.assignments, input) {
				t.Fatalf("input map was modified: %+v, want %+v", tc.assignments, input)
			}
		})
	}
}

// chainWorld places plug on device A and ac and dc on device B. The resolver
// carries the model and the overrides.
func chainWorld(model Model, overrides map[string]Assignment, plug, ac, dc registry.EntityView) ([]registry.DeviceView, *Resolver) {
	devices := []registry.DeviceView{
		{ID: "device-a", Entities: []registry.EntityView{plug}},
		{ID: "device-b", Entities: []registry.EntityView{ac, dc}},
	}
	resolver := NewResolver(overrides)
	resolver.SetModel(model)
	return devices, resolver
}

// liveEntity is a power entity that reported value at time at.
func liveEntity(id, value string, at time.Time) registry.EntityView {
	return registry.EntityView{
		UniqueID: id, ObjectID: id, Name: id, UnitOfMeasurement: "W",
		Value: value, HasValue: true, Available: true, LastSeen: at,
	}
}

func TestChainFlowAndSegments(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	line := func(direction string) Model {
		return Model{Chains: map[string]Chain{"line": {Label: "Linie", Points: []string{"plug", "ac"}, Counting: "plug", Direction: direction}}}
	}

	flowCases := []struct {
		name      string
		direction string
		plug      string
		want      string
	}{
		{"auto positive is down", "", "300", "down"},
		{"auto negative is up", "", "-300", "up"},
		{"reversed positive is up", ChainDirectionReversed, "300", "up"},
		{"fixed down ignores the sign", ChainDirectionFixedDown, "-300", "down"},
		{"fixed up ignores the sign", ChainDirectionFixedUp, "300", "up"},
		{"below the idle threshold is idle", "", "0.5", "idle"},
	}
	for _, tc := range flowCases {
		t.Run(tc.name, func(t *testing.T) {
			devices, resolver := chainWorld(line(tc.direction), nil,
				liveEntity("plug", tc.plug, at), liveEntity("ac", "290", at), liveEntity("dc", "0", at))
			states := evaluateChains(devices, resolver, resolver.Model(), at).states
			if len(states) != 1 || states[0].Flow != tc.want {
				t.Fatalf("states = %+v, want one chain with flow %q", states, tc.want)
			}
		})
	}

	t.Run("down segment", func(t *testing.T) {
		devices, resolver := chainWorld(line(""), nil,
			liveEntity("plug", "312", at), liveEntity("ac", "303", at), liveEntity("dc", "0", at))
		seg := evaluateChains(devices, resolver, resolver.Model(), at).states[0].Segments
		if len(seg) != 1 {
			t.Fatalf("segments = %+v, want one", seg)
		}
		if seg[0].From != "plug" || seg[0].To != "ac" || !seg[0].Available {
			t.Fatalf("segment = %+v, want plug to ac, available", seg[0])
		}
		if math.Abs(seg[0].LossW-9) > 1e-9 {
			t.Fatalf("loss = %v, want 9 W", seg[0].LossW)
		}
		if seg[0].Efficiency == nil || math.Abs(*seg[0].Efficiency-303.0/312.0) > 1e-9 {
			t.Fatalf("efficiency = %v, want 303/312", seg[0].Efficiency)
		}
	})

	t.Run("up segment swaps from and to", func(t *testing.T) {
		devices, resolver := chainWorld(line(ChainDirectionFixedUp), nil,
			liveEntity("plug", "312", at), liveEntity("ac", "303", at), liveEntity("dc", "0", at))
		seg := evaluateChains(devices, resolver, resolver.Model(), at).states[0].Segments
		if seg[0].From != "ac" || seg[0].To != "plug" {
			t.Fatalf("segment = %+v, want ac to plug", seg[0])
		}
		if math.Abs(seg[0].LossW+9) > 1e-9 {
			t.Fatalf("loss = %v, want -9 W", seg[0].LossW)
		}
		if seg[0].Efficiency == nil || math.Abs(*seg[0].Efficiency-312.0/303.0) > 1e-9 {
			t.Fatalf("efficiency = %v, want 312/303", seg[0].Efficiency)
		}
	})

	t.Run("source below the minimum has no efficiency", func(t *testing.T) {
		devices, resolver := chainWorld(line(""), nil,
			liveEntity("plug", "8", at), liveEntity("ac", "6", at), liveEntity("dc", "0", at))
		seg := evaluateChains(devices, resolver, resolver.Model(), at).states[0].Segments
		if math.Abs(seg[0].LossW-2) > 1e-9 {
			t.Fatalf("loss = %v, want 2 W", seg[0].LossW)
		}
		if seg[0].Efficiency != nil {
			t.Fatalf("efficiency = %v, want nil below %v W", *seg[0].Efficiency, chainMinInputW)
		}
	})
}

func TestChainUnknownEntityDoesNotPanic(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	model := Model{Chains: map[string]Chain{
		"line":  {Label: "Linie", Points: []string{"plug", "ghost", "ac"}, Counting: "plug"},
		"other": {Label: "Andere", Points: []string{"ghost", "plug"}, Counting: "ghost"},
	}}
	devices, resolver := chainWorld(model, nil,
		liveEntity("plug", "300", at), liveEntity("ac", "290", at), liveEntity("dc", "0", at))

	states := evaluateChains(devices, resolver, resolver.Model(), at).states
	if len(states) != 2 {
		t.Fatalf("states = %d, want 2", len(states))
	}

	line := states[0]
	if line.Points[1].EntityID != "ghost" || line.Points[1].Freshness != "missing" {
		t.Fatalf("ghost point = %+v, want freshness missing", line.Points[1])
	}
	for i, seg := range line.Segments {
		if seg.Available {
			t.Fatalf("segment %d = %+v, want unavailable next to the unknown point", i, seg)
		}
	}

	other := states[1]
	if other.Points[0].Freshness != "missing" || !other.Points[0].Counting {
		t.Fatalf("counting ghost = %+v, want missing and counting", other.Points[0])
	}
}

func TestSnapshotWithoutChainsOmitsField(t *testing.T) {
	raw, err := json.Marshal(Snapshot{At: time.Unix(0, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"chains"`) {
		t.Fatalf("snapshot without chains serialises the field: %s", raw)
	}

	raw, err = json.Marshal(Snapshot{Chains: []ChainState{{ID: "line"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"chains"`) {
		t.Fatalf("snapshot with chains omits the field: %s", raw)
	}
}

// chainLine is one chain "line" over the given points.
func chainLine(points []string, counting string) Model {
	return Model{Chains: map[string]Chain{"line": {Label: "Linie", Points: points, Counting: counting}}}
}

// roleStateFor returns the role state of the snapshot, the test fails when
// the role is missing.
func roleStateFor(t *testing.T, snapshot Snapshot, role Role) RoleState {
	t.Helper()
	for _, state := range snapshot.Roles {
		if state.Role == role {
			return state
		}
	}
	t.Fatalf("role %s missing in %+v", role, snapshot.Roles)
	return RoleState{}
}

func TestChainReferenceIsNotCounted(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	ac := liveEntity("ac", "290", at)
	ac.Name, ac.ObjectID = "battery ac", "battery_ac"
	devices, resolver := chainWorld(chainLine([]string{"plug", "ac"}, "plug"),
		map[string]Assignment{"plug": {Role: RoleBattery, Scale: 1}},
		liveEntity("plug", "300", at), ac, liveEntity("dc", "0", at))

	snapshot := Aggregate(devices, resolver, at)
	if got := snapshot.Values[RoleBattery]; got != 300 {
		t.Fatalf("battery = %v, want 300 W from the counting point only", got)
	}
	if !reflect.DeepEqual(snapshot.Sources[RoleBattery], []string{"plug"}) {
		t.Fatalf("sources = %v, want only plug", snapshot.Sources[RoleBattery])
	}
	for _, entity := range snapshot.Entities {
		if entity.EntityID == "ac" {
			t.Fatalf("reference point ac appears in entities: %+v", entity)
		}
	}
}

func TestChainSubstituteUsesNearestFreshPoint(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	plug := liveEntity("plug", "300", at)
	plug.Available, plug.HasAvailability = false, true
	devices, resolver := chainWorld(chainLine([]string{"ac", "plug", "dc"}, "plug"),
		map[string]Assignment{"plug": {Role: RoleBattery, Scale: 1}},
		plug, liveEntity("ac", "200", at), liveEntity("dc", "100", at))

	snapshot := Aggregate(devices, resolver, at)
	if got := snapshot.Values[RoleBattery]; got != 200 {
		t.Fatalf("battery = %v, want 200 W from ac (tie goes to the earlier point)", got)
	}
	state := roleStateFor(t, snapshot, RoleBattery)
	if state.Quality != QualitySubstitute || state.Freshness != "fresh" {
		t.Fatalf("quality/freshness = %s/%s, want substitute/fresh", state.Quality, state.Freshness)
	}
	want := []string{entityLabel(devices[1], devices[1].Entities[0])}
	if !reflect.DeepEqual(state.SubstituteFrom, want) {
		t.Fatalf("substitute_from = %v, want %v", state.SubstituteFrom, want)
	}
}

func TestChainSubstituteAppliesScaleAndInvertOfThePoint(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	plug := liveEntity("plug", "300", at)
	plug.Available, plug.HasAvailability = false, true
	ac := liveEntity("ac", "-0.2", at)
	ac.UnitOfMeasurement = "kW"
	devices, resolver := chainWorld(chainLine([]string{"plug", "ac"}, "plug"),
		map[string]Assignment{
			"plug": {Role: RoleBattery, Scale: 1},
			"ac":   {Role: RoleBattery, Scale: 1, Invert: true},
		},
		plug, ac, liveEntity("dc", "0", at))

	snapshot := Aggregate(devices, resolver, at)
	if got := snapshot.Values[RoleBattery]; got != 200 {
		t.Fatalf("battery = %v, want 200 W (-0.2 kW inverted)", got)
	}
}

func TestChainNoFreshPointBehavesLikeToday(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	plug := liveEntity("plug", "300", at)
	plug.Stale = true
	ac := liveEntity("ac", "290", at)
	ac.Stale = true
	devices, resolver := chainWorld(chainLine([]string{"plug", "ac"}, "plug"),
		map[string]Assignment{"plug": {Role: RoleBattery, Scale: 1}},
		plug, ac, liveEntity("dc", "0", at))

	snapshot := Aggregate(devices, resolver, at)
	if got := snapshot.Values[RoleBattery]; got != 300 {
		t.Fatalf("battery = %v, want the counting value 300 W", got)
	}
	state := roleStateFor(t, snapshot, RoleBattery)
	if state.Quality != "stale" || len(state.SubstituteFrom) != 0 {
		t.Fatalf("state = %+v, want stale without substitute", state)
	}
	if len(snapshot.Chains) != 1 || !snapshot.Chains[0].NoCurrentValues {
		t.Fatalf("chains = %+v, want no current values", snapshot.Chains)
	}
}

func TestChainCountingWithoutValueUsesSubstitute(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	plug := liveEntity("plug", "", at)
	plug.HasValue = false
	devices, resolver := chainWorld(chainLine([]string{"plug", "ac"}, "plug"),
		map[string]Assignment{"plug": {Role: RoleBattery, Scale: 1}},
		plug, liveEntity("ac", "200", at), liveEntity("dc", "0", at))

	snapshot := Aggregate(devices, resolver, at)
	if got := snapshot.Values[RoleBattery]; got != 200 {
		t.Fatalf("battery = %v, want 200 W from ac", got)
	}
}

func TestChainOldCountingPointFallsBackAfterAge(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	plug := liveEntity("plug", "300", at.Add(-11*time.Minute))
	ac := liveEntity("ac", "200", at.Add(-time.Minute))
	devices, resolver := chainWorld(chainLine([]string{"plug", "ac"}, "plug"),
		map[string]Assignment{"plug": {Role: RoleBattery, Scale: 1}},
		plug, ac, liveEntity("dc", "0", at))

	snapshot := Aggregate(devices, resolver, at)
	if got := snapshot.Values[RoleBattery]; got != 200 {
		t.Fatalf("battery = %v, want 200 W from ac after the counting point aged out", got)
	}
	if state := roleStateFor(t, snapshot, RoleBattery); state.Quality != QualitySubstitute {
		t.Fatalf("quality = %s, want substitute", state.Quality)
	}
}

func TestChainAllPointsOldKeepsCountingValue(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	plug := liveEntity("plug", "300", at.Add(-11*time.Minute))
	ac := liveEntity("ac", "200", at.Add(-11*time.Minute))
	devices, resolver := chainWorld(chainLine([]string{"plug", "ac"}, "plug"),
		map[string]Assignment{"plug": {Role: RoleBattery, Scale: 1}},
		plug, ac, liveEntity("dc", "0", at))

	snapshot := Aggregate(devices, resolver, at)
	if got := snapshot.Values[RoleBattery]; got != 300 {
		t.Fatalf("battery = %v, want the counting value 300 W", got)
	}
	state := roleStateFor(t, snapshot, RoleBattery)
	if state.Quality != "good" || state.Freshness != "fresh" || len(state.SubstituteFrom) != 0 {
		t.Fatalf("state = %+v, want good, fresh, no substitute", state)
	}
}

func TestChainSubstituteOneWayRoleClampsNegative(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	plug := liveEntity("plug", "300", at)
	plug.Available, plug.HasAvailability = false, true
	devices, resolver := chainWorld(chainLine([]string{"plug", "ac"}, "plug"),
		map[string]Assignment{"plug": {Role: RoleBatteryCharge, Scale: 1}},
		plug, liveEntity("ac", "-200", at), liveEntity("dc", "0", at))

	snapshot := Aggregate(devices, resolver, at)
	if got := snapshot.Values[RoleBatteryCharge]; got != 0 {
		t.Fatalf("battery_charge = %v, want 0 for a negative substitute", got)
	}
	if got := snapshot.BatteryChargePower(); got != 0 {
		t.Fatalf("BatteryChargePower = %v, want 0", got)
	}
}

func TestChainSubstituteBothWayRoleKeepsSign(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	plug := liveEntity("plug", "300", at)
	plug.Available, plug.HasAvailability = false, true
	devices, resolver := chainWorld(chainLine([]string{"plug", "ac"}, "plug"),
		map[string]Assignment{"plug": {Role: RoleBattery, Scale: 1}},
		plug, liveEntity("ac", "-200", at), liveEntity("dc", "0", at))

	snapshot := Aggregate(devices, resolver, at)
	if got := snapshot.Values[RoleBattery]; got != -200 {
		t.Fatalf("battery = %v, want -200 W kept", got)
	}
	if got := snapshot.BatteryDischargePower(); got != 200 {
		t.Fatalf("BatteryDischargePower = %v, want 200", got)
	}
}

func TestChainSubstituteBooksOnCountingDeviceAndGroup(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	plug := liveEntity("plug", "300", at)
	plug.Available, plug.HasAvailability = false, true
	model := chainLine([]string{"plug", "ac"}, "plug")
	model.Categories = map[string]Category{"haus": {Label: "Haus", Base: CategoryConsumer}}
	model.Groups = map[string]Group{"haus": {Label: "Haus", Members: GroupMembers{Devices: []string{"device-a"}}, Role: CustomRole("haus")}}
	devices, resolver := chainWorld(model,
		map[string]Assignment{"plug": {Role: RoleWallbox, Scale: 1}},
		plug, liveEntity("ac", "200", at), liveEntity("dc", "0", at))

	snapshot := Aggregate(devices, resolver, at)
	if got := snapshot.Values[CustomRole("haus")]; got != 200 {
		t.Fatalf("custom:haus = %v, want 200 W", got)
	}
	found := false
	for _, entity := range snapshot.Entities {
		if entity.EntityID == "plug" {
			found = true
			if entity.DeviceID != "device-a" {
				t.Fatalf("device = %s, want device-a", entity.DeviceID)
			}
		}
	}
	if !found {
		t.Fatal("counting entity plug missing from entities")
	}
	for _, group := range snapshot.Groups {
		if group.ID == "haus" && (!group.HasValue || group.Value != 200) {
			t.Fatalf("group haus = %+v, want value 200", group)
		}
	}
}
