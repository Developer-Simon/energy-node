package energy

import (
	"fmt"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energydiscovery"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func TestAggregateNormalizesUnitsAndUsesHeuristicRoles(t *testing.T) {
	devices := []registry.DeviceView{{
		ID: "inverter",
		Entities: []registry.EntityView{
			{UniqueID: "pv_total_power", ObjectID: "total_power", Name: "PV Gesamtleistung", UnitOfMeasurement: "W", Value: "420", HasValue: true},
			{UniqueID: "netz_power", ObjectID: "power", Name: "Netz Leistung", UnitOfMeasurement: "kW", Value: "0.35", HasValue: true},
		},
	}}

	snapshot := Aggregate(devices, nil, time.Unix(100, 0).UTC())
	if got := snapshot.Values[RolePV]; got != 420 {
		t.Fatalf("PV = %v, want 420 W", got)
	}
	if got := snapshot.Values[RoleGrid]; got != 350 {
		t.Fatalf("grid = %v, want 350 W", got)
	}
	if len(snapshot.Quality) != 0 {
		t.Fatalf("unexpected quality warnings: %#v", snapshot.Quality)
	}
}

func TestExplicitAssignmentOverridesHeuristicAndInvertsValue(t *testing.T) {
	devices := []registry.DeviceView{{
		ID:       "meter",
		Entities: []registry.EntityView{{UniqueID: "meter_power", ObjectID: "power", Name: "Netz Leistung", UnitOfMeasurement: "W", Value: "125", HasValue: true}},
	}}
	resolver := NewResolver(map[string]Assignment{"meter_power": {Role: RoleLoad, Invert: true}})

	snapshot := Aggregate(devices, resolver, time.Now().UTC())
	if got := snapshot.Values[RoleLoad]; got != -125 {
		t.Fatalf("load = %v, want -125 W", got)
	}
	if got := snapshot.Entities[0].Role.Source; got != "override" {
		t.Fatalf("source = %q, want override", got)
	}
}

func TestAggregateReportsInvalidPowerWithoutAddingValue(t *testing.T) {
	devices := []registry.DeviceView{{
		ID:       "inverter",
		Entities: []registry.EntityView{{UniqueID: "pv_power", ObjectID: "pv_power", UnitOfMeasurement: "W", Value: "not-a-number", HasValue: true}},
	}}

	snapshot := Aggregate(devices, nil, time.Now().UTC())
	if len(snapshot.Values) != 0 {
		t.Fatalf("values = %#v, want no values", snapshot.Values)
	}
	if len(snapshot.Quality) != 1 {
		t.Fatalf("quality = %#v, want one warning", snapshot.Quality)
	}
}

func TestHeuristicIgnoresEnergyTotalsAndNonPowerUnits(t *testing.T) {
	devices := []registry.DeviceView{{
		ID: "inverter",
		Entities: []registry.EntityView{
			{UniqueID: "pv_energy_today", ObjectID: "pv_energy_today", Name: "PV Tagesertrag", UnitOfMeasurement: "kWh", Value: "4.2", HasValue: true},
			{UniqueID: "battery_soc", ObjectID: "soc", Name: "Batterie SOC", UnitOfMeasurement: "%", Value: "80", HasValue: true},
		},
	}}

	snapshot := Aggregate(devices, nil, time.Now().UTC())
	if len(snapshot.Values) != 0 || len(snapshot.Quality) != 0 || len(snapshot.Entities) != 0 {
		t.Fatalf("non-power entities affected snapshot: %#v", snapshot)
	}
}

func TestAggregateIgnoresEntitiesWithoutLiveValue(t *testing.T) {
	devices := []registry.DeviceView{{
		ID: "offline-meter",
		Entities: []registry.EntityView{{
			UniqueID:          "offline_power",
			ObjectID:          "power",
			Name:              "Offline Leistung",
			UnitOfMeasurement: "W",
			HasValue:          false,
		}},
	}}

	snapshot := Aggregate(devices, nil, time.Now().UTC())
	if len(snapshot.Values) != 0 || len(snapshot.Quality) != 0 || len(snapshot.Entities) != 0 {
		t.Fatalf("missing live value affected snapshot: %#v", snapshot)
	}
}

func TestAggregateMarksUnavailableLastValueAsStale(t *testing.T) {
	devices := []registry.DeviceView{{
		ID: "offline-meter",
		Entities: []registry.EntityView{{
			UniqueID:          "offline_power",
			ObjectID:          "pv_power",
			Name:              "Offline PV",
			UnitOfMeasurement: "W",
			Value:             "420",
			HasValue:          true,
			HasAvailability:   true,
			Available:         false,
		}},
	}}

	snapshot := Aggregate(devices, nil, time.Now().UTC())
	if got := snapshot.Values[RolePV]; got != 420 {
		t.Fatalf("PV = %v, want 420 W", got)
	}
	if len(snapshot.Entities) != 1 {
		t.Fatalf("entities = %#v, want one stale entity", snapshot.Entities)
	}
	if got := snapshot.Entities[0].Freshness; got != "stale" {
		t.Fatalf("entity freshness = %q, want stale", got)
	}
	if len(snapshot.Roles) != 1 || snapshot.Roles[0].Freshness != "stale" || snapshot.Roles[0].Quality != "stale" {
		t.Fatalf("role state = %#v, want stale", snapshot.Roles)
	}
}

func TestAggregateInfersDirectionalRolesAndReportsSemantics(t *testing.T) {
	devices := []registry.DeviceView{{
		ID: "energy",
		Entities: []registry.EntityView{
			{UniqueID: "battery_laden", ObjectID: "battery_charge", Name: "Batterie Laden", UnitOfMeasurement: "W", Value: "200", HasValue: true},
			{UniqueID: "battery_entladen", ObjectID: "battery_discharge", Name: "Batterie Entladen", UnitOfMeasurement: "W", Value: "80", HasValue: true},
			{UniqueID: "netzbezug", ObjectID: "grid_import", Name: "Netzbezug", UnitOfMeasurement: "W", Value: "125", HasValue: true},
			{UniqueID: "einspeisung", ObjectID: "grid_export", Name: "Einspeisung", UnitOfMeasurement: "W", Value: "40", HasValue: true},
			{UniqueID: "wallbox_power", ObjectID: "wallbox_power", Name: "Wallbox", UnitOfMeasurement: "W", Value: "7400", HasValue: true},
			{UniqueID: "waermepumpe_power", ObjectID: "waermepumpe_power", Name: "Waermepumpe", UnitOfMeasurement: "W", Value: "900", HasValue: true},
		},
	}}

	snapshot := Aggregate(devices, nil, time.Unix(100, 0).UTC())
	for role, want := range map[Role]float64{
		RoleBatteryCharge:    200,
		RoleBatteryDischarge: 80,
		RoleGridImport:       125,
		RoleGridExport:       40,
		RoleWallbox:          7400,
		RoleHeatPump:         900,
	} {
		if got := snapshot.Values[role]; got != want {
			t.Fatalf("%s = %v, want %v", role, got, want)
		}
	}
	states := make(map[Role]RoleState, len(snapshot.Roles))
	for _, state := range snapshot.Roles {
		states[state.Role] = state
	}
	if got := states[RoleGridImport].Sign; got != "positiv = Netzbezug" {
		t.Fatalf("grid import sign = %q", got)
	}
	if got := states[RoleWallbox].Quality; got != "good" || states[RoleWallbox].Freshness != "fresh" || states[RoleWallbox].Source != "live" {
		t.Fatalf("wallbox metadata = %#v", states[RoleWallbox])
	}
}

func TestAggregateListsUnassignedPowerEntities(t *testing.T) {
	devices := []registry.DeviceView{{
		ID: "shop",
		Entities: []registry.EntityView{
			{UniqueID: "workshop_socket", ObjectID: "socket_1", Name: "Werkstatt Steckdose", UnitOfMeasurement: "W", Value: "340", HasValue: true},
		},
	}}

	snapshot := Aggregate(devices, nil, time.Now().UTC())
	if snapshot.UnassignedCount != 1 {
		t.Fatalf("unassigned_count = %d, want 1", snapshot.UnassignedCount)
	}
	if len(snapshot.Unassigned) != 1 || snapshot.Unassigned[0].EntityID != "workshop_socket" || snapshot.Unassigned[0].Value != 340 {
		t.Fatalf("unassigned = %#v, want one 340 W entry", snapshot.Unassigned)
	}
}

func TestAggregateIgnoresDashboardsOwnEnergyDiscoveryDevice(t *testing.T) {
	devices := []registry.DeviceView{{
		ID:   energydiscovery.DeviceIdentifier,
		Name: "Energy Node",
		Entities: []registry.EntityView{
			{UniqueID: energydiscovery.DeviceID + "_pv_power", ObjectID: "pv_power", Name: "PV-Leistung", UnitOfMeasurement: "W", Value: "420", HasValue: true},
			{UniqueID: energydiscovery.DeviceID + "_grid_import", ObjectID: "grid_import", Name: "Netzbezug", UnitOfMeasurement: "W", Value: "130", HasValue: true},
		},
	}}

	snapshot := Aggregate(devices, nil, time.Now().UTC())
	if len(snapshot.Values) != 0 {
		t.Fatalf("dashboard's own energy device fed back into aggregation: %#v", snapshot.Values)
	}
	if snapshot.UnassignedCount != 0 || len(snapshot.Unassigned) != 0 {
		t.Fatalf("dashboard's own energy device listed as unassigned: %#v", snapshot.Unassigned)
	}
	if len(snapshot.Entities) != 0 {
		t.Fatalf("dashboard's own energy device resolved to entities: %#v", snapshot.Entities)
	}
}

func TestAggregateExcludesDeliberatelyClearedRolesFromUnassigned(t *testing.T) {
	devices := []registry.DeviceView{{
		ID: "shop",
		Entities: []registry.EntityView{
			{UniqueID: "workshop_socket", ObjectID: "socket_1", Name: "Werkstatt Steckdose", UnitOfMeasurement: "W", Value: "340", HasValue: true},
		},
	}}
	resolver := NewResolver(map[string]Assignment{"workshop_socket": {Role: ""}})

	snapshot := Aggregate(devices, resolver, time.Now().UTC())
	if snapshot.UnassignedCount != 0 || len(snapshot.Unassigned) != 0 {
		t.Fatalf("deliberately cleared role counted as unassigned: %#v", snapshot)
	}
}

func TestAggregateExcludesNonPowerUnitsFromUnassigned(t *testing.T) {
	devices := []registry.DeviceView{{
		ID: "shop",
		Entities: []registry.EntityView{
			{UniqueID: "battery_soc", ObjectID: "soc", Name: "Batterie SOC", UnitOfMeasurement: "%", Value: "80", HasValue: true},
		},
	}}

	snapshot := Aggregate(devices, nil, time.Now().UTC())
	if snapshot.UnassignedCount != 0 || len(snapshot.Unassigned) != 0 {
		t.Fatalf("non-power unit counted as unassigned: %#v", snapshot)
	}
}

func TestAggregateCapsUnassignedListButKeepsExactCount(t *testing.T) {
	entities := make([]registry.EntityView, 0, 30)
	for i := 0; i < 30; i++ {
		entities = append(entities, registry.EntityView{
			UniqueID: fmt.Sprintf("socket_%d", i), ObjectID: fmt.Sprintf("socket_%d", i),
			Name: "Steckdose", UnitOfMeasurement: "W", Value: "10", HasValue: true,
		})
	}
	devices := []registry.DeviceView{{ID: "shop", Entities: entities}}

	snapshot := Aggregate(devices, nil, time.Now().UTC())
	if snapshot.UnassignedCount != 30 {
		t.Fatalf("unassigned_count = %d, want 30", snapshot.UnassignedCount)
	}
	if len(snapshot.Unassigned) != 25 {
		t.Fatalf("unassigned list = %d entries, want capped at 25", len(snapshot.Unassigned))
	}
}

func TestSnapshotDerivesBatteryDirectionFromGenericPower(t *testing.T) {
	charging := Snapshot{Values: map[Role]float64{RoleBattery: 150}}
	if got := charging.BatteryChargePower(); got != 150 {
		t.Fatalf("generic charging power = %v, want 150 W", got)
	}
	if got := charging.BatteryDischargePower(); got != 0 {
		t.Fatalf("generic discharge power while charging = %v, want 0 W", got)
	}

	discharging := Snapshot{Values: map[Role]float64{RoleBattery: -150}}
	if got := discharging.BatteryDischargePower(); got != 150 {
		t.Fatalf("generic discharge power = %v, want 150 W", got)
	}
	if got := discharging.BatteryChargePower(); got != 0 {
		t.Fatalf("generic charge power while discharging = %v, want 0 W", got)
	}
}

func socDevice(entities ...registry.EntityView) []registry.DeviceView {
	return []registry.DeviceView{{ID: "bms", Name: "BMS", Entities: entities}}
}

func socEntity(id, value string) registry.EntityView {
	return registry.EntityView{UniqueID: id, Name: id, Value: value, HasValue: true, UnitOfMeasurement: "%", Source: "live"}
}

func TestAggregateWeightsBatterySoCByCapacity(t *testing.T) {
	// Bank A: 80 % auf 10 kWh, Bank B: 50 % auf 30 kWh.
	// Gewichtet: (80*10 + 50*30) / 40 = 2300/40 = 57.5 %
	// Gespeichert: 2300/100 = 23 kWh
	devices := socDevice(socEntity("bank_a", "80"), socEntity("bank_b", "50"))
	resolver := NewResolver(map[string]Assignment{
		"bank_a": {Role: RoleBatterySoC, CapacityKWh: 10},
		"bank_b": {Role: RoleBatterySoC, CapacityKWh: 30},
	})
	snapshot := Aggregate(devices, resolver, time.Now())

	if !almostEqual(snapshot.Value("battery_soc"), 57.5) {
		t.Fatalf("battery_soc = %v, want 57.5", snapshot.Value("battery_soc"))
	}
	if !almostEqual(snapshot.Value("battery_capacity_kwh"), 40) {
		t.Fatalf("battery_capacity_kwh = %v, want 40", snapshot.Value("battery_capacity_kwh"))
	}
	if !almostEqual(snapshot.Value("battery_energy_kwh"), 23) {
		t.Fatalf("battery_energy_kwh = %v, want 23", snapshot.Value("battery_energy_kwh"))
	}
}

func TestAggregateBatterySoCRoleStateIsTheWeightedMeanNotTheSum(t *testing.T) {
	devices := socDevice(socEntity("bank_a", "80"), socEntity("bank_b", "80"))
	resolver := NewResolver(map[string]Assignment{
		"bank_a": {Role: RoleBatterySoC, CapacityKWh: 10},
		"bank_b": {Role: RoleBatterySoC, CapacityKWh: 10},
	})
	snapshot := Aggregate(devices, resolver, time.Now())

	var found *RoleState
	for i := range snapshot.Roles {
		if snapshot.Roles[i].Role == RoleBatterySoC {
			found = &snapshot.Roles[i]
		}
		if snapshot.Roles[i].Role == Role("battery_capacity_kwh") || snapshot.Roles[i].Role == Role("battery_energy_kwh") {
			t.Fatalf("kWh-Rechenwerte duerfen nicht in Roles auftauchen: %#v", snapshot.Roles[i])
		}
	}
	if found == nil {
		t.Fatal("Roles enthaelt keinen battery_soc-Eintrag")
	}
	if !almostEqual(found.Value, 80) {
		t.Fatalf("RoleState.Value = %v, want 80 (Mittelwert, nicht Summe 160)", found.Value)
	}
	if found.Unit != "%" {
		t.Fatalf("RoleState.Unit = %q, want %%", found.Unit)
	}
	if len(found.Entities) != 2 {
		t.Fatalf("RoleState.Entities = %v, want beide Baenke", found.Entities)
	}
}

func TestAggregateSkipsBatterySoCWithoutCapacityAndCountsIt(t *testing.T) {
	devices := socDevice(socEntity("bank_a", "80"), socEntity("bank_b", "20"))
	resolver := NewResolver(map[string]Assignment{
		"bank_a": {Role: RoleBatterySoC, CapacityKWh: 10},
		"bank_b": {Role: RoleBatterySoC}, // keine Kapazitaet
	})
	snapshot := Aggregate(devices, resolver, time.Now())

	if !almostEqual(snapshot.Value("battery_soc"), 80) {
		t.Fatalf("battery_soc = %v, want 80 (bank_b zaehlt nicht mit)", snapshot.Value("battery_soc"))
	}
	if !almostEqual(snapshot.Value("battery_capacity_kwh"), 10) {
		t.Fatalf("battery_capacity_kwh = %v, want 10", snapshot.Value("battery_capacity_kwh"))
	}
	if snapshot.BatterySoCWithoutCapacity != 1 {
		t.Fatalf("battery_soc_without_capacity = %d, want 1", snapshot.BatterySoCWithoutCapacity)
	}
}

func TestAggregateOmitsBatteryKeysWhenNoCapacityIsKnown(t *testing.T) {
	devices := socDevice(socEntity("bank_a", "80"))
	resolver := NewResolver(map[string]Assignment{"bank_a": {Role: RoleBatterySoC}})
	snapshot := Aggregate(devices, resolver, time.Now())

	for _, key := range []string{"battery_soc", "battery_capacity_kwh", "battery_energy_kwh"} {
		if snapshot.HasValue(key) {
			t.Fatalf("Values enthaelt %q, obwohl keine Kapazitaet bekannt ist", key)
		}
	}
}

func TestAggregateRejectsBatterySoCOnAPowerEntity(t *testing.T) {
	entity := registry.EntityView{UniqueID: "meter", Name: "meter", Value: "1200", HasValue: true, UnitOfMeasurement: "W", Source: "live"}
	resolver := NewResolver(map[string]Assignment{"meter": {Role: RoleBatterySoC, CapacityKWh: 10}})
	snapshot := Aggregate(socDevice(entity), resolver, time.Now())

	if snapshot.HasValue("battery_soc") {
		t.Fatal("eine W-Entitaet darf nicht als Fuellstand zaehlen")
	}
	if len(snapshot.Quality) == 0 {
		t.Fatal("die unpassende Einheit muss in Quality vermerkt werden")
	}
}

func TestAggregateIgnoresScaleAndInvertForBatterySoC(t *testing.T) {
	devices := socDevice(socEntity("bank_a", "80"))
	resolver := NewResolver(map[string]Assignment{
		"bank_a": {Role: RoleBatterySoC, CapacityKWh: 10, Scale: 0.5, Invert: true},
	})
	snapshot := Aggregate(devices, resolver, time.Now())

	if !almostEqual(snapshot.Value("battery_soc"), 80) {
		t.Fatalf("battery_soc = %v, want 80 - Scale/Invert gelten fuer die SoC-Rolle nicht", snapshot.Value("battery_soc"))
	}
}

// Die Einzelpositionen der Energiegrafiken (measured_split "entities", siehe
// energy-model.js) beschriften sich aus diesem Feld. Die Form ist bewusst
// dieselbe wie die Zeilenbeschriftung auf der Energie-Rollen-Seite
// (energy.page.js: `${device.name} / ${entity.name || entity.object_id}`),
// damit eine Position in der Grafik ohne Umweg der Zeile zuzuordnen ist,
// in der sie zugewiesen wurde.
func TestAggregateLabelsResolvedEntitiesWithDeviceAndEntityName(t *testing.T) {
	devices := []registry.DeviceView{{
		ID:   "werkstatt",
		Name: "Werkstatt",
		Entities: []registry.EntityView{{
			UniqueID:          "werkstatt_power",
			Name:              "Leistung",
			ObjectID:          "power",
			UnitOfMeasurement: "W",
			Value:             "520",
			HasValue:          true,
		}},
	}}
	resolver := NewResolver(map[string]Assignment{"werkstatt_power": {Role: RoleLoad, Scale: 1}})

	snapshot := Aggregate(devices, resolver, time.Now())

	if len(snapshot.Entities) != 1 {
		t.Fatalf("Entities = %d, want 1", len(snapshot.Entities))
	}
	if got := snapshot.Entities[0].Label; got != "Werkstatt / Leistung" {
		t.Fatalf("Label = %q, want %q", got, "Werkstatt / Leistung")
	}
}
