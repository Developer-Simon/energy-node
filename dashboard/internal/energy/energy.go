// Package energy resolves energy roles from discovery entities and aggregates
// available power measurements into a stable dashboard snapshot.
package energy

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energydiscovery"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

type Role string

const (
	RolePV               Role = "pv"
	RoleBattery          Role = "battery"
	RoleBatteryCharge    Role = "battery_charge"
	RoleBatteryDischarge Role = "battery_discharge"
	RoleGrid             Role = "grid"
	RoleGridImport       Role = "grid_import"
	RoleGridExport       Role = "grid_export"
	RoleLoad             Role = "load"
	RoleWallbox          Role = "wallbox"
	RoleHeatPump         Role = "heat_pump"

	// RoleBatterySoC ist die einzige Rolle mit Prozent- statt Leistungs-
	// einheit. Sie wird nicht summiert, sondern nach CapacityKWh gewichtet
	// gemittelt - siehe den zweiten Zweig in Aggregate.
	RoleBatterySoC Role = "battery_soc"
)

// keyBatteryCapacityKWh und keyBatteryEnergyKWh sind Schluessel in
// Snapshot.Values, aber keine Rollen: es gibt keine Entitaet, die man ihnen
// zuordnen koennte. Sie liegen trotzdem in Values, damit die geteilte Fixture
// balance-cases.json sie ohne jede Aenderung an der Testmechanik ausdruecken
// kann. Verbraucher ausserhalb des Packages lesen sie ueber
// Snapshot.Value("battery_capacity_kwh").
const (
	keyBatteryCapacityKWh Role = "battery_capacity_kwh"
	keyBatteryEnergyKWh   Role = "battery_energy_kwh"
)

type Assignment struct {
	Role   Role    `json:"role"`
	Scale  float64 `json:"scale,omitempty"`
	Invert bool    `json:"invert,omitempty"`
	// CapacityKWh hat nur fuer RoleBatterySoC eine Bedeutung: die nutzbare
	// Kapazitaet dieser Bank in kWh, das Gewicht im Gesamtfuellstand.
	// Bei den Leistungsrollen bleibt es 0 und wird nirgends gelesen.
	CapacityKWh float64 `json:"capacity_kwh,omitempty"`
}

type ResolvedAssignment struct {
	Assignment
	Source string `json:"source"`
}

type RoleState struct {
	Role      Role     `json:"role"`
	Label     string   `json:"label"`
	Value     float64  `json:"value"`
	Unit      string   `json:"unit"`
	Sign      string   `json:"sign"`
	Quality   string   `json:"quality"`
	Freshness string   `json:"freshness"`
	Source    string   `json:"source"`
	Entities  []string `json:"entities"`
}

type Snapshot struct {
	At              time.Time          `json:"at"`
	Values          map[Role]float64   `json:"values"`
	Sources         map[Role][]string  `json:"sources"`
	Quality         []string           `json:"quality,omitempty"`
	Entities        []ResolvedEntity   `json:"entities"`
	Roles           []RoleState        `json:"roles"`
	Interpretation  Interpretation     `json:"interpretation"`
	Balance         Balance            `json:"balance"`
	Unassigned      []UnassignedEntity `json:"unassigned"`
	UnassignedCount int                `json:"unassigned_count"`

	// BatterySoCWithoutCapacity zaehlt Entitaeten mit Rolle battery_soc, denen
	// keine Kapazitaet hinterlegt ist. Sie gehen in keine der drei Batterie-
	// zahlen ein; der Energie-Tab macht daraus eine Hinweismeldung. Bewusst
	// ein eigenes Struct-Feld und kein Eintrag in Values - dort stehen
	// Messwerte, keine Abzaehlungen.
	BatterySoCWithoutCapacity int `json:"battery_soc_without_capacity"`
}

// UnassignedEntity is a power-unit entity Aggregate found no role for -
// neither a heuristic match nor an override. Distinct from an override that
// deliberately clears a role (Resolver.Resolve reports that with
// Source == "override"), so the settings page can tell "never assigned" from
// "assigned to nothing on purpose".
type UnassignedEntity struct {
	DeviceID string  `json:"device_id"`
	EntityID string  `json:"entity_id"`
	Name     string  `json:"name"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"`
}

// maxUnassignedEntities caps the Unassigned list; UnassignedCount stays the
// exact total so "N Leistungswerte ohne Rolle" on the settings page never
// undercounts even when the list itself is truncated.
const maxUnassignedEntities = 25

// WithInterpretation is the opt-in second step that fills Balance from cfg,
// keeping Aggregate's signature untouched for the many callers that only
// need the raw role values.
func (s Snapshot) WithInterpretation(cfg Interpretation) Snapshot {
	s.Interpretation = cfg
	s.Balance = DeriveBalance(s, cfg)
	return s
}

type ResolvedEntity struct {
	DeviceID string `json:"device_id"`
	EntityID string `json:"entity_id"`
	// Label ist der Anzeigename fuer Grafiken, die eine einzelne Entitaet
	// benennen muessen (measured_split "entities"). Gleiche Form wie die
	// Zeilenbeschriftung der Energie-Rollen-Seite, damit Grafik und
	// Zuordnungsformular dieselbe Sprache sprechen.
	Label     string             `json:"label"`
	Value     float64            `json:"value"`
	Unit      string             `json:"unit"`
	Role      ResolvedAssignment `json:"role"`
	Sign      string             `json:"sign"`
	Quality   string             `json:"quality"`
	Freshness string             `json:"freshness"`
	Source    string             `json:"source"`
	LastSeen  time.Time          `json:"last_seen"`
}

func (s Snapshot) Value(role string) float64 {
	return s.Values[Role(role)]
}

func (s Snapshot) HasValue(role string) bool {
	_, ok := s.Values[Role(role)]
	return ok
}

func (s Snapshot) IsStale(role string) bool {
	for _, state := range s.Roles {
		if state.Role == Role(role) {
			return state.Freshness == "stale"
		}
	}
	return false
}

func (s Snapshot) HasPower(role string) bool {
	value, ok := s.Values[Role(role)]
	return ok && math.Abs(value) > 0.000001
}

func (s Snapshot) BatteryChargePower() float64 {
	if value, ok := s.Values[RoleBatteryCharge]; ok {
		return math.Abs(value)
	}
	if value, ok := s.Values[RoleBattery]; ok && value > 0 {
		return value
	}
	return 0
}

func (s Snapshot) BatteryDischargePower() float64 {
	if value, ok := s.Values[RoleBatteryDischarge]; ok {
		return math.Abs(value)
	}
	if value, ok := s.Values[RoleBattery]; ok && value < 0 {
		return math.Abs(value)
	}
	return 0
}

// GridImportPower/GridExportPower mirror BatteryChargePower/
// BatteryDischargePower: prefer the split grid_import/grid_export roles,
// fall back to the sign of a combined "grid" role. Ported from the
// energy-model.js functions of the same name, which stay a deliberate
// duplicate for the JS mirror used by energy-day.js's history points.
func (s Snapshot) GridImportPower() float64 {
	if value, ok := s.Values[RoleGridImport]; ok {
		return math.Abs(value)
	}
	if value, ok := s.Values[RoleGrid]; ok && value > 0 {
		return value
	}
	return 0
}

func (s Snapshot) GridExportPower() float64 {
	if value, ok := s.Values[RoleGridExport]; ok {
		return math.Abs(value)
	}
	if value, ok := s.Values[RoleGrid]; ok && value < 0 {
		return math.Abs(value)
	}
	return 0
}

type Resolver struct {
	mu             sync.RWMutex
	overrides      map[string]Assignment
	interpretation Interpretation
}

func NewResolver(overrides map[string]Assignment) *Resolver {
	copy := make(map[string]Assignment, len(overrides))
	for id, assignment := range overrides {
		if assignment.Scale == 0 {
			assignment.Scale = 1
		}
		copy[id] = assignment
	}
	return &Resolver{overrides: copy, interpretation: DefaultInterpretation()}
}

func (r *Resolver) Resolve(entity registry.EntityView) (ResolvedAssignment, bool) {
	r.mu.RLock()
	assignment, ok := r.overrides[entity.UniqueID]
	r.mu.RUnlock()
	if ok {
		return ResolvedAssignment{Assignment: assignment, Source: "override"}, assignment.Role != ""
	}
	role, ok := inferRole(entity)
	if !ok {
		return ResolvedAssignment{}, false
	}
	return ResolvedAssignment{Assignment: Assignment{Role: role, Scale: 1}, Source: "heuristic"}, true
}

func (r *Resolver) SetOverrides(overrides map[string]Assignment) {
	next := NewResolver(overrides)
	r.mu.Lock()
	r.overrides = next.overrides
	r.mu.Unlock()
}

// SetInterpretation/Interpretation give the automation engine and the HTTP
// handlers a shared, mutex-guarded view of the balance interpretation to
// use - same pattern as SetOverrides/Resolve.
func (r *Resolver) SetInterpretation(cfg Interpretation) {
	r.mu.Lock()
	r.interpretation = cfg.Normalized()
	r.mu.Unlock()
}

func (r *Resolver) Interpretation() Interpretation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.interpretation
}

func Aggregate(devices []registry.DeviceView, resolver *Resolver, at time.Time) Snapshot {
	if resolver == nil {
		resolver = NewResolver(nil)
	}
	snapshot := Snapshot{At: at, Values: make(map[Role]float64), Sources: make(map[Role][]string), Entities: []ResolvedEntity{}, Roles: []RoleState{}, Unassigned: []UnassignedEntity{}}
	roleStates := make(map[Role]*RoleState)
	// Zwei Akkumulatoren statt einer Summe: Prozentwerte lassen sich nicht
	// addieren, nur nach Kapazitaet gewichtet mitteln.
	var socWeighted, socCapacity float64
	for _, device := range devices {
		// Das Dashboard veröffentlicht seine eigene Bilanz als HA-MQTT-Gerät
		// (internal/energydiscovery) und liest die Discovery-Configs über die
		// eigene Subscription wieder in die Registry ein. Ohne diesen Guard
		// ordnete inferRole die zurückgelesenen W-Sensoren erneut Rollen zu -
		// Doppelzählung und Rückkopplung. Die Registry behält das Gerät, damit
		// die "Energy Node"-Kachel im Energie-Tab es weiter anzeigt.
		if device.ID == energydiscovery.DeviceIdentifier {
			continue
		}
		for _, entity := range device.Entities {
			assignment, ok := resolver.Resolve(entity)
			if !ok {
				if isPowerUnit(entity.UnitOfMeasurement) && assignment.Source != "override" {
					snapshot.UnassignedCount++
					if len(snapshot.Unassigned) < maxUnassignedEntities {
						value, unit, _ := powerValue(entity.Value, entity.UnitOfMeasurement)
						snapshot.Unassigned = append(snapshot.Unassigned, UnassignedEntity{
							DeviceID: device.ID, EntityID: entity.UniqueID, Name: entity.Name, Value: value, Unit: unit,
						})
					}
				}
				continue
			}
			if !entity.HasValue {
				continue
			}
			if assignment.Role == RoleBatterySoC {
				value, err := percentValue(entity.Value, entity.UnitOfMeasurement)
				if err != nil {
					snapshot.Quality = append(snapshot.Quality, fmt.Sprintf("%s: %v", entity.UniqueID, err))
					continue
				}
				// Scale und Invert gelten hier bewusst nicht.
				if assignment.CapacityKWh > 0 {
					socWeighted += value * assignment.CapacityKWh
					socCapacity += assignment.CapacityKWh
				} else {
					snapshot.BatterySoCWithoutCapacity++
				}
				snapshot.Sources[RoleBatterySoC] = append(snapshot.Sources[RoleBatterySoC], entity.UniqueID)
				semantics := semanticsFor(RoleBatterySoC)
				freshness, quality := "fresh", "good"
				if entity.Stale || (entity.HasAvailability && !entity.Available) {
					freshness, quality = "stale", "stale"
				}
				source := entity.Source
				if source == "" {
					source = "live"
				}
				state, ok := roleStates[RoleBatterySoC]
				if !ok {
					state = &RoleState{Role: RoleBatterySoC, Label: semantics.Label, Unit: "%", Sign: semantics.Sign, Quality: quality, Freshness: freshness, Source: source, Entities: []string{}}
					roleStates[RoleBatterySoC] = state
				} else {
					state.Quality = mergeQuality(state.Quality, quality)
					state.Freshness = mergeFreshness(state.Freshness, freshness)
					state.Source = mergeSource(state.Source, source)
				}
				// state.Value bleibt hier absichtlich unberuehrt - eine Summe
				// von Prozentwerten waere Unsinn. Er wird nach der Schleife
				// auf den gewichteten Mittelwert gesetzt.
				state.Entities = append(state.Entities, entity.UniqueID)
				snapshot.Entities = append(snapshot.Entities, ResolvedEntity{DeviceID: device.ID, EntityID: entity.UniqueID, Label: entityLabel(device, entity), Value: value, Unit: "%", Role: assignment, Sign: semantics.Sign, Quality: quality, Freshness: freshness, Source: source, LastSeen: entity.LastSeen})
				continue
			}
			value, unit, err := powerValue(entity.Value, entity.UnitOfMeasurement)
			if err != nil {
				snapshot.Quality = append(snapshot.Quality, fmt.Sprintf("%s: %v", entity.UniqueID, err))
				continue
			}
			if assignment.Invert {
				value = -value
			}
			value *= assignment.Scale
			snapshot.Values[assignment.Role] += value
			snapshot.Sources[assignment.Role] = append(snapshot.Sources[assignment.Role], entity.UniqueID)
			semantics := semanticsFor(assignment.Role)
			freshness := "fresh"
			quality := "good"
			if entity.Stale || (entity.HasAvailability && !entity.Available) {
				freshness = "stale"
				quality = "stale"
			}
			source := entity.Source
			if source == "" {
				source = "live"
			}
			state, ok := roleStates[assignment.Role]
			if !ok {
				state = &RoleState{Role: assignment.Role, Label: semantics.Label, Unit: unit, Sign: semantics.Sign, Quality: quality, Freshness: freshness, Source: source, Entities: []string{}}
				roleStates[assignment.Role] = state
			} else {
				state.Quality = mergeQuality(state.Quality, quality)
				state.Freshness = mergeFreshness(state.Freshness, freshness)
				state.Source = mergeSource(state.Source, source)
			}
			state.Value += value
			state.Entities = append(state.Entities, entity.UniqueID)
			snapshot.Entities = append(snapshot.Entities, ResolvedEntity{DeviceID: device.ID, EntityID: entity.UniqueID, Label: entityLabel(device, entity), Value: value, Unit: unit, Role: assignment, Sign: semantics.Sign, Quality: quality, Freshness: freshness, Source: source, LastSeen: entity.LastSeen})
		}
	}
	if socCapacity > 0 {
		snapshot.Values[RoleBatterySoC] = socWeighted / socCapacity
		snapshot.Values[keyBatteryCapacityKWh] = socCapacity
		snapshot.Values[keyBatteryEnergyKWh] = socWeighted / 100
		if state, ok := roleStates[RoleBatterySoC]; ok {
			state.Value = snapshot.Values[RoleBatterySoC]
		}
	}
	for _, role := range []Role{RolePV, RoleBattery, RoleBatteryCharge, RoleBatteryDischarge, RoleGrid, RoleGridImport, RoleGridExport, RoleLoad, RoleWallbox, RoleHeatPump, RoleBatterySoC} {
		if state, ok := roleStates[role]; ok {
			snapshot.Roles = append(snapshot.Roles, *state)
		}
	}
	return snapshot
}

type roleSemantics struct {
	Label string
	Sign  string
}

func semanticsFor(role Role) roleSemantics {
	switch role {
	case RolePV:
		return roleSemantics{Label: "PV", Sign: "positiv = Erzeugung"}
	case RoleBatteryCharge:
		return roleSemantics{Label: "Batterie laden", Sign: "positiv = Laden"}
	case RoleBatteryDischarge:
		return roleSemantics{Label: "Batterie entladen", Sign: "positiv = Entladen"}
	case RoleGridImport:
		return roleSemantics{Label: "Grid-Import", Sign: "positiv = Netzbezug"}
	case RoleGridExport:
		return roleSemantics{Label: "Grid-Export", Sign: "positiv = Einspeisung"}
	case RoleLoad:
		return roleSemantics{Label: "Hausverbrauch", Sign: "positiv = Verbrauch"}
	case RoleWallbox:
		return roleSemantics{Label: "Wallbox", Sign: "positiv = Verbrauch"}
	case RoleHeatPump:
		return roleSemantics{Label: "Waermepumpe", Sign: "positiv = Verbrauch"}
	case RoleBatterySoC:
		return roleSemantics{Label: "Batterie-Fuellstand", Sign: "positiv = Ladezustand"}
	default:
		return roleSemantics{Label: string(role), Sign: "positiv = Rohwert"}
	}
}

func mergeQuality(left, right string) string {
	if left == "invalid" || right == "invalid" {
		return "invalid"
	}
	if left == "stale" || right == "stale" {
		return "stale"
	}
	return "good"
}

func mergeFreshness(left, right string) string {
	if left == "stale" || right == "stale" {
		return "stale"
	}
	return "fresh"
}

func mergeSource(left, right string) string {
	if left == right {
		return left
	}
	return "mixed"
}

func powerValue(raw, unit string) (float64, string, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid power value %q", raw)
	}
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "w", "watt", "watts":
		return value, "W", nil
	case "kw", "kilowatt", "kilowatts":
		return value * 1000, "W", nil
	default:
		return 0, "", fmt.Errorf("unsupported power unit %q", unit)
	}
}

// percentValue ist das Prozent-Gegenstueck zu powerValue: dieselbe Bauart,
// dieselbe Fehlerform, damit ein unpassend zugeordneter Wert in Quality
// genauso landet wie eine Leistung mit falscher Einheit.
func percentValue(raw, unit string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid percent value %q", raw)
	}
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "%", "percent", "prozent":
		return value, nil
	default:
		return 0, fmt.Errorf("unsupported battery_soc unit %q", unit)
	}
}

func inferRole(entity registry.EntityView) (Role, bool) {
	if !isPowerUnit(entity.UnitOfMeasurement) {
		return "", false
	}
	text := strings.ToLower(strings.Join([]string{entity.ObjectID, entity.Name, entity.StateTopic, entity.DeviceClass}, " "))
	switch {
	case containsAny(text, "wallbox", "ev_charger", "ev charger", "ladestation"):
		return RoleWallbox, true
	case containsAny(text, "wärmepumpe", "waermepumpe", "heat pump", "heatpump"):
		return RoleHeatPump, true
	case containsAny(text, "pv", "solar", "photovolta", "solarproduktion"):
		return RolePV, true
	case containsAny(text, "battery", "batterie", "speicher", "akku"):
		if containsAny(text, "discharge", "discharging", "entladen", "entladung") {
			return RoleBatteryDischarge, true
		}
		if containsAny(text, "charge", "charging", "laden", "ladung") {
			return RoleBatteryCharge, true
		}
		return RoleBattery, true
	case containsAny(text, "grid", "netz", "import", "export"):
		if containsAny(text, "import", "bezug", "netzbezug") {
			return RoleGridImport, true
		}
		if containsAny(text, "export", "einspeis", "netzexport") {
			return RoleGridExport, true
		}
		return RoleGrid, true
	case containsAny(text, "load", "verbrauch", "hausverbrauch", "house"):
		return RoleLoad, true
	default:
		return "", false
	}
}

func isPowerUnit(unit string) bool {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "w", "watt", "watts", "kw", "kilowatt", "kilowatts":
		return true
	default:
		return false
	}
}

func containsAny(value string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

// entityLabel spiegelt energy.page.js' Zeilenbeschriftung
// `${device.name} / ${entity.name || entity.object_id}`. Fehlt der
// Geraetename, bleibt der Entitaetsteil allein stehen statt ein fuehrendes
// " / " zu erzeugen.
func entityLabel(device registry.DeviceView, entity registry.EntityView) string {
	name := entity.Name
	if name == "" {
		name = entity.ObjectID
	}
	if name == "" {
		name = entity.UniqueID
	}
	if device.Name == "" {
		return name
	}
	return device.Name + " / " + name
}
