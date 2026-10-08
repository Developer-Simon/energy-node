// dashboard/internal/httpapi/history_exchange_series.go
// Welche Serien das Dashboard aufzeichnet. Ein Fremd-Peer wie Home
// Assistant liest die Liste aus der Ankuendigung und bietet nur diese an:
// was nicht hier steht, zoege jeder Browser in seine IndexedDB, ohne es je
// anzuzeigen.
package httpapi

import (
	"sort"
	"time"

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

// recordedExchangeSeries baut die Liste bei jeder Ankuendigung neu: die
// Rollenzuordnung und history_extra_entities aendern sich zur Laufzeit.
// Ein unlesbarer Einstellungsstand laesst nur die Zusatzentitaeten weg, die
// Ankuendigung selbst scheitert daran nicht.
func recordedExchangeSeries(reg *registry.Registry, store *settings.Store, resolver *energy.Resolver) func() []exchangeSeries {
	return func() []exchangeSeries {
		snapshot := energy.Aggregate(reg.Snapshot(), resolver, time.Now().UTC())
		var extras []registry.EntityValue
		if store != nil {
			if value, err := store.LoadSettings(); err == nil && len(value.HistoryExtraEntities) > 0 {
				extras = reg.Values(value.HistoryExtraEntities)
			}
		}
		return exchangeSeriesFrom(snapshot, extras)
	}
}
