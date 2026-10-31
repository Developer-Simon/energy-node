package auth

import (
	"errors"
	"reflect"
)

// usersFileVersion ist die Version, die users.json heute schreibt. Eine
// Datei mit hoeherer Version wird nicht angefasst, siehe Spec Abschnitt 2.
const usersFileVersion = 5

// ErrUnsupportedUsersVersion meldet eine users.json aus einer neueren Version
// des Dashboards. Der Start bricht ab, die Datei bleibt unveraendert.
var ErrUnsupportedUsersVersion = errors.New("users: unsupported file version")

// legacyGuestRoles sind die sechs Gastrollen vor Version 2. Sie gehen in die
// explizite Gastvorgabe der Migration ein (Spec Abschnitt 2, Schritt 4).
var legacyGuestRoles = []string{RoleAutomations, RoleEditLayout, RoleCheckUpdates, RoleEditEnergy, RoleEditDeviceMap, RoleEditDeviceMapView}

// viewRoleKeys liefert die Ansehen-Berechtigungen in Katalogreihenfolge.
func viewRoleKeys() []string {
	var out []string
	for _, info := range roleCatalog {
		if info.Category == CategoryView {
			out = append(out, info.Key)
		}
	}
	return out
}

// migrateV1 bringt die Konten einer Datei der Version 1 nach Version 2 und
// liefert die Gastvorgabe. Die Schritte stehen in Spec Abschnitt 2, sie
// laufen einmal beim Uebergang und nie wieder.
func migrateV1(users map[string]User) []string {
	for name, user := range users {
		roles := append([]string(nil), user.Roles...)
		// Schritt 1: jedes Konto, auch jeder Gast.
		roles = append(roles, viewRoleKeys()...)
		roles = append(roles, RoleOperateDevices, RoleEditSettings, RoleEditConfig)
		// Schritt 2: wer automations hat, bedient sie auch.
		if containsRole(roles, RoleAutomations) {
			roles = append(roles, RoleOperateAutomations)
		}
		// Schritt 3: Nicht-Gast mit system_actions.
		if !user.Guest && containsRole(user.Roles, RoleSystemActions) {
			roles = append(roles, RoleInstallUpdates, RoleUpdateSettings, RoleTailscale, RoleViewLogs, RoleUserManagement)
		}
		// Schritt 5 (Version 3): Einrichten trennen, siehe migrateV2.
		user.Roles = Normalize(migrateV3(migrateV2(roles, user.Guest)))
		users[name] = user
	}
	// Schritt 4: die sechs bisherigen Gastrollen plus die Ergaenzungen aus
	// Schritt 1 und 2, nicht gastfaehige Schluessel fallen heraus.
	guest := append([]string(nil), legacyGuestRoles...)
	guest = append(guest, viewRoleKeys()...)
	guest = append(guest, RoleOperateDevices, RoleEditSettings, RoleEditConfig, RoleOperateAutomations)
	return Normalize(guestAllowedOnly(migrateV3(migrateV2(guest, true))))
}

// migrateV2 bringt die Rollen einer Datei der Version 2 nach Version 3: das
// Einrichten wird von tune_live_updates und edit_settings getrennt (Spec
// Abschnitt 2, Version 3). Die Eingabe sind die rohen Rollen, vor Normalize,
// weil Normalize den entfernten Schluessel tune_live_updates verwerfen wuerde.
// Niemand verliert ein Recht: wer tune_live_updates oder edit_settings hat,
// bekommt edit_setup; wer edit_config hat und kein Gast ist, bekommt
// setup_tiny_tuya. Die Pruefung auf edit_config nimmt die wirksamen Rollen,
// eine ohne view_config verworfene edit_config zaehlt also nicht.
func migrateV2(roles []string, guest bool) []string {
	out := make([]string, 0, len(roles)+2)
	hasSetup := false
	for _, key := range roles {
		if key == "tune_live_updates" {
			hasSetup = true
			continue
		}
		if key == RoleEditSettings {
			hasSetup = true
		}
		out = append(out, key)
	}
	if hasSetup {
		out = append(out, RoleEditSetup)
	}
	if !guest && containsRole(Normalize(roles), RoleEditConfig) {
		out = append(out, RoleSetupTinyTuya)
	}
	return out
}

// migrateV3 bringt die Geraete-Rechte einer Datei der Version 3 nach Version 4:
// wer view_devices hat, bekommt view_device_analysis, wer edit_layout hat,
// bekommt edit_device_appearance. Die Eingabe sind rohe Rollen, es gibt kein
// Normalize, die Aufrufer normalisieren selbst.
func migrateV3(roles []string) []string {
	out := append([]string(nil), roles...)
	if containsRole(roles, RoleViewDevices) {
		out = append(out, RoleViewDeviceAnalysis)
	}
	if containsRole(roles, RoleEditLayout) {
		out = append(out, RoleEditDeviceAppearance)
	}
	return out
}

// legacyDefaultGuestRoles ist die bisherige Gastvorgabe: die Migration
// angewandt auf einen Gast mit der alten Rollenliste. Version 5 erkennt daran
// unveraenderte Vorgaben (migrateV4Locked).
func legacyDefaultGuestRoles() []string {
	return migrateV1(map[string]User{"guest": {Username: "guest", Guest: true, Roles: legacyGuestRoles}})
}

// defaultGuestRoles ist die Gastvorgabe einer neuen Installation: nur ansehen,
// keine Bearbeitung. Normalisiert, wie die Vorgabe nach einem Neustart aus
// users.json kommt, damit Migration und Neuinstallation gleich vergleichen.
func defaultGuestRoles() []string {
	return Normalize([]string{RoleViewDevices, RoleViewDeviceMap, RoleViewEnergy, RoleViewHistory, RoleViewOverview, RoleViewStats})
}

// defaultHAUserRoles ist die Vorgabe fuer HA-Konten ohne HA-Admin: Ansehen und
// Bedienen, dazu Gestalten, Einstellungen und Update-Pruefung, kein Einrichten.
func defaultHAUserRoles() []string {
	return Normalize([]string{
		RoleViewOverview, RoleViewDevices, RoleViewDeviceAnalysis, RoleViewHistory, RoleViewStats,
		RoleViewEnergy, RoleViewDeviceMap, RoleViewConfig, RoleViewAutomations, RoleViewDiagnostics,
		RoleOperateDevices, RoleOperateAutomations, RoleEditLayout, RoleEditDeviceAppearance,
		RoleEditDeviceMapView, RoleEditSettings, RoleCheckUpdates,
	})
}

// defaultHAAdminRoles ist die Vorgabe fuer HA-Konten mit HA-Admin: alle
// HA-tauglichen Berechtigungen ohne die erhoehten.
func defaultHAAdminRoles() []string {
	var out []string
	for _, info := range roleCatalog {
		if info.HAAllowed && !info.Elevated {
			out = append(out, info.Key)
		}
	}
	return Normalize(out)
}

// migrateV4Locked wendet die Regeln der Version 5 an: eine unveraenderte
// Gastvorgabe (genau die alte Standardliste) wird durch die neue ersetzt.
// Gastkonten mit genau der alten Liste bekommen die neue Vorgabe, unabhaengig
// davon, ob die Gastvorgabe ersetzt wurde. Die HA-Vorgaben werden nur
// gemeinsam ersetzt, wenn beide Seiten unveraendert sind. Eigene HA-Konten
// bleiben unberuehrt. Liefert, ob die Gastvorgabe ersetzt wurde.
func (m *Manager) migrateV4Locked() (guestReplaced bool) {
	legacy := legacyDefaultGuestRoles()
	if sameRoles(m.guestRoles, legacy) {
		m.guestRoles = defaultGuestRoles()
		guestReplaced = true
	}
	for name, user := range m.users {
		if user.Guest && sameRoles(user.Roles, legacy) {
			user.Roles = defaultGuestRoles()
			m.users[name] = user
		}
	}
	if m.haRoles != nil && sameRoles(m.haRoles.Admin, legacy) && sameRoles(m.haRoles.User, legacy) {
		m.haRoles.Admin = defaultHAAdminRoles()
		m.haRoles.User = defaultHAUserRoles()
	}
	return guestReplaced
}

// sameRoles vergleicht zwei Rollenlisten nach Normalize, die Reihenfolge und
// doppelte oder ungueltige Schluessel zaehlen nicht.
func sameRoles(a, b []string) bool {
	return reflect.DeepEqual(Normalize(a), Normalize(b))
}

// AdminRoles liefert die Rechte des Kontos aus auth.pw (alle ausser http_login).
func AdminRoles() []string { return bootstrapAdminRoles() }

// bootstrapAdminRoles sind alle Berechtigungen ausser http_login.
func bootstrapAdminRoles() []string {
	var out []string
	for _, key := range AllRoles() {
		if key != RoleHTTPLogin {
			out = append(out, key)
		}
	}
	return out
}

// guestAllowedOnly behaelt nur die Schluessel, die fuer Gaeste erlaubt sind.
func guestAllowedOnly(roles []string) []string {
	out := make([]string, 0, len(roles))
	for _, key := range roles {
		if info, ok := RoleByKey(key); ok && info.GuestAllowed {
			out = append(out, key)
		}
	}
	return out
}

// MigrationNotice meldet, ob der Hinweis zur Gastvorgabe noch offen ist. Er
// gilt nach einer Migration bis zum ersten SetGuestRoles.
func (m *Manager) MigrationNotice() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.guestRolesReviewed
}
