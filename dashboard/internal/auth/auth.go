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
	RoleEditLayout   = "edit_layout"
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

type Session struct {
	Token     string
	CSRFToken string
	User      User
	ExpiresAt time.Time
}

type Manager struct {
	mu       sync.Mutex
	path     string
	users    map[string]User
	sessions map[string]Session
	now      func() time.Time
}

type contextKey struct{}

var ErrInvalidCredentials = errors.New("invalid credentials")

func NewManager(path, bootstrapUsername, bootstrapPassword string) (*Manager, error) {
	return newManager(path, bootstrapUsername, bootstrapPassword, time.Now)
}

func newManager(path, bootstrapUsername, bootstrapPassword string, now func() time.Time) (*Manager, error) {
	if bootstrapUsername == "" {
		bootstrapUsername = "admin"
	}
	manager := &Manager{
		path:     filepath.Clean(path),
		users:    map[string]User{},
		sessions: map[string]Session{},
		now:      now,
	}
	changed, err := manager.load()
	if err != nil {
		return nil, err
	}
	manager.mu.Lock()
	if user, exists := manager.users[bootstrapUsername]; exists && !user.Guest && !HasRole(user, RoleDeleteDeviceDiscovery) {
		user.Roles = append(user.Roles, RoleDeleteDeviceDiscovery)
		manager.users[bootstrapUsername] = user
		changed = true
	}
	if user, exists := manager.users[bootstrapUsername]; exists && !user.Guest && !HasRole(user, RoleTuneLiveUpdates) {
		user.Roles = append(user.Roles, RoleTuneLiveUpdates)
		manager.users[bootstrapUsername] = user
		changed = true
	}
	if user, exists := manager.users[bootstrapUsername]; exists && !user.Guest && !HasRole(user, RoleMQTTConfig) {
		user.Roles = append(user.Roles, RoleMQTTConfig)
		manager.users[bootstrapUsername] = user
		changed = true
	}
	if user, exists := manager.users[bootstrapUsername]; exists && !user.Guest && !HasRole(user, RoleAutomations) {
		user.Roles = append(user.Roles, RoleAutomations)
		manager.users[bootstrapUsername] = user
		changed = true
	}
	if user, exists := manager.users[bootstrapUsername]; exists && !user.Guest && !HasRole(user, RoleEditLayout) {
		user.Roles = append(user.Roles, RoleEditLayout)
		manager.users[bootstrapUsername] = user
		changed = true
	}
	if bootstrapPassword != "" {
		if _, exists := manager.users[bootstrapUsername]; !exists {
			hash, hashErr := hashPassword(bootstrapPassword)
			if hashErr != nil {
				manager.mu.Unlock()
				return nil, hashErr
			}
			nowValue := manager.now().UTC()
			manager.users[bootstrapUsername] = User{Username: bootstrapUsername, PasswordHash: hash, Roles: []string{RoleSystemActions, RoleDeleteDeviceDiscovery, RoleTuneLiveUpdates, RoleMQTTConfig, RoleAutomations, RoleEditLayout}, CreatedAt: nowValue, LastLoginAt: nowValue}
			changed = true
		}
	}
	if changed {
		if err := manager.saveLocked(); err != nil {
			manager.mu.Unlock()
			return nil, err
		}
	}
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
	return m.newSessionLocked(user), nil
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
	// Provisional: guests get RoleAutomations and RoleEditLayout too, since
	// there is no per-user role UI yet and locking either behind an
	// admin-only login would make the feature unusable for the primary user.
	// Revisit once role management grows a UI
	// (knowhow/dashboard/automationen-tab.md).
	user := User{Username: username, Guest: true, Roles: []string{RoleAutomations, RoleEditLayout}, CreatedAt: nowValue, LastLoginAt: nowValue}
	m.users[username] = user
	if err := m.saveLocked(); err != nil {
		return Session{}, err
	}
	return m.newSessionLocked(user), nil
}

func (m *Manager) Session(token string) (Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[token]
	if !ok || !session.ExpiresAt.After(m.now()) {
		if ok {
			delete(m.sessions, token)
		}
		return Session{}, false
	}
	return session, true
}

func (m *Manager) Logout(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
}

func (m *Manager) ValidateCSRF(token, csrfToken string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[token]
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

func (m *Manager) newSessionLocked(user User) Session {
	token, _ := randomID("session-")
	csrfToken, _ := randomID("csrf-")
	lifetime := sessionLifetime
	if user.Guest {
		lifetime = guestSessionLife
	}
	session := Session{Token: token, CSRFToken: csrfToken, User: user, ExpiresAt: m.now().Add(lifetime)}
	m.sessions[token] = session
	return session
}

func (m *Manager) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	users := make([]User, 0, len(m.users))
	for _, user := range m.users {
		users = append(users, user)
	}
	data, err := json.MarshalIndent(userFile{Users: users}, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(m.path), ".users-*.tmp")
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
	return os.Rename(temporaryName, m.path)
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
