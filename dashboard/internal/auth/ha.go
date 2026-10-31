package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/companionpair"
)

// HAIdentity ist die Identität eines Home-Assistant-Benutzers, wie ihn der
// Verifier oder die Companion-Integration meldet.
type HAIdentity struct {
	UserID   string
	Username string
	Name     string
	Admin    bool
}

// HAVerifier prüft Zugangsdaten bei Home Assistant.
type HAVerifier interface {
	// VerifyHA liefert die Identität, ErrInvalidCredentials bei falschen
	// Zugangsdaten oder ErrHomeAssistantUnavailable.
	VerifyHA(ctx context.Context, username, password string) (HAIdentity, error)
}

// HAVerifyTimeout begrenzt eine Prüfung bei Home Assistant.
const HAVerifyTimeout = 5 * time.Second

// ErrHomeAssistantUnavailable meldet, dass Home Assistant die Prüfung nicht
// beantworten konnte.
var ErrHomeAssistantUnavailable = errors.New("home assistant unavailable")

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

// SetHAVerifier setzt den Verifier für Anmeldungen über Home Assistant. nil
// schaltet die Anmeldung über Home Assistant ab.
func (m *Manager) SetHAVerifier(v HAVerifier) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.haVerifier = v
}

// CompanionSession startet eine Sitzung für einen HA-Benutzer, der über die
// Companion-Kopplung kommt. Konto und Felder werden nachgeführt.
func (m *Manager) CompanionSession(id HAIdentity) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.companion == nil {
		return Session{}, ErrNotPaired
	}
	nowValue := m.now().UTC()
	account := m.upsertHAAccountLocked(id, nowValue)
	if account.Disabled {
		return Session{}, ErrAccountDisabled
	}
	account.LastLoginAt = nowValue
	m.haAccounts[id.UserID] = account
	if err := m.saveLocked(); err != nil {
		return Session{}, err
	}
	return m.newSessionViaLocked(m.haUserLocked(account), false, ViaCompanion)
}

// HAUserChanged übernimmt eine Änderung eines HA-Benutzers. Ist er inaktiv
// oder entfernt, fallen alle seine Sitzungen weg, das Konto bleibt. Sonst
// werden die HA-Sitzungen neu berechnet, damit ein geänderter Admin-Status
// sofort wirkt. Unbekannte Konten bleiben unberührt.
func (m *Manager) HAUserChanged(id HAIdentity, active, removed bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.haAccounts[id.UserID]
	if !ok {
		return nil
	}
	updated := current
	// Bei einer Entfernung liefert HA keine Angaben mehr: das gespeicherte
	// Konto bleibt, es werden nur die Sitzungen beendet.
	if !removed {
		updated.Username = id.Username
		updated.DisplayName = id.Name
		updated.HAAdmin = id.Admin
	}
	dropSessions := removed || !active
	changed := !reflect.DeepEqual(updated, current) || (dropSessions && m.hasHASessionLocked(id.UserID))
	if !changed {
		return nil
	}
	accounts := copyHAAccounts(m.haAccounts)
	accounts[id.UserID] = updated
	oldAccounts, oldSessions := m.haAccounts, m.sessions
	m.haAccounts = accounts
	m.sessions = copySessions(m.sessions)
	m.refreshHASessionsLocked()
	if dropSessions {
		for key, session := range m.sessions {
			if session.User.Source == SourceHomeAssistant && session.User.HAUserID == id.UserID {
				delete(m.sessions, key)
			}
		}
	}
	if err := m.saveLocked(); err != nil {
		m.haAccounts, m.sessions = oldAccounts, oldSessions
		return err
	}
	return m.saveSessionsLocked()
}

// hasHASessionLocked meldet, ob das HA-Konto eine Sitzung hat.
func (m *Manager) hasHASessionLocked(haUserID string) bool {
	for _, session := range m.sessions {
		if session.User.Source == SourceHomeAssistant && session.User.HAUserID == haUserID {
			return true
		}
	}
	return false
}

// upsertHAAccountLocked liefert das HA-Konto für id: das gespeicherte, mit
// nachgeführtem Namen, Anzeigenamen und Admin-Status, oder ein neues mit den
// Vorgaben. Gespeichert wird nicht, das macht der Aufrufer.
func (m *Manager) upsertHAAccountLocked(id HAIdentity, nowValue time.Time) HAAccount {
	account, ok := m.haAccounts[id.UserID]
	if !ok {
		account = HAAccount{HAUserID: id.UserID, RolesCustom: false, Roles: []string{}, CreatedAt: nowValue}
	}
	account.Username = id.Username
	account.DisplayName = id.Name
	account.HAAdmin = id.Admin
	return account
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
	var status CompanionStatus
	if m.companion != nil {
		pairedAt := m.companion.PairedAt
		lastSeen := m.companion.LastSeenAt
		status = CompanionStatus{Paired: true, PairedAt: &pairedAt, LastSeenAt: &lastSeen}
	}
	if m.pairing != nil && m.now().Before(m.pairing.expiresAt) {
		expiresAt := m.pairing.expiresAt
		status.PairingExpiresAt = &expiresAt
	}
	return status
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

// pendingPairing ist der offene Kopplungscode. Er lebt nur im Speicher.
type pendingPairing struct {
	code      string
	expiresAt time.Time
	failures  int
}

// maxPairingFailures ist die Zahl der Fehlversuche, nach denen der Code verfällt.
const maxPairingFailures = 5

// CreatePairingCode erzeugt einen Kopplungscode und ersetzt einen offenen.
// Der Code wird nur formatiert zurückgegeben und nie gespeichert.
func (m *Manager) CreatePairingCode(actor string) (code string, expiresAt time.Time, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, err := companionpair.NewCode()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = m.now().UTC().Add(PairingCodeLifetime)
	m.pairing = &pendingPairing{code: raw, expiresAt: expiresAt}
	log.Printf("auth: %s %s %s", actor, "pairing_code", "companion")
	return companionpair.Format(raw), expiresAt, nil
}

// DropPairingCode verwirft einen offenen Kopplungscode.
func (m *Manager) DropPairingCode(actor string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pairing = nil
}

// Pair koppelt die Companion-Integration. check bekommt den normalisierten
// Code und prüft den Nachweis der Gegenseite. Jeder Fehlschlag zählt im
// Begrenzer und am Code; beim fünften verfällt der Code.
func (m *Manager) Pair(check func(code string) bool) (token, code string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if !m.pairLimiter.allowed("", now) {
		return "", "", ErrPairingInvalid
	}
	pending := m.pairing
	if pending != nil && !now.Before(pending.expiresAt) {
		m.pairing = nil
		pending = nil
	}
	if pending == nil {
		m.pairLimiter.fail("", now)
		return "", "", ErrPairingInvalid
	}
	code = companionpair.Normalize(pending.code)
	if !check(code) {
		m.pairLimiter.fail("", now)
		pending.failures++
		if pending.failures >= maxPairingFailures {
			m.pairing = nil
		}
		return "", "", ErrPairingInvalid
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token = hex.EncodeToString(buf)

	oldCompanion, oldRoles, oldSessions, oldPairing := m.companion, m.haRoles, m.sessions, m.pairing
	m.companion = &companionFile{Label: "Home Assistant", TokenHash: tokenHash(token), PairedAt: now.UTC(), LastSeenAt: now.UTC()}
	if m.haRoles == nil {
		m.haRoles = &haRolesFile{Admin: defaultHAAdminRoles(), User: defaultHAUserRoles()}
	}
	m.pairing = nil
	m.sessions = withoutHASessions(m.sessions)
	if err := m.saveLocked(); err != nil {
		m.companion, m.haRoles, m.sessions, m.pairing = oldCompanion, oldRoles, oldSessions, oldPairing
		return "", "", err
	}
	if err := m.saveSessionsLocked(); err != nil {
		return "", "", err
	}
	log.Printf("auth: %s %s %s", "companion", "pair", "home_assistant")
	return token, code, nil
}

// Unpair hebt die Kopplung auf. HA-Konten und Vorgaben bleiben.
func (m *Manager) Unpair(actor string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.companion == nil {
		return ErrNotPaired
	}
	oldCompanion, oldSessions, oldPairing := m.companion, m.sessions, m.pairing
	m.companion, m.pairing = nil, nil
	m.sessions = withoutHASessions(m.sessions)
	if err := m.saveLocked(); err != nil {
		m.companion, m.sessions, m.pairing = oldCompanion, oldSessions, oldPairing
		return err
	}
	if err := m.saveSessionsLocked(); err != nil {
		return err
	}
	log.Printf("auth: %s %s %s", actor, "unpair", "home_assistant")
	return nil
}

// CompanionTokenValid prüft ein Token der Kopplung. Ohne Kopplung ist jedes falsch.
func (m *Manager) CompanionTokenValid(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.companion == nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(m.companion.TokenHash), []byte(tokenHash(token))) == 1
}

// CompanionSeen schreibt den Zeitpunkt des letzten Kontakts der Companion-Seite.
func (m *Manager) CompanionSeen() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.companion == nil {
		return ErrNotPaired
	}
	old := m.companion
	updated := *old
	updated.LastSeenAt = m.now().UTC()
	m.companion = &updated
	if err := m.saveLocked(); err != nil {
		m.companion = old
		return err
	}
	return nil
}

// withoutHASessions liefert eine Kopie der Sitzungen ohne HA-Sitzungen.
func withoutHASessions(sessions map[string]Session) map[string]Session {
	out := make(map[string]Session, len(sessions))
	for key, session := range sessions {
		if session.User.Source != SourceHomeAssistant {
			out[key] = session
		}
	}
	return out
}
