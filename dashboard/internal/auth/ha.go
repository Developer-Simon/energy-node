package auth

import (
	"log"
	"sort"
	"strings"
	"time"
)

// HAAccount ist ein Konto, das Home Assistant bereitstellt. Es hat keinen
// lokalen Zugang: Anmeldung läuft über Home Assistant, die Berechtigungen
// kommen aus RolesCustom oder aus den Vorgaben (ha_roles).
type HAAccount struct {
	HAUserID    string    `json:"ha_user_id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	HAAdmin     bool      `json:"ha_admin"`
	RolesCustom bool      `json:"roles_custom"`
	Roles       []string  `json:"roles"`
	Disabled    bool      `json:"disabled"`
	CreatedAt   time.Time `json:"created_at"`
	LastLoginAt time.Time `json:"last_login_at"`
	// Nur in der Ausgabe von HAAccounts: ein lokales Konto trägt denselben Namen.
	NameConflict bool `json:"name_conflict,omitempty"`
}

// CompanionStatus beschreibt die Kopplung zur Companion-Integration. Der
// offene Kopplungscode ist nicht Teil davon, außer PairingExpiresAt.
type CompanionStatus struct {
	Paired           bool       `json:"paired"`
	PairedAt         *time.Time `json:"paired_at,omitempty"`
	LastSeenAt       *time.Time `json:"last_seen_at,omitempty"`
	PairingExpiresAt *time.Time `json:"pairing_expires_at,omitempty"`
}

// HAAccounts liefert die HA-Konten sortiert nach Name, dann nach HAUserID.
func (m *Manager) HAAccounts() []HAAccount {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]HAAccount, 0, len(m.haAccounts))
	for _, account := range m.haAccounts {
		account.Roles = append([]string{}, account.Roles...)
		_, account.NameConflict = m.users[strings.ToLower(account.Username)]
		out = append(out, account)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Username != out[j].Username {
			return out[i].Username < out[j].Username
		}
		return out[i].HAUserID < out[j].HAUserID
	})
	return out
}

// HARoles liefert die Vorgaben für HA-Konten. set ist false, solange ha_roles
// in users.json fehlt.
func (m *Manager) HARoles() (admin, user []string, set bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.haRoles == nil {
		return nil, nil, false
	}
	return append([]string{}, m.haRoles.Admin...), append([]string{}, m.haRoles.User...), true
}

// SetHARoles setzt die Vorgaben für HA-Konten. http_login ist für sie nicht
// erlaubt. Bestehende Sitzungen von HA-Konten ohne eigene Rollen bekommen die
// neuen Vorgaben sofort.
func (m *Manager) SetHARoles(actor string, admin, user []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	adminKeys, err := validateHARoles(admin)
	if err != nil {
		return err
	}
	userKeys, err := validateHARoles(user)
	if err != nil {
		return err
	}
	if err := m.commitHALocked(m.haAccounts, &haRolesFile{Admin: adminKeys, User: userKeys}); err != nil {
		return err
	}
	log.Printf("auth: %s %s %s", actor, "ha_roles", "homeassistant")
	return nil
}

// UpdateHAAccount ändert Sperre und Rollen eines HA-Kontos. Ohne rolesCustom
// gelten die Vorgaben, die gespeicherten Rollen werden dann geleert. Mit
// rolesCustom und einer leeren Liste startet das Konto mit der Vorgabe.
func (m *Manager) UpdateHAAccount(actor, haUserID string, roles []string, disabled, rolesCustom bool) (HAAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.haAccounts[haUserID]
	if !ok {
		return HAAccount{}, ErrUserNotFound
	}
	normalized, err := validateHARoles(roles)
	if err != nil {
		return HAAccount{}, err
	}
	updated := current
	updated.Disabled = disabled
	updated.RolesCustom = rolesCustom
	updated.Roles = []string{}
	if rolesCustom {
		updated.Roles = normalized
		if len(normalized) == 0 {
			updated.Roles = Normalize(m.haPresetLocked(current.HAAdmin))
		}
	}
	accounts := copyHAAccounts(m.haAccounts)
	accounts[haUserID] = updated
	if err := m.commitHALocked(accounts, m.haRoles); err != nil {
		return HAAccount{}, err
	}
	log.Printf("auth: %s %s %s", actor, "update_ha", updated.Username)
	return updated, nil
}

// DeleteHAAccount entfernt ein HA-Konto samt seinen Sitzungen.
func (m *Manager) DeleteHAAccount(actor, haUserID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.haAccounts[haUserID]
	if !ok {
		return ErrUserNotFound
	}
	accounts := copyHAAccounts(m.haAccounts)
	delete(accounts, haUserID)
	if err := m.commitHALocked(accounts, m.haRoles); err != nil {
		return err
	}
	log.Printf("auth: %s %s %s", actor, "delete_ha", current.Username)
	return nil
}

// CompanionStatus liefert den Stand der Kopplung. Ohne Kopplung ist Paired false.
func (m *Manager) CompanionStatus() CompanionStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.companion == nil {
		return CompanionStatus{}
	}
	pairedAt := m.companion.PairedAt
	lastSeen := m.companion.LastSeenAt
	return CompanionStatus{Paired: true, PairedAt: &pairedAt, LastSeenAt: &lastSeen}
}

// haUserLocked liefert den wirksamen Benutzer eines HA-Kontos. Die Rollen sind
// die eigenen bei RolesCustom, sonst die Vorgabe. http_login fällt immer weg.
func (m *Manager) haUserLocked(a HAAccount) User {
	roles := a.Roles
	if !a.RolesCustom {
		roles = m.haPresetLocked(a.HAAdmin)
	}
	kept := make([]string, 0, len(roles))
	for _, key := range roles {
		if key != RoleHTTPLogin {
			kept = append(kept, key)
		}
	}
	return User{
		Username:    a.Username,
		Roles:       Normalize(kept),
		Source:      SourceHomeAssistant,
		HAUserID:    a.HAUserID,
		DisplayName: a.DisplayName,
		Disabled:    a.Disabled,
		CreatedAt:   a.CreatedAt,
		LastLoginAt: a.LastLoginAt,
	}
}

// haPresetLocked liefert die Vorgabe für HA-Admins oder die übrigen HA-Konten.
func (m *Manager) haPresetLocked(admin bool) []string {
	if m.haRoles == nil {
		return nil
	}
	if admin {
		return m.haRoles.Admin
	}
	return m.haRoles.User
}

// refreshHASessionsLocked rechnet die Sitzungen von HA-Konten neu aus den
// HA-Konten. Fehlt ein Konto oder ist es gesperrt, fällt die Sitzung weg.
func (m *Manager) refreshHASessionsLocked() {
	for key, session := range m.sessions {
		if session.User.Source != SourceHomeAssistant {
			continue
		}
		account, ok := m.haAccounts[session.User.HAUserID]
		if !ok || account.Disabled {
			delete(m.sessions, key)
			continue
		}
		session.User = m.haUserLocked(account)
		m.sessions[key] = session
	}
}

// commitHALocked übernimmt die neuen HA-Konten und Vorgaben, rechnet die
// Sitzungen neu und speichert. Schlägt das Speichern fehl, bleibt der alte Stand.
// Der Aufrufer hält m.mu.
func (m *Manager) commitHALocked(accounts map[string]HAAccount, roles *haRolesFile) error {
	oldAccounts, oldRoles, oldSessions := m.haAccounts, m.haRoles, m.sessions
	m.haAccounts, m.haRoles, m.sessions = accounts, roles, copySessions(m.sessions)
	m.refreshHASessionsLocked()
	if err := m.saveLocked(); err != nil {
		m.haAccounts, m.haRoles, m.sessions = oldAccounts, oldRoles, oldSessions
		return err
	}
	return m.saveSessionsLocked()
}

func copyHAAccounts(accounts map[string]HAAccount) map[string]HAAccount {
	out := make(map[string]HAAccount, len(accounts))
	for id, account := range accounts {
		out[id] = account
	}
	return out
}

// validateHARoles prüft Rollen für HA-Konten: unbekannte Schlüssel und
// http_login werden abgewiesen, das Ergebnis ist normalisiert.
func validateHARoles(roles []string) ([]string, error) {
	for _, key := range roles {
		if key == RoleHTTPLogin {
			return nil, ErrHARoleNotAllowed
		}
		if !ValidRole(key) {
			return nil, ErrUnknownRole
		}
	}
	return Normalize(roles), nil
}
