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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	RoleSystemActions         = "system_actions"
	RoleDeleteDeviceDiscovery = "delete_device_discovery"
	RoleMQTTConfig            = "mqtt_config"
	RoleAutomations           = "automations"
	// RoleEditLayout schaltet den Bearbeitungsmodus der Uebersicht frei
	// (Editieren-Knopf, Editor-Fragment). Sie steht nicht in der Gastvorgabe,
	// ein Gast bekommt sie nur, wenn ein Admin sie ihm gibt.
	RoleEditLayout = "edit_layout"
	// RoleViewDeviceAnalysis schaltet die Analyse im Geraete-Dialog frei:
	// Rohdaten, Discovery, Schreibaktionen und Konfigurationswarnungen.
	RoleViewDeviceAnalysis = "view_device_analysis"
	// RoleEditDeviceAppearance schaltet das Festlegen von Icon und Favoriten
	// eines Geraets frei. Sie steht nicht in der Gastvorgabe, ein Gast bekommt
	// sie nur, wenn ein Admin sie ihm gibt.
	RoleEditDeviceAppearance = "edit_device_appearance"
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
	RoleEditEnergy = "edit_energy"
	// RoleEditDeviceMap schaltet das Bearbeiten der Geraetekarte frei
	// (Kanten, Schalter, Geraete). Wie RoleEditEnergy bekommt sie vorlaeufig
	// jeder, auch jeder Gast, bis es eine Benutzerverwaltung gibt.
	RoleEditDeviceMap = "edit_device_map"
	// RoleEditDeviceMapView schaltet das Umschalten der Geraetekarte in die
	// Ansicht des Bearbeiters frei. Wie RoleEditDeviceMap bekommt sie
	// vorlaeufig jeder, auch jeder Gast, bis es eine Benutzerverwaltung gibt.
	RoleEditDeviceMapView = "edit_device_map_view"
	guestLifetime         = 7 * 24 * time.Hour
	sessionLifetime       = 24 * time.Hour
	guestSessionLife      = 7 * 24 * time.Hour
	passwordRounds        = 120000
	// SourceHomeAssistant markiert Konten und Sitzungen, die aus Home Assistant
	// kommen (User.Source). Lokale Konten und Gäste haben keine Quelle.
	SourceHomeAssistant = "homeassistant"
	// ViaCompanion markiert eine Sitzung, die über die Companion-Kopplung entstand.
	ViaCompanion = "companion"
	// PairingCodeLifetime ist die Gültigkeit eines Kopplungscodes.
	PairingCodeLifetime = 10 * time.Minute
)

type User struct {
	Username         string    `json:"username"`
	PasswordHash     string    `json:"password_hash,omitempty"`
	HTTPPasswordHash string    `json:"http_password_hash,omitempty"`
	Roles            []string  `json:"roles,omitempty"`
	Guest            bool      `json:"guest,omitempty"`
	Disabled         bool      `json:"disabled"`
	CreatedAt        time.Time `json:"created_at"`
	LastLoginAt      time.Time `json:"last_login_at"`
	// HasHTTPPassword is set only in the output of Users and Guests; the hash
	// itself never leaves the package.
	HasHTTPPassword bool `json:"has_http_password,omitempty"`
	// Source ist "" für lokale Konten und Gäste, SourceHomeAssistant für HA-Konten.
	Source      string `json:"source,omitempty"`
	HAUserID    string `json:"ha_user_id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

// companionFile hält die Kopplung zur Home-Assistant-Companion-Integration. Der
// Token selbst steht nie in der Datei, nur sein Hash.
type companionFile struct {
	Label      string    `json:"label"`
	TokenHash  string    `json:"token_hash"`
	PairedAt   time.Time `json:"paired_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// haRolesFile sind die Vorgaben für HA-Konten: Admin für Konten mit HA-Admin,
// User für alle anderen.
type haRolesFile struct {
	Admin []string `json:"admin"`
	User  []string `json:"user"`
}

type userFile struct {
	Version            int      `json:"version"`
	GuestRoles         []string `json:"guest_roles"`
	GuestRolesReviewed bool     `json:"guest_roles_reviewed,omitempty"`
	// MinPasswordLength is the minimum for new passwords; absent or 0 means the default.
	MinPasswordLength int            `json:"min_password_length,omitempty"`
	Users             []User         `json:"users"`
	Companion         *companionFile `json:"companion,omitempty"`
	HARoles           *haRolesFile   `json:"ha_roles,omitempty"`
	HAUsers           []HAAccount    `json:"ha_users,omitempty"`
}

// sessionFile is what survives a restart of the dashboard, so an update
// that restarts it (or a reboot) does not log everyone out. It holds only
// a hash of each session token: the file alone never yields a cookie
// that would be accepted.
type sessionFile struct {
	Sessions []storedSession `json:"sessions"`
}

type storedSession struct {
	TokenHash  string    `json:"token_sha256"`
	CSRFToken  string    `json:"csrf_token"`
	Username   string    `json:"username"`
	ExpiresAt  time.Time `json:"expires_at"`
	Restricted bool      `json:"restricted,omitempty"`
	HAUserID   string    `json:"ha_user_id,omitempty"`
	Via        string    `json:"via,omitempty"`
}

type Session struct {
	Token     string
	CSRFToken string
	User      User
	ExpiresAt time.Time
	// Restricted marks a session from an HTTP login or from the HTTP
	// password; its HTTPS-only roles are withheld (see EffectiveUser).
	Restricted bool
	// Via nennt den Weg, auf dem die Sitzung entstand (ViaCompanion), leer sonst.
	Via string
}

type Manager struct {
	mu           sync.Mutex
	path         string
	sessionsPath string
	users        map[string]User
	// guestRoles is the role list every new guest gets, read from users.json.
	guestRoles []string
	// migrated is true when this start moved users.json from version 1 to 2.
	migrated bool
	// guestRolesReviewed is false until the admin has set the guest roles
	// once after a migration; it drives MigrationNotice.
	guestRolesReviewed bool
	// minPasswordLength is the minimum length for new passwords, from users.json.
	minPasswordLength int
	// sessions is keyed by tokenHash(token), the same key sessions.json
	// stores, so a session loaded from disk is found like a fresh one.
	sessions map[string]Session
	// haAccounts sind die Home-Assistant-Konten, nach HAUserID geschlüsselt.
	haAccounts map[string]HAAccount
	// haRoles sind die Vorgaben für HA-Konten; nil, solange ha_roles fehlt.
	haRoles *haRolesFile
	// companion ist die Kopplung; nil ohne Kopplung.
	companion *companionFile
	// pairing ist der offene Kopplungscode, nur im Speicher; nil ohne Code.
	pairing *pendingPairing
	// pairLimiter begrenzt die Fehlversuche beim Kopplen insgesamt.
	pairLimiter *failureLimiter
	// haVerifier prüft Anmeldungen über Home Assistant; nil ohne Verifier.
	haVerifier HAVerifier
	// haLimiter begrenzt die Fehlversuche bei Anmeldungen über Home Assistant.
	haLimiter *failureLimiter
	now       func() time.Time
}

type contextKey struct{}

var ErrInvalidCredentials = errors.New("invalid credentials")

// ErrInsecureLoginNotAllowed meldet ein richtiges Passwort über HTTP bei
// einem Konto ohne die Berechtigung http_login.
var ErrInsecureLoginNotAllowed = errors.New("insecure login not allowed")

var (
	ErrPairingInvalid   = errors.New("pairing code invalid or expired")
	ErrNotPaired        = errors.New("no companion paired")
	ErrAccountDisabled  = errors.New("account disabled")
	ErrHARoleNotAllowed = errors.New("role is not allowed for home assistant accounts")
)

func NewManager(path, bootstrapUsername, bootstrapPassword string) (*Manager, error) {
	return newManager(path, bootstrapUsername, bootstrapPassword, time.Now)
}

func newManager(path, bootstrapUsername, bootstrapPassword string, now func() time.Time) (*Manager, error) {
	if bootstrapUsername == "" {
		bootstrapUsername = "admin"
	}
	manager := &Manager{
		path:               filepath.Clean(path),
		sessionsPath:       filepath.Join(filepath.Dir(filepath.Clean(path)), "sessions.json"),
		users:              map[string]User{},
		sessions:           map[string]Session{},
		haAccounts:         map[string]HAAccount{},
		pairLimiter:        newFailureLimiter(0, 10, time.Minute),
		haLimiter:          newFailureLimiter(5, 20, time.Minute),
		now:                now,
		guestRoles:         defaultGuestRoles(),
		guestRolesReviewed: true,
		minPasswordLength:  DefaultMinPasswordLength,
	}
	manager.mu.Lock()
	changed, err := manager.loadLocked()
	if err != nil {
		manager.mu.Unlock()
		return nil, err
	}
	// The bootstrap admin is created only while no non-guest account exists.
	// An existing one is never touched here: roles are managed in the UI.
	if bootstrapPassword != "" && !manager.hasNonGuestLocked() {
		hash, hashErr := hashPassword(bootstrapPassword)
		if hashErr != nil {
			manager.mu.Unlock()
			return nil, hashErr
		}
		nowValue := manager.now().UTC()
		manager.users[bootstrapUsername] = User{Username: bootstrapUsername, PasswordHash: hash, Roles: Normalize(bootstrapAdminRoles()), CreatedAt: nowValue, LastLoginAt: nowValue}
		changed = true
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

// loadLocked reads users.json, migrates it when it is older than the current
// version and reports whether the file has to be written again.
func (m *Manager) loadLocked() (bool, error) {
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
	if file.Version > usersFileVersion {
		return false, fmt.Errorf("%w: %d (this dashboard reads up to %d)", ErrUnsupportedUsersVersion, file.Version, usersFileVersion)
	}
	for _, user := range file.Users {
		if user.Username == "" || m.users[user.Username].Username != "" {
			return false, fmt.Errorf("users: duplicate or empty username")
		}
		if user.Roles == nil {
			user.Roles = []string{}
		}
		m.users[user.Username] = user
	}
	for _, account := range file.HAUsers {
		if account.Roles == nil {
			account.Roles = []string{}
		}
		m.haAccounts[account.HAUserID] = account
	}
	m.haRoles = file.HARoles
	m.companion = file.Companion
	m.minPasswordLength = DefaultMinPasswordLength
	if file.MinPasswordLength >= MinPasswordLengthLowest && file.MinPasswordLength <= MinPasswordLengthHighest {
		m.minPasswordLength = file.MinPasswordLength
	}
	changed := false
	if file.Version < 2 {
		m.migrated = len(file.Users) > 0
		m.guestRoles = migrateV1(m.users)
		m.guestRolesReviewed = !m.migrated
		changed = true
	} else {
		// Version 2 kennt tune_live_updates, Version 3 nicht mehr (migrateV2),
		// Version 4 ergaenzt die Geraete-Rechte (migrateV3). Jede Umschreibung
		// laeuft einmal beim Uebergang und nie wieder.
		upgradeV2 := file.Version < 3
		upgrade := file.Version < usersFileVersion
		if upgrade {
			changed = true
		}
		guestRoles := file.GuestRoles
		if upgradeV2 {
			guestRoles = migrateV2(guestRoles, true)
		}
		if upgrade {
			guestRoles = migrateV3(guestRoles)
		}
		m.guestRoles = Normalize(guestAllowedOnly(guestRoles))
		m.guestRolesReviewed = file.GuestRolesReviewed
		for name, user := range m.users {
			roles := user.Roles
			if upgradeV2 {
				roles = migrateV2(roles, user.Guest)
			}
			if upgrade {
				roles = migrateV3(roles)
			}
			user.Roles = Normalize(roles)
			m.users[name] = user
		}
		if upgrade {
			for id, account := range m.haAccounts {
				account.Roles = migrateV3(account.Roles)
				m.haAccounts[id] = account
			}
			if m.haRoles != nil {
				m.haRoles.Admin = migrateV3(m.haRoles.Admin)
				m.haRoles.User = migrateV3(m.haRoles.User)
			}
		}
	}
	if m.cleanupGuestsLocked() {
		changed = true
	}
	return changed, nil
}

// hasNonGuestLocked reports whether an account exists that is not a guest.
func (m *Manager) hasNonGuestLocked() bool {
	for _, user := range m.users {
		if !user.Guest {
			return true
		}
	}
	return false
}

// Login prüft die Anmeldung nach Spec Abschnitt 5. secure sagt, ob die Anfrage
// über HTTPS kommt. Über HTTP entsteht immer eine eingeschränkte Sitzung.
// Kennt der Manager den Namen lokal, gilt nur das lokale Konto. Sonst fragt
// er Home Assistant, wenn eine Kopplung und ein Verifier da sind.
func (m *Manager) Login(username, password string, secure bool) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupGuestsLocked()
	name := strings.ToLower(username)
	if _, ok := m.users[name]; ok {
		return m.loginLocalLocked(name, password, secure)
	}
	return m.loginHALocked(username, name, password, secure)
}

// loginHALocked meldet einen HA-Benutzer an. Der Aufrufer hält m.mu. Während
// der Abfrage bei Home Assistant wird m.mu freigegeben und danach wieder
// genommen. Deshalb wird die Kopplung danach erneut geprüft.
func (m *Manager) loginHALocked(username, name, password string, secure bool) (Session, error) {
	if !secure || m.companion == nil || m.haVerifier == nil || !m.haLimiter.allowed(name, m.now()) {
		return Session{}, ErrInvalidCredentials
	}
	verifier := m.haVerifier
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), HAVerifyTimeout)
	identity, err := verifier.VerifyHA(ctx, username, password)
	cancel()
	m.mu.Lock()
	if errors.Is(err, ErrInvalidCredentials) {
		m.haLimiter.fail(name, m.now())
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, ErrHomeAssistantUnavailable
	}
	if m.companion == nil {
		return Session{}, ErrInvalidCredentials
	}
	nowValue := m.now().UTC()
	account := m.upsertHAAccountLocked(identity, nowValue)
	if account.Disabled {
		return Session{}, ErrInvalidCredentials
	}
	account.LastLoginAt = nowValue
	m.haAccounts[identity.UserID] = account
	if err := m.saveLocked(); err != nil {
		return Session{}, err
	}
	return m.newSessionLocked(m.haUserLocked(account), false)
}

// loginLocalLocked prüft die Anmeldung gegen ein lokales Konto oder einen Gast.
func (m *Manager) loginLocalLocked(username, password string, secure bool) (Session, error) {
	user, ok := m.users[username]
	if !ok || user.Guest || user.Disabled {
		return Session{}, ErrInvalidCredentials
	}
	restricted := !secure
	var matched bool
	switch {
	case !secure && user.HTTPPasswordHash != "":
		// Über HTTP zählt nur das HTTP-Passwort, das Hauptpasswort nie.
		matched = verifyPassword(user.HTTPPasswordHash, password)
	case !secure:
		matched = verifyPassword(user.PasswordHash, password)
	case verifyPassword(user.PasswordHash, password):
		matched = true
	default:
		// Über HTTPS prüft das HTTP-Passwort erst, wenn das Hauptpasswort nicht stimmt.
		restricted = true
		matched = user.HTTPPasswordHash != "" && verifyPassword(user.HTTPPasswordHash, password)
	}
	if !matched {
		return Session{}, ErrInvalidCredentials
	}
	if !secure && !HasRole(user, RoleHTTPLogin) {
		return Session{}, ErrInsecureLoginNotAllowed
	}
	user.LastLoginAt = m.now().UTC()
	m.users[username] = user
	if err := m.saveLocked(); err != nil {
		return Session{}, err
	}
	return m.newSessionLocked(user, restricted)
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
	user := User{Username: username, Guest: true, Roles: append([]string(nil), m.guestRoles...), CreatedAt: nowValue, LastLoginAt: nowValue}
	m.users[username] = user
	if err := m.saveLocked(); err != nil {
		return Session{}, err
	}
	return m.newSessionLocked(user, false)
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

func (m *Manager) newSessionLocked(user User, restricted bool) (Session, error) {
	return m.newSessionViaLocked(user, restricted, "")
}

// newSessionViaLocked legt eine Sitzung an, die über den Weg via entstanden ist.
func (m *Manager) newSessionViaLocked(user User, restricted bool, via string) (Session, error) {
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
	session := Session{Token: token, CSRFToken: csrfToken, User: user, ExpiresAt: m.now().Add(lifetime), Restricted: restricted, Via: via}
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
		if stored.TokenHash == "" || !stored.ExpiresAt.After(now) {
			continue
		}
		if stored.HAUserID != "" {
			// Eine HA-Sitzung kommt aus den HA-Konten, nie aus den lokalen
			// Konten gleichen Namens. Fehlt das Konto oder ist es gesperrt, fällt sie weg.
			account, ok := m.haAccounts[stored.HAUserID]
			if !ok || account.Disabled {
				continue
			}
			m.sessions[stored.TokenHash] = Session{CSRFToken: stored.CSRFToken, User: m.haUserLocked(account), ExpiresAt: stored.ExpiresAt, Via: stored.Via}
			continue
		}
		user, ok := m.users[stored.Username]
		if !ok {
			continue
		}
		m.sessions[stored.TokenHash] = Session{CSRFToken: stored.CSRFToken, User: user, ExpiresAt: stored.ExpiresAt, Restricted: stored.Restricted}
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
		sessions = append(sessions, storedSession{TokenHash: key, CSRFToken: session.CSRFToken, Username: session.User.Username, ExpiresAt: session.ExpiresAt, Restricted: session.Restricted, HAUserID: session.User.HAUserID, Via: session.Via})
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
	// Sorted, so the same state always produces the same bytes.
	sort.Slice(users, func(i, j int) bool { return users[i].Username < users[j].Username })
	accounts := make([]HAAccount, 0, len(m.haAccounts))
	for _, account := range m.haAccounts {
		accounts = append(accounts, account)
	}
	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].Username != accounts[j].Username {
			return accounts[i].Username < accounts[j].Username
		}
		return accounts[i].HAUserID < accounts[j].HAUserID
	})
	file := userFile{
		Version:            usersFileVersion,
		GuestRoles:         append([]string{}, m.guestRoles...),
		GuestRolesReviewed: m.guestRolesReviewed,
		MinPasswordLength:  m.minPasswordLength,
		Users:              users,
		Companion:          m.companion,
		HARoles:            m.haRoles,
		HAUsers:            accounts,
	}
	data, err := json.MarshalIndent(file, "", "  ")
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
	if info, ok := RoleByKey(role); ok && info.Requires != "" && !containsRole(user.Roles, info.Requires) {
		return false
	}
	return containsRole(user.Roles, role)
}

func containsRole(roles []string, role string) bool {
	for _, candidate := range roles {
		if candidate == role {
			return true
		}
	}
	return false
}

// EffectiveUser liefert eine Kopie des Benutzers mit den Berechtigungen, die
// für diese Anfrage gelten: fehlende Voraussetzungen fallen weg, und über HTTP
// oder in einer eingeschränkten Sitzung auch alle HTTPS-only-Berechtigungen.
func EffectiveUser(session Session, secure bool) User {
	user := session.User
	withholdHTTPSOnly := !secure || session.Restricted
	roles := make([]string, 0, len(user.Roles))
	for _, key := range Normalize(user.Roles) {
		if info, _ := RoleByKey(key); withholdHTTPSOnly && info.HTTPSOnly {
			continue
		}
		roles = append(roles, key)
	}
	user.Roles = roles
	return user
}

// Withheld nennt die Berechtigungen der Sitzung, die EffectiveUser für diese
// Anfrage wegnimmt, in Katalogreihenfolge.
func Withheld(session Session, secure bool) []string {
	effective := EffectiveUser(session, secure).Roles
	var out []string
	for _, key := range AllRoles() {
		if containsRole(session.User.Roles, key) && !containsRole(effective, key) {
			out = append(out, key)
		}
	}
	return out
}

func (s Session) CookieValue() string {
	return s.Token
}
