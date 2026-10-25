package auth

import (
	"errors"
	"log"
	"regexp"
	"sort"
	"strings"
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
	// ErrWeakPassword meldet ein Passwort unter der Mindestlänge.
	ErrWeakPassword = errors.New("password is too short")
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

const minPasswordLength = 10

var usernamePattern = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

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
	if err := validatePassword(password); err != nil {
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
		if session.User.Username != name {
			continue
		}
		if disabled {
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
		if session.User.Username == name {
			delete(sessions, key)
		}
	}
	if err := m.commitLocked(users, sessions); err != nil {
		return err
	}
	log.Printf("auth: %s %s %s", actor, "delete", name)
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
	if !usernamePattern.MatchString(name) || strings.HasPrefix(name, "guest-") {
		return "", ErrInvalidUsername
	}
	return name, nil
}

func validatePassword(password string) error {
	if utf8.RuneCountInString(password) < minPasswordLength {
		return ErrWeakPassword
	}
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
