package auth

import "errors"

// usersFileVersion ist die Version, die users.json heute schreibt. Eine
// Datei mit hoeherer Version wird nicht angefasst, siehe Spec Abschnitt 2.
const usersFileVersion = 2

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
		user.Roles = Normalize(roles)
		users[name] = user
	}
	// Schritt 4: die sechs bisherigen Gastrollen plus die Ergaenzungen aus
	// Schritt 1 und 2, nicht gastfaehige Schluessel fallen heraus.
	guest := append([]string(nil), legacyGuestRoles...)
	guest = append(guest, viewRoleKeys()...)
	guest = append(guest, RoleOperateDevices, RoleEditSettings, RoleEditConfig, RoleOperateAutomations)
	return Normalize(guestAllowedOnly(guest))
}

// defaultGuestRoles ist die Gastvorgabe einer neuen Installation: die
// Migration angewandt auf einen Gast mit der alten Rollenliste.
func defaultGuestRoles() []string {
	return migrateV1(map[string]User{"guest": {Username: "guest", Guest: true, Roles: legacyGuestRoles}})
}

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
