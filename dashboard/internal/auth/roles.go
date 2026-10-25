package auth

import "sort"

// Kategorien der Berechtigungen. Sie bestimmen nur die Gruppierung in der
// Oberflaeche, die Texte kommen aus dem i18n-Katalog.
const (
	CategoryView    = "view"
	CategoryOperate = "operate"
	CategoryDesign  = "design"
	CategorySetup   = "setup"
	CategoryUpdates = "updates"
	CategorySystem  = "system"
)

// Ansehen-Berechtigungen: jede Seite hat eine eigene.
const (
	RoleViewOverview    = "view_overview"
	RoleViewDevices     = "view_devices"
	RoleViewHistory     = "view_history"
	RoleViewStats       = "view_stats"
	RoleViewEnergy      = "view_energy"
	RoleViewDeviceMap   = "view_device_map"
	RoleViewConfig      = "view_config"
	RoleViewAutomations = "view_automations"
	RoleViewDiagnostics = "view_diagnostics"
)

// Bedienen, Gestalten, Einrichten, Updates und System.
const (
	RoleOperateDevices     = "operate_devices"
	RoleOperateAutomations = "operate_automations"
	RoleEditSettings       = "edit_settings"
	RoleEditConfig         = "edit_config"
	RoleInstallUpdates     = "install_updates"
	RoleUpdateSettings     = "update_settings"
	RoleViewLogs           = "view_logs"
	RoleTailscale          = "tailscale"
	RoleUserManagement     = "user_management"
	RoleHTTPLogin          = "http_login"
)

// RoleInfo beschreibt eine Berechtigung im Katalog. Requires ist die
// Ansehen-Berechtigung, ohne die Key nicht gilt, sonst leer. HAAllowed ist nur
// fuer http_login false.
type RoleInfo struct {
	Key          string
	Category     string
	Requires     string
	HTTPSOnly    bool
	GuestAllowed bool
	HAAllowed    bool
	// Elevated markiert die erhöhten Berechtigungen, die die Oberfläche
	// hervorhebt.
	Elevated bool
}

// roleCatalog steht in Anzeigereihenfolge, wie die Tabelle in der Spec,
// Abschnitt 1.
var roleCatalog = []RoleInfo{
	{Key: RoleViewOverview, Category: CategoryView, GuestAllowed: true, HAAllowed: true},
	{Key: RoleViewDevices, Category: CategoryView, GuestAllowed: true, HAAllowed: true},
	{Key: RoleViewHistory, Category: CategoryView, GuestAllowed: true, HAAllowed: true},
	{Key: RoleViewStats, Category: CategoryView, GuestAllowed: true, HAAllowed: true},
	{Key: RoleViewEnergy, Category: CategoryView, GuestAllowed: true, HAAllowed: true},
	{Key: RoleViewDeviceMap, Category: CategoryView, GuestAllowed: true, HAAllowed: true},
	{Key: RoleViewConfig, Category: CategoryView, GuestAllowed: true, HAAllowed: true},
	{Key: RoleViewAutomations, Category: CategoryView, GuestAllowed: true, HAAllowed: true},
	{Key: RoleViewDiagnostics, Category: CategoryView, GuestAllowed: true, HAAllowed: true},
	{Key: RoleOperateDevices, Category: CategoryOperate, Requires: RoleViewDevices, GuestAllowed: true, HAAllowed: true},
	{Key: RoleOperateAutomations, Category: CategoryOperate, Requires: RoleViewAutomations, GuestAllowed: true, HAAllowed: true},
	{Key: "edit_layout", Category: CategoryDesign, Requires: RoleViewOverview, GuestAllowed: true, HAAllowed: true},
	{Key: "edit_device_map_view", Category: CategoryDesign, Requires: RoleViewDeviceMap, GuestAllowed: true, HAAllowed: true},
	{Key: RoleEditSettings, Category: CategoryDesign, GuestAllowed: true, HAAllowed: true},
	{Key: "automations", Category: CategorySetup, Requires: RoleViewAutomations, GuestAllowed: true, HAAllowed: true},
	{Key: "edit_energy", Category: CategorySetup, Requires: RoleViewEnergy, GuestAllowed: true, HAAllowed: true},
	{Key: "edit_device_map", Category: CategorySetup, Requires: RoleViewDeviceMap, GuestAllowed: true, HAAllowed: true},
	{Key: RoleEditConfig, Category: CategorySetup, Requires: RoleViewConfig, GuestAllowed: true, HAAllowed: true},
	{Key: "delete_device_discovery", Category: CategorySetup, Requires: RoleViewDevices, GuestAllowed: true, HAAllowed: true},
	{Key: "mqtt_config", Category: CategorySetup, HTTPSOnly: true, HAAllowed: true, Elevated: true},
	{Key: "tune_live_updates", Category: CategorySetup, GuestAllowed: true, HAAllowed: true},
	{Key: "check_updates", Category: CategoryUpdates, GuestAllowed: true, HAAllowed: true},
	{Key: RoleInstallUpdates, Category: CategoryUpdates, HAAllowed: true},
	{Key: RoleUpdateSettings, Category: CategoryUpdates, HTTPSOnly: true, HAAllowed: true, Elevated: true},
	{Key: RoleViewLogs, Category: CategorySystem, Requires: RoleViewDiagnostics, HTTPSOnly: true, HAAllowed: true},
	{Key: "system_actions", Category: CategorySystem, HTTPSOnly: true, HAAllowed: true, Elevated: true},
	{Key: RoleTailscale, Category: CategorySystem, HTTPSOnly: true, HAAllowed: true, Elevated: true},
	{Key: RoleUserManagement, Category: CategorySystem, HTTPSOnly: true, HAAllowed: true, Elevated: true},
	{Key: RoleHTTPLogin, Category: CategorySystem},
}

// Roles liefert eine Kopie des Katalogs in Anzeigereihenfolge.
func Roles() []RoleInfo {
	out := make([]RoleInfo, len(roleCatalog))
	copy(out, roleCatalog)
	return out
}

// AllRoles liefert alle Schluessel in Katalogreihenfolge.
func AllRoles() []string {
	out := make([]string, 0, len(roleCatalog))
	for _, info := range roleCatalog {
		out = append(out, info.Key)
	}
	return out
}

// RoleByKey sucht eine Berechtigung im Katalog.
func RoleByKey(key string) (RoleInfo, bool) {
	for _, info := range roleCatalog {
		if info.Key == key {
			return info, true
		}
	}
	return RoleInfo{}, false
}

// ValidRole meldet, ob der Schluessel im Katalog steht.
func ValidRole(key string) bool {
	_, ok := RoleByKey(key)
	return ok
}

// Normalize dedupliziert, entfernt unbekannte Schluessel und Berechtigungen
// ohne ihre Voraussetzung und sortiert das Ergebnis.
func Normalize(roles []string) []string {
	present := make(map[string]bool, len(roles))
	for _, key := range roles {
		if ValidRole(key) {
			present[key] = true
		}
	}
	out := make([]string, 0, len(present))
	for key := range present {
		info, _ := RoleByKey(key)
		if info.Requires != "" && !present[info.Requires] {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
