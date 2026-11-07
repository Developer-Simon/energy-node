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
			t.Fatalf("efficiency = %v, want nil below %v W", *seg[0].Efficiency, ChainMinInputW)
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

// flowOfOneWayMeter evaluates the chain plug, ac with plug as counting point
// and returns the flow and the segments of the chain.
func flowOfOneWayMeter(t *testing.T, direction string, plug, ac registry.EntityView) ChainState {
	t.Helper()
	at := time.Unix(1000, 0).UTC()
	model := Model{Chains: map[string]Chain{"line": {Label: "Linie", Points: []string{"plug", "ac"}, Counting: "plug", Direction: direction}}}
	devices, resolver := chainWorld(model, nil, plug, ac, liveEntity("dc", "0", at))
	states := evaluateChains(devices, resolver, resolver.Model(), at).states
	if len(states) != 1 {
		t.Fatalf("states = %+v, want one chain", states)
	}
	return states[0]
}

func TestChainFlowFollowsNegativeReferenceOfOneWayMeter(t *testing.T) {
	at := time.Unix(1000, 0).UTC()

	auto := flowOfOneWayMeter(t, "", liveEntity("plug", "290", at), liveEntity("ac", "-300", at))
	if auto.Flow != "up" {
		t.Fatalf("auto flow = %q, want up from the negative reference", auto.Flow)
	}
	if len(auto.Segments) != 1 || auto.Segments[0].From != "ac" || auto.Segments[0].To != "plug" {
		t.Fatalf("segments = %+v, want one segment ac to plug", auto.Segments)
	}
	if !auto.Segments[0].Available || math.Abs(auto.Segments[0].LossW-10) > 1e-9 {
		t.Fatalf("segment = %+v, want available with loss 10 W", auto.Segments[0])
	}

	reversed := flowOfOneWayMeter(t, ChainDirectionReversed, liveEntity("plug", "290", at), liveEntity("ac", "-300", at))
	if reversed.Flow != "down" {
		t.Fatalf("reversed flow = %q, want down", reversed.Flow)
	}

	if got := flowOfOneWayMeter(t, ChainDirectionFixedDown, liveEntity("plug", "290", at), liveEntity("ac", "-300", at)).Flow; got != "down" {
		t.Fatalf("fixed_down flow = %q, want down", got)
	}
	if got := flowOfOneWayMeter(t, ChainDirectionFixedUp, liveEntity("plug", "290", at), liveEntity("ac", "-300", at)).Flow; got != "up" {
		t.Fatalf("fixed_up flow = %q, want up", got)
	}
}

func TestChainFlowIgnoresSmallNegativeReference(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	state := flowOfOneWayMeter(t, "", liveEntity("plug", "290", at), liveEntity("ac", "-5", at))
	if state.Flow != "down" {
		t.Fatalf("flow = %q, want down, -5 W is noise", state.Flow)
	}
}

func TestChainFlowIgnoresStaleNegativeReference(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	ac := liveEntity("ac", "-300", at)
	ac.Stale = true
	state := flowOfOneWayMeter(t, "", liveEntity("plug", "290", at), ac)
	if state.Flow != "down" {
		t.Fatalf("flow = %q, want down, a stale reference does not count", state.Flow)
	}
}

func TestChainFlowNegativeCountingStaysUp(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	state := flowOfOneWayMeter(t, "", liveEntity("plug", "-290", at), liveEntity("ac", "300", at))
	if state.Flow != "up" {
		t.Fatalf("flow = %q, want up, the sign of the counting point wins", state.Flow)
	}
}

func TestModelIsStorageRole(t *testing.T) {
	model := Model{Categories: map[string]Category{
		"akku": {Label: "Akku", Base: CategoryStorage},
		"herd": {Label: "Herd", Base: CategoryConsumer},
	}}
	for _, role := range []Role{RoleBattery, RoleBatteryCharge, RoleBatteryDischarge, CustomRole("akku")} {
		if !model.IsStorageRole(role) {
			t.Fatalf("IsStorageRole(%s) = false, want true", role)
		}
	}
	for _, role := range []Role{CustomRole("herd"), RoleLoad} {
		if model.IsStorageRole(role) {
			t.Fatalf("IsStorageRole(%s) = true, want false", role)
		}
	}
}

func floatPtr(v float64) *float64 { return &v }

func TestNormalBandLearned(t *testing.T) {
	cases := []struct {
		name    string
		insight ChainSegmentInsight
		source  float64
		want    ChainNormalRange
	}{
		{"loss and deviation", ChainSegmentInsight{Learned: true, LossW: 10, DeviationW: 1}, 300, ChainNormalRange{LowW: 1, HighW: 19}},
		{"deviation widens the band", ChainSegmentInsight{Learned: true, LossW: 10, DeviationW: 4}, 300, ChainNormalRange{LowW: -2, HighW: 22}},
		{"5 W floor, not half the loss", ChainSegmentInsight{Learned: true, LossW: 40, DeviationW: 0}, 100, ChainNormalRange{LowW: 35, HighW: 45}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalBand(tc.insight, tc.source, nil); got != tc.want {
				t.Fatalf("NormalBand() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestNormalBandTolerance(t *testing.T) {
	insight := ChainSegmentInsight{Learned: true, LossW: 10}
	cases := []struct {
		name      string
		tolerance ChainTolerance
		want      ChainNormalRange
	}{
		{"absolute watts", ChainTolerance{W: floatPtr(20)}, ChainNormalRange{LowW: -10, HighW: 30, Manual: true}},
		{"larger of watts and percent", ChainTolerance{W: floatPtr(5), Pct: floatPtr(10)}, ChainNormalRange{LowW: -20, HighW: 40, Manual: true}},
		{"percent of the source", ChainTolerance{Pct: floatPtr(2)}, ChainNormalRange{LowW: 4, HighW: 16, Manual: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalBand(insight, 300, &tc.tolerance); got != tc.want {
				t.Fatalf("NormalBand() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestKeepAdjacentTolerances(t *testing.T) {
	tolerances := []ChainTolerance{
		{A: "a", B: "b", W: floatPtr(10)},
		{A: "b", B: "c", W: floatPtr(10)},
	}
	cases := []struct {
		name   string
		points []string
		want   []ChainTolerance
	}{
		{"inserted point breaks the b-c pair", []string{"a", "b", "x", "c"}, tolerances[:1]},
		{"order of the pair does not matter", []string{"b", "a", "c"}, tolerances[:1]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := KeepAdjacentTolerances(tolerances, tc.points)
			if len(got) != len(tc.want) || got[0].A != tc.want[0].A || got[0].B != tc.want[0].B {
				t.Fatalf("KeepAdjacentTolerances() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// chainSwitch is a commandable switch entity with the given state payload.
func chainSwitch(id, value string) registry.EntityView {
	return registry.EntityView{
		UniqueID: id, ObjectID: id, Name: id, Component: "switch", Commandable: true,
		PayloadOn: "ON", PayloadOff: "OFF", Value: value, HasValue: true, Available: true,
	}
}

func TestChainPointSwitchOff(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	model := Model{Chains: map[string]Chain{"line": {Label: "Linie", Points: []string{"plug", "ac"}, Counting: "plug"}}}
	off := chainSwitch("sw", "OFF")
	category := chainSwitch("sw", "OFF")
	category.EntityCategory = "config"

	cases := []struct {
		name      string
		switches  []registry.EntityView
		overrides map[string]Assignment
		want      bool
	}{
		{"one switch that is off", []registry.EntityView{off}, nil, true},
		{"one switch that is on", []registry.EntityView{chainSwitch("sw", "ON")}, nil, false},
		{"unknown state", []registry.EntityView{chainSwitch("sw", "unavailable")}, nil, false},
		{"two switches", []registry.EntityView{off, chainSwitch("sw2", "OFF")}, nil, false},
		{"switch overridden to an empty role does not count", []registry.EntityView{off, chainSwitch("sw2", "ON")}, map[string]Assignment{"sw2": {}}, true},
		{"switch overridden to another role does not count", []registry.EntityView{off}, map[string]Assignment{"sw": {Role: RoleLoad}}, false},
		{"entity category does not count", []registry.EntityView{category}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			devices, resolver := chainWorld(model, tc.overrides, liveEntity("plug", "300", at), liveEntity("ac", "290", at), liveEntity("dc", "0", at))
			devices[0].Entities = append(devices[0].Entities, tc.switches...)
			states := evaluateChains(devices, resolver, resolver.Model(), at).states
			if got := states[0].Points[0].SwitchOff; got != tc.want {
				t.Fatalf("SwitchOff = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChainPointCarriesScaleAndInvert(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	model := Model{Chains: map[string]Chain{"line": {Label: "Linie", Points: []string{"plug", "ac"}, Counting: "plug"}}}
	devices, resolver := chainWorld(model, map[string]Assignment{"plug": {Role: RoleLoad, Scale: 2, Invert: true}},
		liveEntity("plug", "300", at), liveEntity("ac", "290", at), liveEntity("dc", "0", at))
	points := evaluateChains(devices, resolver, resolver.Model(), at).states[0].Points

	if points[0].Scale != 2 || !points[0].Invert || points[0].Value != -600 {
		t.Fatalf("plug point = %+v, want scale 2, invert, value -600", points[0])
	}
	if points[1].Scale != 1 || points[1].Invert {
		t.Fatalf("ac point = %+v, want scale 1 without invert", points[1])
	}
}

func TestChainStateWithoutInsightStaysEmpty(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	model := Model{Chains: map[string]Chain{"line": {Label: "Linie", Points: []string{"plug", "ac"}, Counting: "plug"}}}
	devices, resolver := chainWorld(model, nil, liveEntity("plug", "300", at), liveEntity("ac", "290", at), liveEntity("dc", "0", at))
	state := evaluateChains(devices, resolver, resolver.Model(), at).states[0]

	if state.Learning || state.Findings != nil || state.Tolerances != nil || state.Segments[0].Normal != nil {
		t.Fatalf("state without insight = %+v, want no learning, findings, tolerances or normal band", state)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"learning"`, `"findings"`, `"tolerances"`, `"normal"`, `"switch_off"`} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("JSON contains %s: %s", key, raw)
		}
	}
}

// fakeChainInsight answers every question with fixed values.
type fakeChainInsight struct {
	segment     ChainSegmentInsight
	pairLearned bool
	findings    []ChainFinding
}

func (f *fakeChainInsight) Segment(source, target string, countingW float64) ChainSegmentInsight {
	return f.segment
}
func (f *fakeChainInsight) PairLearned(a, b string) bool { return f.pairLearned }
func (f *fakeChainInsight) Findings(chainID string) []ChainFinding {
	return f.findings
}
func (f *fakeChainInsight) Curve(points []string) ChainCurve { return ChainCurve{} }

func TestChainStateReadsInsight(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	model := Model{Chains: map[string]Chain{"line": {Label: "Linie", Points: []string{"plug", "ac"}, Counting: "plug"}}}
	learned := ChainSegmentInsight{Learned: true, LossW: 10, DeviationW: 1}
	finding := ChainFinding{Kind: FindingDrift, ChainID: "line", ChainLabel: "Linie"}

	cases := []struct {
		name        string
		plug        registry.EntityView
		insight     fakeChainInsight
		wantNormal  *ChainNormalRange
		wantLearn   bool
		wantFinding int
	}{
		{
			name:       "fresh counting point and learned class give a band",
			plug:       liveEntity("plug", "300", at),
			insight:    fakeChainInsight{segment: learned, pairLearned: true, findings: []ChainFinding{finding}},
			wantNormal: &ChainNormalRange{LowW: 1, HighW: 19},

			wantFinding: 1,
		},
		{
			name:    "source below the minimum gives no band",
			plug:    liveEntity("plug", "8", at),
			insight: fakeChainInsight{segment: learned, pairLearned: true},
		},
		{
			name:    "idle chain gives no band",
			plug:    liveEntity("plug", "0.5", at),
			insight: fakeChainInsight{segment: learned, pairLearned: true},
		},
		{
			name: "stale counting point gives no band",
			plug: func() registry.EntityView {
				e := liveEntity("plug", "300", at.Add(-time.Hour))
				return e
			}(),
			insight: fakeChainInsight{segment: learned, pairLearned: true},
		},
		{
			name:       "unlearned pair sets learning, the band follows the class",
			plug:       liveEntity("plug", "300", at),
			insight:    fakeChainInsight{segment: learned, pairLearned: false},
			wantNormal: &ChainNormalRange{LowW: 1, HighW: 19},
			wantLearn:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			devices, resolver := chainWorld(model, nil, tc.plug, liveEntity("ac", "290", at), liveEntity("dc", "0", at))
			insight := tc.insight
			resolver.SetChainInsight(&insight)
			state := evaluateChains(devices, resolver, resolver.Model(), at).states[0]

			got := state.Segments[0].Normal
			switch {
			case tc.wantNormal == nil && got != nil:
				t.Fatalf("Normal = %+v, want nil", got)
			case tc.wantNormal != nil && (got == nil || *got != *tc.wantNormal):
				t.Fatalf("Normal = %+v, want %+v", got, tc.wantNormal)
			}
			if state.Learning != tc.wantLearn {
				t.Fatalf("Learning = %v, want %v", state.Learning, tc.wantLearn)
			}
			if len(state.Findings) != tc.wantFinding {
				t.Fatalf("Findings = %+v, want %d", state.Findings, tc.wantFinding)
			}
		})
	}
}

func TestResolverKeepsInsightAcrossSetOverrides(t *testing.T) {
	resolver := NewResolver(nil)
	insight := &fakeChainInsight{}
	resolver.SetChainInsight(insight)
	resolver.SetOverrides(map[string]Assignment{"x": {Role: RoleLoad}})

	if got := resolver.ChainInsight(); got != insight {
		t.Fatalf("ChainInsight() = %v after SetOverrides, want the insight set before", got)
	}
}

// pairChainInsight knows the learned loss of some pairs, given as
// "source->target", and records the counting power of every question.
type pairChainInsight struct {
	losses   map[string]float64
	countedW []float64
}

func (p *pairChainInsight) Segment(source, target string, countingW float64) ChainSegmentInsight {
	p.countedW = append(p.countedW, countingW)
	loss, ok := p.losses[source+"->"+target]
	return ChainSegmentInsight{Learned: ok, LossW: loss}
}
func (p *pairChainInsight) PairLearned(a, b string) bool { return true }
func (p *pairChainInsight) Findings(chainID string) []ChainFinding {
	return nil
}
func (p *pairChainInsight) Curve(points []string) ChainCurve { return ChainCurve{} }

func TestSubstituteAddsLossWhenCountingIsUpstream(t *testing.T) {
	insight := &pairChainInsight{losses: map[string]float64{"plug->ac": 9}}
	points := []string{"plug", "ac", "dc"}
	// ac is downstream of the counting point plug: counting = source + loss.
	if got := correctedSubstitute(insight, points, 1, 0, "down", 300); got != 309 {
		t.Fatalf("substitute = %v, want 309", got)
	}
}

func TestSubstituteSubtractsLossWhenCountingIsDownstream(t *testing.T) {
	insight := &pairChainInsight{losses: map[string]float64{"ac->plug": 9}}
	points := []string{"plug", "ac", "dc"}
	// Flow up: ac is upstream of plug, so the loss is taken off the magnitude.
	if got := correctedSubstitute(insight, points, 1, 0, "up", -300); got != -291 {
		t.Fatalf("substitute = %v, want -291", got)
	}
}

func TestSubstituteSumsSegmentsOnTheWay(t *testing.T) {
	insight := &pairChainInsight{losses: map[string]float64{"plug->ac": 9, "ac->dc": 6}}
	points := []string{"plug", "ac", "dc"}
	if got := correctedSubstitute(insight, points, 2, 0, "down", 300); got != 315 {
		t.Fatalf("substitute = %v, want 315", got)
	}
}

func TestSubstituteStaysRawWithoutLearnedSegment(t *testing.T) {
	insight := &pairChainInsight{losses: map[string]float64{"plug->ac": 9}}
	points := []string{"plug", "ac", "dc"}
	if got := correctedSubstitute(insight, points, 2, 0, "down", 300); got != 300 {
		t.Fatalf("substitute = %v, want the raw 300 W without a partial correction", got)
	}
}

func TestSubstituteStaysRawWhenIdleOrWithoutInsight(t *testing.T) {
	insight := &pairChainInsight{losses: map[string]float64{"plug->ac": 9}}
	points := []string{"plug", "ac", "dc"}
	if got := correctedSubstitute(insight, points, 1, 0, "idle", 300); got != 300 {
		t.Fatalf("idle substitute = %v, want 300", got)
	}
	if got := correctedSubstitute(nil, points, 1, 0, "down", 300); got != 300 {
		t.Fatalf("substitute without insight = %v, want 300", got)
	}
}

func TestSubstituteNeverCrossesZero(t *testing.T) {
	insight := &pairChainInsight{losses: map[string]float64{"ac->plug": 9}}
	points := []string{"plug", "ac", "dc"}
	if got := correctedSubstitute(insight, points, 1, 0, "up", -5); got != 0 {
		t.Fatalf("substitute = %v, want 0, not a sign flip", got)
	}
}

func TestSubstituteLooksUpClassByRawValue(t *testing.T) {
	insight := &pairChainInsight{losses: map[string]float64{"plug->ac": 9}}
	points := []string{"plug", "ac", "dc"}
	correctedSubstitute(insight, points, 1, 0, "down", -300)
	if len(insight.countedW) != 1 || insight.countedW[0] != 300 {
		t.Fatalf("counting classes = %v, want [300] from the absolute raw value", insight.countedW)
	}
}

func TestChainSubstituteCorrectedByLearnedLoss(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	plug := liveEntity("plug", "300", at)
	plug.Available, plug.HasAvailability = false, true
	model := Model{Chains: map[string]Chain{"line": {Label: "Linie", Points: []string{"plug", "ac"}, Counting: "plug"}}}
	devices, resolver := chainWorld(model,
		map[string]Assignment{"plug": {Role: RoleBattery, Scale: 1}},
		plug, liveEntity("ac", "300", at), liveEntity("dc", "0", at))
	resolver.SetChainInsight(&pairChainInsight{losses: map[string]float64{"plug->ac": 9}})

	snapshot := Aggregate(devices, resolver, at)
	if got := snapshot.Values[RoleBattery]; got != 309 {
		t.Fatalf("battery = %v, want 309 W (ac plus the learned 9 W loss)", got)
	}
}
