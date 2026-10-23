// dashboard/internal/httpapi/history_exchange_series.go
// Welche Serien das Dashboard aufzeichnet. Ein Fremd-Peer wie Home
// Assistant liest die Liste aus der Ankuendigung und bietet nur diese an:
// was nicht hier steht, zoege jeder Browser in seine IndexedDB, ohne es je
// anzuzeigen.
package httpapi

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// exchangeSeriesFrom spiegelt roleSamples() in history-recorder.js: eine
// Rolle zaehlt, sobald eine ihrer Teilrollen einen Wert hat, und eine eigene
// Kategorie kuendigt `role:custom:<basis>:<id>` an. Weicht die Liste vom
// Recorder ab, bietet Home Assistant Serien an, die kein Browser fuehrt,
// oder verschweigt welche, die er fuehrt.
func exchangeSeriesFrom(snapshot energy.Snapshot, extras []registry.EntityValue) []exchangeSeries {
	has := func(roles ...energy.Role) bool {
		for _, role := range roles {
			if snapshot.HasValue(string(role)) {
				return true
			}
		}
		return false
	}
	list := []exchangeSeries{}
	add := func(role, unit string) {
		list = append(list, exchangeSeries{ID: "role:" + role, Unit: unit})
	}
	if has(energy.RolePV) {
		add("pv", "W")
	}
	if has(energy.RoleBattery, energy.RoleBatteryCharge, energy.RoleBatteryDischarge) {
		add("battery", "W")
	}
	if has(energy.RoleGrid, energy.RoleGridImport, energy.RoleGridExport) {
		add("grid", "W")
	}
	if has(energy.RoleLoad) {
		add("load", "W")
	}
	if has(energy.RoleWallbox) {
		add("wallbox", "W")
	}
	if has(energy.RoleHeatPump) {
		add("heat_pump", "W")
	}
	if has(energy.RoleBatterySoC) {
		add("battery_soc", "%")
	}
	custom := []string{}
	for role := range snapshot.Values {
		id, ok := role.CategoryID()
		if !ok {
			continue
		}
		if category, known := snapshot.Categories[id]; known {
			custom = append(custom, "custom:"+string(category.Base)+":"+id)
		}
	}
	sort.Strings(custom)
	for _, name := range custom {
		add(name, "W")
	}
	for _, entity := range extras {
		list = append(list, exchangeSeries{ID: entity.UniqueID, Unit: entity.Unit})
	}
	return list
}

// automationScenarioOutputPrefix und automationScenarioOutputSuffix bilden die
// Entitaets-ID eines Szenario-Reglers nach. Sie stehen so in entity_config
// in libs/energy_node_common/src/energy_node_common/discovery.py
// (unique_id = f"{device_id}_{object_id}", Geraet "automation").
const (
	automationScenarioOutputPrefix = "automation_scenario_"
	automationScenarioOutputSuffix = "_output"
)

// scenarioOutputEntityIDs liefert die Ausgabe-Entitaeten aller Szenarien mit
// while.mode "regulate" in Dokumentreihenfolge. Fehlt die Datei oder ist sie
// kaputt, gibt es keine Ausgaben und keinen Fehler.
func scenarioOutputEntityIDs(configs *config.Manager) []string {
	if configs == nil {
		return nil
	}
	data, err := configs.Read("automation_rules")
	if err != nil {
		return nil
	}
	var doc struct {
		Scenarios []struct {
			ID    string `json:"id"`
			While struct {
				Mode string `json:"mode"`
			} `json:"while"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	var ids []string
	for _, scenario := range doc.Scenarios {
		if scenario.While.Mode == "regulate" {
			ids = append(ids, automationScenarioOutputPrefix+scenario.ID+automationScenarioOutputSuffix)
		}
	}
	return ids
}

// recordedEntityIDs sind die Entitaeten, die der Browser zusaetzlich zu den
// Rollen aufzeichnet: history_extra_entities (Reihenfolge erhalten), danach
// die Szenario-Ausgaben, Duplikate entfernt. Allowlist von /history/entities
// und Serienliste der Ankuendigung kommen beide hier her, sonst laufen sie
// auseinander.
func recordedEntityIDs(store *settings.Store, configs *config.Manager) []string {
	ids := []string{}
	seen := map[string]bool{}
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if store != nil {
		if value, err := store.LoadSettings(); err == nil {
			for _, id := range value.HistoryExtraEntities {
				add(id)
			}
		}
	}
	for _, id := range scenarioOutputEntityIDs(configs) {
		add(id)
	}
	return ids
}

// recordedExchangeSeries baut die Liste bei jeder Ankuendigung neu: die
// Rollenzuordnung und history_extra_entities aendern sich zur Laufzeit.
// Ein unlesbarer Einstellungsstand laesst nur die Zusatzentitaeten weg, die
// Ankuendigung selbst scheitert daran nicht.
func recordedExchangeSeries(reg *registry.Registry, store *settings.Store, configs *config.Manager, resolver *energy.Resolver) func() []exchangeSeries {
	return func() []exchangeSeries {
		snapshot := energy.Aggregate(reg.Snapshot(), resolver, time.Now().UTC())
		var extras []registry.EntityValue
		if ids := recordedEntityIDs(store, configs); len(ids) > 0 {
			extras = reg.Values(ids)
		}
		return exchangeSeriesFrom(snapshot, extras)
	}
}
