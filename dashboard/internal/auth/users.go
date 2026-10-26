package auth

import (
	"errors"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	// ErrLastAdmin meldet eine Änderung, die das letzte aktive lokale Konto mit
	// user_management entfernen würde.
	ErrLastAdmin = errors.New("last admin cannot be deleted, disabled or stripped of user_management")
	// ErrSelfProtection meldet den Versuch, das eigene Konto zu löschen oder zu sperren.
	ErrSelfProtection = errors.New("own account cannot be deleted or disabled")
	// ErrInvalidUsername meldet einen Benutzernamen außerhalb der Regeln (3 bis 32
	// Zeichen aus [a-z0-9._-], Präfix guest- reserviert).
	ErrInvalidUsername = errors.New("invalid username")
	// ErrWeakPassword meldet ein Passwort unter der Mindestlänge. Der Fehler
	// aus validatePassword ist ein PasswordTooShortError, der die Grenze nennt.
	ErrWeakPassword = errors.New("password is too short")
	// ErrInvalidPasswordPolicy meldet eine Mindestlänge außerhalb von 8 bis 64.
	ErrInvalidPasswordPolicy = errors.New("minimum password length out of range")
	// ErrUserExists meldet einen Benutzernamen, der ohne Rücksicht auf Groß- und
	// Kleinschreibung schon vergeben ist.
	ErrUserExists = errors.New("username already exists")
	// ErrUserNotFound meldet ein unbekanntes Konto.
	ErrUserNotFound = errors.New("user not found")
	// ErrUnknownRole meldet einen Berechtigungsschlüssel, den der Katalog nicht kennt.
	ErrUnknownRole = errors.New("unknown role")
	// ErrGuestRoleNotAllowed meldet eine Gastvorgabe mit einer Berechtigung, die
	// GuestAllowed nicht hat (wird in Stufe 1b genutzt).
	ErrGuestRoleNotAllowed = errors.New("role is not allowed for guests")
	// ErrHTTPPasswordReused meldet ein HTTP-Passwort, das dem Hauptpasswort gleicht
	// (wird in Stufe 1b genutzt).
	ErrHTTPPasswordReused = errors.New("http password must differ from the main password")
	// ErrHTTPLoginRequired meldet ein HTTP-Passwort für ein Konto ohne http_login
	// (wird in Stufe 1b genutzt).
	ErrHTTPLoginRequired = errors.New("account needs the http_login role")
)

const (
	// DefaultMinPasswordLength gilt, solange users.json keine Mindestlänge
	// setzt, und nach dem Start ohne Eintrag.
	DefaultMinPasswordLength = 10
	// MinPasswordLengthLowest und MinPasswordLengthHighest sind die Grenzen,
	// in denen der Administrator die Mindestlänge setzen kann.
	MinPasswordLengthLowest  = 8
	MinPasswordLengthHighest = 64
)

// PasswordTooShortError meldet ein Passwort unter der gerade geltenden
// Mindestlänge. errors.Is(err, ErrWeakPassword) trifft ihn ebenfalls.
type PasswordTooShortError struct {
	MinLength int
}

func (e PasswordTooShortError) Error() string {
	return ErrWeakPassword.Error()
}

func (e PasswordTooShortError) Is(target error) bool {
	return target == ErrWeakPassword
}

var usernamePattern = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

// reservedUsernames sind Namen, die ein Festpfad der API belegt. Ein Konto mit
// diesem Namen würde den Pfad unter /api/v1/users/{name} verdecken.
var reservedUsernames = map[string]bool{"password-policy": true, "companion": true, "ha-roles": true}

// sessionIDLength is the length of the session ID: the first characters of the
// SHA-256 hash of the token, which is the key of the session map.
const sessionIDLength = 16

// SessionInfo beschreibt eine angemeldete Sitzung für die Sitzungsliste. Das
// Token selbst steht nicht darin, nur die ID.
type SessionInfo struct {
	ID         string    `json:"id"`
	Username   string    `json:"username"`
	Guest      bool      `json:"guest"`
	Restricted bool      `json:"restricted"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

// Users liefert die lokalen Konten sortiert nach Name, ohne Hashes.
func (m *Manager) Users() []User {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listLocked(false)
}

// Guests liefert die Gastkonten sortiert nach Name, ohne Hashes.
func (m *Manager) Guests() []User {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listLocked(true)
}

func (m *Manager) listLocked(guests bool) []User {
	out := []User{}
	for _, user := range m.users {
		if user.Guest == guests {
			out = append(out, publicUser(user))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out
}

// CreateUser legt ein lokales Konto an. Der Benutzername wird kleingeschrieben
// gespeichert, die Berechtigungen werden normalisiert.
func (m *Manager) CreateUser(actor, username, password string, roles []string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name, err := normalizeUsername(username)
	if err != nil {
		return User{}, err
	}
	if _, exists := m.users[name]; exists {
		return User{}, ErrUserExists
	}
	// Ein Name, den ein HA-Konto trägt, ist für lokale Konten gesperrt, damit
	// Sitzungen und Zuordnungen nie verwechselt werden können.
	for _, account := range m.haAccounts {
		if strings.EqualFold(account.Username, name) {
			return User{}, ErrUserExists
		}
	}
	if err := m.validatePasswordLocked(password); err != nil {
		return User{}, err
	}
	normalized, err := validateRoles(roles)
	if err != nil {
		return User{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}
	nowValue := m.now().UTC()
	user := User{Username: name, PasswordHash: hash, Roles: normalized, CreatedAt: nowValue, LastLoginAt: nowValue}
	users := copyUsers(m.users)
	users[name] = user
	if err := m.commitLocked(users, m.sessions); err != nil {
		return User{}, err
	}
	log.Printf("auth: %s %s %s", actor, "create", name)
	return publicUser(user), nil
}

// UpdateUser ändert Berechtigungen und Sperrstatus eines lokalen Kontos. Laufende
// Sessions bekommen die neuen Werte, bei einer Sperre werden sie verworfen.
// Gastkonten sind hier nicht erreichbar.
func (m *Manager) UpdateUser(actor, username string, roles []string, disabled bool) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := strings.ToLower(username)
	current, ok := m.users[name]
	if !ok || current.Guest {
		return User{}, ErrUserNotFound
	}
	if disabled && strings.EqualFold(actor, name) {
		return User{}, ErrSelfProtection
	}
	normalized, err := validateRoles(roles)
	if err != nil {
		return User{}, err
	}
	updated := current
	updated.Roles = normalized
	updated.Disabled = disabled
	if !containsRole(normalized, RoleHTTPLogin) {
		updated.HTTPPasswordHash = ""
	}
	users := copyUsers(m.users)
	users[name] = updated
	if err := checkLastAdmin(adminCount(m.users), adminCount(users)); err != nil {
		return User{}, err
	}
	sessions := copySessions(m.sessions)
	for key, session := range sessions {
		if session.User.Source != "" || session.User.Username != name {
			continue
		}
		// Ohne http_login gibt es keine eingeschränkte Sitzung mehr, sie wird
		// zusammen mit dem Entzug verworfen (Spec Abschnitt 4).
		if disabled || (session.Restricted && !containsRole(normalized, RoleHTTPLogin)) {
			delete(sessions, key)
			continue
		}
		session.User = updated
		sessions[key] = session
	}
	if err := m.commitLocked(users, sessions); err != nil {
		return User{}, err
	}
	log.Printf("auth: %s %s %s", actor, "update", name)
	return publicUser(updated), nil
}

// DeleteUser entfernt ein Konto, auch ein Gastkonto, samt seiner Sessions.
func (m *Manager) DeleteUser(actor, username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := strings.ToLower(username)
	if strings.EqualFold(actor, name) {
		return ErrSelfProtection
	}
	if _, ok := m.users[name]; !ok {
		return ErrUserNotFound
	}
	users := copyUsers(m.users)
	delete(users, name)
	if err := checkLastAdmin(adminCount(m.users), adminCount(users)); err != nil {
		return err
	}
	sessions := copySessions(m.sessions)
	for key, session := range sessions {
		if session.User.Source == "" && session.User.Username == name {
			delete(sessions, key)
		}
	}
	if err := m.commitLocked(users, sessions); err != nil {
		return err
	}
	log.Printf("auth: %s %s %s", actor, "delete", name)
	return nil
}

// Sessions liefert alle nicht abgelaufenen Sitzungen, sortiert nach Konto und
// Ablauf. Current markiert die Sitzung zu currentToken.
func (m *Manager) Sessions(currentToken string) []SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	currentKey := tokenHash(currentToken)
	out := []SessionInfo{}
	for key, session := range m.sessions {
		if !session.ExpiresAt.After(now) {
			continue
		}
		out = append(out, SessionInfo{
			ID:         key[:sessionIDLength],
			Username:   session.User.Username,
			Guest:      session.User.Guest,
			Restricted: session.Restricted,
			ExpiresAt:  session.ExpiresAt,
			Current:    key == currentKey,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Username != out[j].Username {
			return out[i].Username < out[j].Username
		}
		if !out[i].ExpiresAt.Equal(out[j].ExpiresAt) {
			return out[i].ExpiresAt.Before(out[j].ExpiresAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// EndSession beendet genau die Sitzung mit der ID aus Sessions. Die Sitzung
// currentToken lässt sich so nicht beenden, dafür ist die Abmeldung da.
// Schlägt das Speichern fehl, bleibt die Sitzung im Speicher beendet.
func (m *Manager) EndSession(actor, id, currentToken string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	key := ""
	for candidate, session := range m.sessions {
		if candidate[:sessionIDLength] == id && session.ExpiresAt.After(now) {
			key = candidate
			break
		}
	}
	if key == "" {
		return ErrUserNotFound
	}
	if key == tokenHash(currentToken) {
		return ErrSelfProtection
	}
	username := m.sessions[key].User.Username
	delete(m.sessions, key)
	if err := m.saveSessionsLocked(); err != nil {
		return err
	}
	log.Printf("auth: %s %s %s", actor, "end_session", username)
	return nil
}

// commitLocked übernimmt den neuen Stand und speichert ihn. Schlägt das Speichern
// der Konten fehl, bleibt der alte Stand. Ein Fehler beim Speichern der Sessions
// lässt den neuen Stand im Speicher stehen, weil die Konten dann schon gespeichert sind.
func (m *Manager) commitLocked(users map[string]User, sessions map[string]Session) error {
	oldUsers, oldSessions := m.users, m.sessions
	m.users, m.sessions = users, sessions
	if err := m.saveLocked(); err != nil {
		m.users, m.sessions = oldUsers, oldSessions
		return err
	}
	return m.saveSessionsLocked()
}

// adminCount zählt die aktiven lokalen Konten mit user_management.
func adminCount(users map[string]User) int {
	count := 0
	for _, user := range users {
		if !user.Guest && !user.Disabled && HasRole(user, RoleUserManagement) {
			count++
		}
	}
	return count
}

// checkLastAdmin verbietet eine Änderung, die den letzten Admin entfernt.
func checkLastAdmin(before, after int) error {
	if before > 0 && after == 0 {
		return ErrLastAdmin
	}
	return nil
}

func normalizeUsername(username string) (string, error) {
	name := strings.ToLower(username)
	if !usernamePattern.MatchString(name) || strings.HasPrefix(name, "guest-") || reservedUsernames[name] {
		return "", ErrInvalidUsername
	}
	return name, nil
}

// validatePasswordLocked prüft die Länge gegen die aktuelle Mindestlänge. Der
// Aufrufer hält m.mu.
func (m *Manager) validatePasswordLocked(password string) error {
	if utf8.RuneCountInString(password) < m.minPasswordLength {
		return PasswordTooShortError{MinLength: m.minPasswordLength}
	}
	return nil
}

// MinPasswordLength liefert die Mindestlänge für neue Passwörter.
func (m *Manager) MinPasswordLength() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.minPasswordLength
}

// SetMinPasswordLength setzt die Mindestlänge für neue Passwörter. Bestehende
// Passwörter bleiben unberührt. Werte außerhalb von 8 bis 64 werden abgewiesen.
func (m *Manager) SetMinPasswordLength(actor string, n int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n < MinPasswordLengthLowest || n > MinPasswordLengthHighest {
		return ErrInvalidPasswordPolicy
	}
	// Wie SetGuestRoles: die Länge steht in users.json und wird bei einem
	// Fehler beim Speichern zurückgesetzt.
	old := m.minPasswordLength
	m.minPasswordLength = n
	if err := m.saveLocked(); err != nil {
		m.minPasswordLength = old
		return err
	}
	log.Printf("auth: %s %s %s", actor, "password_policy", "passwords")
	return nil
}

func validateRoles(roles []string) ([]string, error) {
	for _, key := range roles {
		if !ValidRole(key) {
			return nil, ErrUnknownRole
		}
	}
	return Normalize(roles), nil
}

// publicUser liefert eine Kopie ohne Hashes.
func publicUser(user User) User {
	out := user
	out.PasswordHash = ""
	out.HTTPPasswordHash = ""
	out.HasHTTPPassword = user.HTTPPasswordHash != ""
	out.Roles = append([]string{}, user.Roles...)
	return out
}

func copyUsers(users map[string]User) map[string]User {
	out := make(map[string]User, len(users))
	for name, user := range users {
		out[name] = user
	}
	return out
}

func copySessions(sessions map[string]Session) map[string]Session {
	out := make(map[string]Session, len(sessions))
	for key, session := range sessions {
		out[key] = session
	}
	return out
}

// SetPassword setzt das Passwort eines lokalen Kontos durch einen Admin. Alle
// Sessions des Kontos werden verworfen.
func (m *Manager) SetPassword(actor, username, newPassword string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := strings.ToLower(username)
	current, ok := m.users[name]
	if !ok || current.Guest {
		return ErrUserNotFound
	}
	if err := m.validatePasswordLocked(newPassword); err != nil {
		return err
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	updated := current
	updated.PasswordHash = hash
	users := copyUsers(m.users)
	users[name] = updated
	sessions := copySessions(m.sessions)
	for key, session := range sessions {
		if session.User.Source == "" && session.User.Username == name {
			delete(sessions, key)
		}
	}
	if err := m.commitLocked(users, sessions); err != nil {
		return err
	}
	log.Printf("auth: %s %s %s", actor, "password", name)
	return nil
}

// SetHTTPPassword setzt oder entfernt (leeres Passwort) das HTTP-Passwort eines
// Kontos. Verworfen werden nur die eingeschränkten Sessions des Kontos.
func (m *Manager) SetHTTPPassword(actor, username, newPassword string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := strings.ToLower(username)
	changed, err := m.setHTTPPasswordLocked(name, newPassword, "")
	if err != nil || !changed {
		return err
	}
	log.Printf("auth: %s %s %s", actor, "http_password", name)
	return nil
}

// SetOwnHTTPPassword setzt das HTTP-Passwort des Kontos der Sitzung token. Die
// Regeln sind dieselben wie bei SetHTTPPassword.
func (m *Manager) SetOwnHTTPPassword(token, newPassword string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.liveSessionLocked(token)
	if !ok {
		return ErrInvalidCredentials
	}
	name := session.User.Username
	changed, err := m.setHTTPPasswordLocked(name, newPassword, tokenHash(token))
	if err != nil || !changed {
		return err
	}
	log.Printf("auth: %s %s %s", name, "http_password", name)
	return nil
}

// setHTTPPasswordLocked prüft und setzt das HTTP-Passwort und verwirft die
// eingeschränkten Sessions des Kontos außer der Sitzung keep (leer bei Admin-
// Änderungen). Ein leeres Passwort für ein Konto ohne http_login ist ein
// No-op: es gibt nichts zu entfernen, also wird nichts geschrieben und keine
// Session verworfen. changed sagt, ob ein Schreibvorgang stattfand.
func (m *Manager) setHTTPPasswordLocked(name, newPassword, keep string) (changed bool, err error) {
	current, ok := m.users[name]
	if !ok || current.Guest {
		return false, ErrUserNotFound
	}
	if !containsRole(current.Roles, RoleHTTPLogin) {
		if newPassword == "" {
			return false, nil
		}
		return false, ErrHTTPLoginRequired
	}
	updated := current
	if newPassword != "" {
		if err := m.validatePasswordLocked(newPassword); err != nil {
			return false, err
		}
		if verifyPassword(current.PasswordHash, newPassword) {
			return false, ErrHTTPPasswordReused
		}
		hash, err := hashPassword(newPassword)
		if err != nil {
			return false, err
		}
		updated.HTTPPasswordHash = hash
	} else {
		updated.HTTPPasswordHash = ""
	}
	users := copyUsers(m.users)
	users[name] = updated
	sessions := copySessions(m.sessions)
	for key, session := range sessions {
		if session.User.Source != "" || session.User.Username != name {
			continue
		}
		if session.Restricted && key != keep {
			delete(sessions, key)
			continue
		}
		session.User = updated
		sessions[key] = session
	}
	return true, m.commitLocked(users, sessions)
}

// ChangeOwnPassword ändert das Passwort des Kontos der Sitzung token nach
// Prüfung des alten Passworts. Die Sitzung token bleibt bestehen, alle anderen
// Sitzungen des Kontos werden verworfen.
func (m *Manager) ChangeOwnPassword(token, oldPassword, newPassword string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := tokenHash(token)
	session, ok := m.liveSessionLocked(token)
	if !ok {
		return ErrInvalidCredentials
	}
	name := session.User.Username
	current, ok := m.users[name]
	if !ok || current.Guest || current.PasswordHash == "" || !verifyPassword(current.PasswordHash, oldPassword) {
		return ErrInvalidCredentials
	}
	if err := m.validatePasswordLocked(newPassword); err != nil {
		return err
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	updated := current
	updated.PasswordHash = hash
	users := copyUsers(m.users)
	users[name] = updated
	sessions := copySessions(m.sessions)
	for other, candidate := range sessions {
		if candidate.User.Source == "" && candidate.User.Username == name && other != key {
			delete(sessions, other)
		}
	}
	kept := sessions[key]
	kept.User = updated
	sessions[key] = kept
	if err := m.commitLocked(users, sessions); err != nil {
		return err
	}
	log.Printf("auth: %s %s %s", name, "password", name)
	return nil
}

// liveSessionLocked liefert die nicht abgelaufene Sitzung zu token.
func (m *Manager) liveSessionLocked(token string) (Session, bool) {
	session, ok := m.sessions[tokenHash(token)]
	// Eine HA-Sitzung ändert keine lokalen Passwörter, auch nicht bei gleichem Namen.
	if !ok || !session.ExpiresAt.After(m.now()) || session.User.Source != "" {
		return Session{}, false
	}
	return session, true
}

// GuestRoles liefert die Gastvorgabe.
func (m *Manager) GuestRoles() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.guestRoles...)
}

// SetGuestRoles setzt die Gastvorgabe. Nur Berechtigungen mit GuestAllowed sind
// erlaubt. Die Vorgabe gilt für bestehende Gastkonten und Gastsitzungen sofort
// und hebt den Migrationshinweis auf.
func (m *Manager) SetGuestRoles(actor string, roles []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	normalized, err := validateRoles(roles)
	if err != nil {
		return err
	}
	for _, key := range roles {
		if info, _ := RoleByKey(key); !info.GuestAllowed {
			return ErrGuestRoleNotAllowed
		}
	}
	users := copyUsers(m.users)
	for name, user := range users {
		if user.Guest {
			user.Roles = append([]string{}, normalized...)
			users[name] = user
		}
	}
	sessions := copySessions(m.sessions)
	for key, session := range sessions {
		if session.User.Guest {
			session.User.Roles = append([]string{}, normalized...)
			sessions[key] = session
		}
	}
	// Eigener Ablauf statt commitLocked, weil die Gastvorgabe mit in users.json
	// steht und bei einem Fehler beim Speichern der Konten zurückgesetzt werden muss.
	oldRoles, oldReviewed := m.guestRoles, m.guestRolesReviewed
	oldUsers, oldSessions := m.users, m.sessions
	m.guestRoles, m.guestRolesReviewed = normalized, true
	m.users, m.sessions = users, sessions
	if err := m.saveLocked(); err != nil {
		m.guestRoles, m.guestRolesReviewed = oldRoles, oldReviewed
		m.users, m.sessions = oldUsers, oldSessions
		return err
	}
	if err := m.saveSessionsLocked(); err != nil {
		return err
	}
	log.Printf("auth: %s %s %s", actor, "guest_roles", "guests")
	return nil
}
