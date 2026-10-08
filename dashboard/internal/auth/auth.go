package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	RoleSystemActions         = "system_actions"
	RoleDeleteDeviceDiscovery = "delete_device_discovery"
	RoleTuneLiveUpdates       = "tune_live_updates"
	RoleMQTTConfig            = "mqtt_config"
	RoleAutomations           = "automations"
	// RoleEditLayout schaltet den Bearbeitungsmodus der Uebersicht frei
	// (Editieren-Knopf, Editor-Fragment). Aktuell bekommt sie jeder - auch
	// jeder Gast (siehe ContinueAsGuest) -, damit das Layout ohne Adminkonto
	// anpassbar bleibt; die eigene Rolle ist das Plumbing, an das eine
	// spaetere Rollen-Oberflaeche greift.
	RoleEditLayout = "edit_layout"
	// RoleCheckUpdates schaltet nur das Lesen frei, ob auf GitHub eine
	// neuere Version liegt (siehe internal/updatecheck) - keine der
	// updater-startenden Aktionen hinter RoleSystemActions. Wie
	// RoleEditLayout bekommt sie provisorisch jeder, auch jeder Gast, weil
	// es noch keine Rollen-Oberflaeche gibt, ueber die man das gezielt
	// wieder entziehen koennte.
	RoleCheckUpdates = "check_updates"
	// RoleEditEnergy schaltet das Schreiben der Energie-Konfiguration frei:
	// Rollen, Interpretation, Gruppen, Kategorien und das Wiederherstellen
	// ihrer Revisionen. Wie RoleEditLayout bekommt sie vorlaeufig jeder, auch
	// jeder Gast, bis es eine Benutzerverwaltung gibt.
	RoleEditEnergy   = "edit_energy"
	guestLifetime    = 7 * 24 * time.Hour
	sessionLifetime  = 24 * time.Hour
	guestSessionLife = 7 * 24 * time.Hour
	passwordRounds   = 120000
)

type User struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash,omitempty"`
	Roles        []string  `json:"roles,omitempty"`
	Guest        bool      `json:"guest,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	LastLoginAt  time.Time `json:"last_login_at"`
}

type userFile struct {
	Users []User `json:"users"`
}

// sessionFile is what survives a restart of the dashboard, so an update
// that restarts it (or a reboot) does not log everyone out. It holds only
// a hash of each session token: the file alone never yields a cookie
// that would be accepted.
type sessionFile struct {
	Sessions []storedSession `json:"sessions"`
}

type storedSession struct {
	TokenHash string    `json:"token_sha256"`
	CSRFToken string    `json:"csrf_token"`
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Session struct {
	Token     string
	CSRFToken string
	User      User
	ExpiresAt time.Time
}

type Manager struct {
	mu           sync.Mutex
	path         string
	sessionsPath string
	users        map[string]User
	// sessions is keyed by tokenHash(token), the same key sessions.json
	// stores, so a session loaded from disk is found like a fresh one.
	sessions map[string]Session
	now      func() time.Time
}

type contextKey struct{}

var ErrInvalidCredentials = errors.New("invalid credentials")

func NewManager(path, bootstrapUsername, bootstrapPassword string) (*Manager, error) {
	return newManager(path, bootstrapUsername, bootstrapPassword, time.Now)
}

// guestRoles bekommt jeder Gast, solange es keine Rollen-Oberflaeche gibt
// (siehe ContinueAsGuest). Die Nachruestung in newManager gibt sie auch
// Gaesten, deren Konto vor einer neuen Rolle angelegt wurde.
var guestRoles = []string{RoleAutomations, RoleEditLayout, RoleCheckUpdates, RoleEditEnergy}

func newManager(path, bootstrapUsername, bootstrapPassword string, now func() time.Time) (*Manager, error) {
	if bootstrapUsername == "" {
		bootstrapUsername = "admin"
	}
	manager := &Manager{
		path:         filepath.Clean(path),
		sessionsPath: filepath.Join(filepath.Dir(filepath.Clean(path)), "sessions.json"),
		users:        map[string]User{},
		sessions:     map[string]Session{},
		now:          now,
	}
	changed, err := manager.load()
	if err != nil {
		return nil, err
	}
	manager.mu.Lock()
	// Roles added after an admin was first bootstrapped are backfilled here,
	// since an existing users.json keeps the role list it was written with.
	// Every new role that the bootstrap admin gets below belongs in this list
	// too, or the already-installed node is the one that stays locked out.
	for _, role := range []string{RoleDeleteDeviceDiscovery, RoleTuneLiveUpdates, RoleMQTTConfig, RoleAutomations, RoleEditLayout, RoleCheckUpdates, RoleEditEnergy} {
		if user, exists := manager.users[bootstrapUsername]; exists && !user.Guest && !HasRole(user, role) {
			user.Roles = append(user.Roles, role)
			manager.users[bootstrapUsername] = user
			changed = true
		}
	}
	for name, user := range manager.users {
		if !user.Guest {
			continue
		}
		for _, role := range guestRoles {
			if !HasRole(user, role) {
				user.Roles = append(user.Roles, role)
				changed = true
			}
		}
		manager.users[name] = user
	}
	if bootstrapPassword != "" {
		if _, exists := manager.users[bootstrapUsername]; !exists {
			hash, hashErr := hashPassword(bootstrapPassword)
			if hashErr != nil {
				manager.mu.Unlock()
				return nil, hashErr
			}
			nowValue := manager.now().UTC()
			manager.users[bootstrapUsername] = User{Username: bootstrapUsername, PasswordHash: hash, Roles: []string{RoleSystemActions, RoleDeleteDeviceDiscovery, RoleTuneLiveUpdates, RoleMQTTConfig, RoleAutomations, RoleEditLayout, RoleCheckUpdates, RoleEditEnergy}, CreatedAt: nowValue, LastLoginAt: nowValue}
			changed = true
		}
	}
	if changed {
		if err := manager.saveLocked(); err != nil {
			manager.mu.Unlock()
			return nil, err
		}
	}
	// Sessions come last: they are rebuilt from the users loaded above, so
	// one whose user is gone (a cleaned-up guest) is dropped. An unreadable
	// sessions.json only costs a new login, never the start.
	manager.loadSessionsLocked()
	manager.mu.Unlock()
	return manager, nil
}

func (m *Manager) load() (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var file userFile
	if err := json.Unmarshal(data, &file); err != nil {
		return false, fmt.Errorf("users: invalid JSON: %w", err)
	}
	changed := false
	for _, user := range file.Users {
		if user.Username == "" || m.users[user.Username].Username != "" {
			return false, fmt.Errorf("users: duplicate or empty username")
		}
		if user.Roles == nil {
			user.Roles = []string{}
		}
		m.users[user.Username] = user
	}
	if m.cleanupGuestsLocked() {
		changed = true
	}
	return changed, nil
}

func (m *Manager) Login(username, password string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupGuestsLocked()
	user, ok := m.users[username]
	if !ok || user.Guest || !verifyPassword(user.PasswordHash, password) {
		return Session{}, ErrInvalidCredentials
	}
	user.LastLoginAt = m.now().UTC()
	m.users[username] = user
	if err := m.saveLocked(); err != nil {
		return Session{}, err
	}
	return m.newSessionLocked(user)
}

func (m *Manager) ContinueAsGuest() (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupGuestsLocked()
	username, err := randomID("guest-")
	if err != nil {
		return Session{}, err
	}
	nowValue := m.now().UTC()
	// Provisional: guests get RoleAutomations, RoleEditLayout and
	// RoleCheckUpdates too, since there is no per-user role UI yet and
	// locking either behind an admin-only login would make the feature
	// unusable for the primary user. Revisit once role management grows a
	// UI (knowhow/dashboard/automationen-tab.md).
	user := User{Username: username, Guest: true, Roles: append([]string(nil), guestRoles...), CreatedAt: nowValue, LastLoginAt: nowValue}
	m.users[username] = user
	if err := m.saveLocked(); err != nil {
		return Session{}, err
	}
	return m.newSessionLocked(user)
}

func (m *Manager) Session(token string) (Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := tokenHash(token)
	session, ok := m.sessions[key]
	if !ok || !session.ExpiresAt.After(m.now()) {
		if ok {
			delete(m.sessions, key)
		}
		return Session{}, false
	}
	session.Token = token
	return session, true
}

func (m *Manager) Logout(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := tokenHash(token)
	if _, ok := m.sessions[key]; !ok {
		return
	}
	delete(m.sessions, key)
	_ = m.saveSessionsLocked()
}

func (m *Manager) ValidateCSRF(token, csrfToken string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[tokenHash(token)]
	if !ok || !session.ExpiresAt.After(m.now()) || csrfToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(session.CSRFToken), []byte(csrfToken)) == 1
}

func (m *Manager) cleanupGuestsLocked() bool {
	cutoff := m.now().Add(-guestLifetime)
	changed := false
	for username, user := range m.users {
		if user.Guest && user.LastLoginAt.Before(cutoff) {
			delete(m.users, username)
			changed = true
		}
	}
	return changed
}

func (m *Manager) newSessionLocked(user User) (Session, error) {
	token, err := randomID("session-")
	if err != nil {
		return Session{}, err
	}
	csrfToken, err := randomID("csrf-")
	if err != nil {
		return Session{}, err
	}
	lifetime := sessionLifetime
	if user.Guest {
		lifetime = guestSessionLife
	}
	session := Session{Token: token, CSRFToken: csrfToken, User: user, ExpiresAt: m.now().Add(lifetime)}
	m.sessions[tokenHash(token)] = session
	if err := m.saveSessionsLocked(); err != nil {
		delete(m.sessions, tokenHash(token))
		return Session{}, err
	}
	return session, nil
}

// loadSessionsLocked restores the sessions saved by saveSessionsLocked.
// Expired ones and ones whose user no longer exists are skipped; the user
// itself comes from users.json, so a role change made since still applies.
func (m *Manager) loadSessionsLocked() {
	data, err := os.ReadFile(m.sessionsPath)
	if err != nil {
		return
	}
	var file sessionFile
	if err := json.Unmarshal(data, &file); err != nil {
		return
	}
	now := m.now()
	for _, stored := range file.Sessions {
		user, ok := m.users[stored.Username]
		if !ok || stored.TokenHash == "" || !stored.ExpiresAt.After(now) {
			continue
		}
		m.sessions[stored.TokenHash] = Session{CSRFToken: stored.CSRFToken, User: user, ExpiresAt: stored.ExpiresAt}
	}
}

// saveSessionsLocked writes every live session to sessions.json, dropping
// the expired ones on the way.
func (m *Manager) saveSessionsLocked() error {
	now := m.now()
	sessions := make([]storedSession, 0, len(m.sessions))
	for key, session := range m.sessions {
		if !session.ExpiresAt.After(now) {
			delete(m.sessions, key)
			continue
		}
		sessions = append(sessions, storedSession{TokenHash: key, CSRFToken: session.CSRFToken, Username: session.User.Username, ExpiresAt: session.ExpiresAt})
	}
	data, err := json.MarshalIndent(sessionFile{Sessions: sessions}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(m.sessionsPath, data)
}

func (m *Manager) saveLocked() error {
	users := make([]User, 0, len(m.users))
	for _, user := range m.users {
		users = append(users, user)
	}
	data, err := json.MarshalIndent(userFile{Users: users}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(m.path, data)
}

// writeFileAtomic writes data as a 0600 file through a temporary file and a
// rename, so a crash never leaves a half-written file behind.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+strings.TrimSuffix(filepath.Base(path), ".json")+"-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	derived := deriveKey([]byte(password), salt, passwordRounds)
	return strings.Join([]string{
		"pbkdf2-sha256",
		strconv.Itoa(passwordRounds),
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived),
	}, "$"), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	rounds, err := strconv.Atoi(parts[1])
	if err != nil || rounds < 10000 || rounds > 1000000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	actual := deriveKey([]byte(password), salt, rounds)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func deriveKey(password, salt []byte, rounds int) []byte {
	const keyLength = 32
	block := make([]byte, 4)
	block[3] = 1
	mac := hmac.New(sha256.New, password)
	mac.Write(salt)
	mac.Write(block)
	u := mac.Sum(nil)
	result := append([]byte(nil), u...)
	for round := 1; round < rounds; round++ {
		mac = hmac.New(sha256.New, password)
		mac.Write(u)
		u = mac.Sum(nil)
		for index := range result {
			result[index] ^= u[index]
		}
	}
	return result[:keyLength]
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomID(prefix string) (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value), nil
}

func WithUser(ctx context.Context, user User) context.Context {
	return context.WithValue(ctx, contextKey{}, user)
}

func UserFromContext(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(contextKey{}).(User)
	return user, ok
}

func HasRole(user User, role string) bool {
	for _, candidate := range user.Roles {
		if candidate == role {
			return true
		}
	}
	return false
}

func (s Session) CookieValue() string {
	return s.Token
}
