package auth

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/companionpair"
)

// addHAAccount legt ein HA-Konto direkt in die Map und speichert.
func addHAAccount(t *testing.T, m *Manager, account HAAccount) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.haAccounts[account.HAUserID] = account
	if err := m.saveLocked(); err != nil {
		t.Fatal(err)
	}
}

// addHASession erzeugt eine Sitzung für das HA-Konto haUserID.
func addHASession(t *testing.T, m *Manager, haUserID string) Session {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	session, err := m.newSessionLocked(m.haUserLocked(m.haAccounts[haUserID]), false)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestHAUsersRoundTripThroughUsersJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	m := mustManager(t, path, "", "")
	addHAAccount(t, m, HAAccount{HAUserID: "ha-1", Username: "maria", DisplayName: "Maria", RolesCustom: true, Roles: []string{RoleViewDevices, RoleViewOverview}, CreatedAt: now, LastLoginAt: now})
	if err := m.SetHARoles("admin", []string{RoleViewOverview}, []string{RoleViewDevices}); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.companion = &companionFile{Label: "Home Assistant", TokenHash: "hash", PairedAt: now, LastSeenAt: now}
	if err := m.saveLocked(); err != nil {
		t.Fatal(err)
	}
	m.mu.Unlock()

	reloaded := mustManager(t, path, "", "")
	if got, want := reloaded.HAAccounts(), m.HAAccounts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ha accounts after restart: got %+v, want %+v", got, want)
	}
	admin, user, set := reloaded.HARoles()
	if !set || !reflect.DeepEqual(admin, []string{RoleViewOverview}) || !reflect.DeepEqual(user, []string{RoleViewDevices}) {
		t.Fatalf("ha roles after restart: got %v %v %v", admin, user, set)
	}
	if got, want := reloaded.CompanionStatus(), m.CompanionStatus(); !reflect.DeepEqual(got, want) {
		t.Fatalf("companion after restart: got %+v, want %+v", got, want)
	}
}

func TestSetHARolesRejectsHTTPLogin(t *testing.T) {
	m := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
	if err := m.SetHARoles("admin", []string{RoleViewOverview}, []string{RoleViewDevices}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetHARoles("admin", []string{RoleHTTPLogin}, nil); !errors.Is(err, ErrHARoleNotAllowed) {
		t.Fatalf("http_login as admin preset: got %v, want ErrHARoleNotAllowed", err)
	}
	if err := m.SetHARoles("admin", nil, []string{RoleHTTPLogin}); !errors.Is(err, ErrHARoleNotAllowed) {
		t.Fatalf("http_login as user preset: got %v, want ErrHARoleNotAllowed", err)
	}
	if err := m.SetHARoles("admin", []string{"no_such_role"}, nil); !errors.Is(err, ErrUnknownRole) {
		t.Fatalf("unknown role: got %v, want ErrUnknownRole", err)
	}
	admin, user, _ := m.HARoles()
	if !reflect.DeepEqual(admin, []string{RoleViewOverview}) || !reflect.DeepEqual(user, []string{RoleViewDevices}) {
		t.Fatalf("ha roles changed by rejected call: admin %v, user %v", admin, user)
	}
}

func TestHAAccountEffectiveRolesFollowPreset(t *testing.T) {
	m := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
	addHAAccount(t, m, HAAccount{HAUserID: "ha-1", Username: "maria", HAAdmin: true})
	if err := m.SetHARoles("admin", []string{RoleViewOverview}, []string{RoleViewDevices}); err != nil {
		t.Fatal(err)
	}
	session := addHASession(t, m, "ha-1")
	if session.User.Source != SourceHomeAssistant || !reflect.DeepEqual(session.User.Roles, []string{RoleViewOverview}) {
		t.Fatalf("admin preset in session: source %q roles %v", session.User.Source, session.User.Roles)
	}

	if err := m.SetHARoles("admin", []string{RoleViewDevices}, []string{RoleViewDevices}); err != nil {
		t.Fatal(err)
	}
	live, ok := m.Session(session.Token)
	if !ok {
		t.Fatal("HA session must survive a preset change")
	}
	if !reflect.DeepEqual(live.User.Roles, []string{RoleViewDevices}) {
		t.Fatalf("live session roles after preset change: got %v, want [view_devices]", live.User.Roles)
	}
}

func TestCustomRolesStartWithPreset(t *testing.T) {
	m := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
	addHAAccount(t, m, HAAccount{HAUserID: "ha-1", Username: "maria", HAAdmin: true})
	if err := m.SetHARoles("admin", []string{RoleViewOverview, RoleViewDevices}, nil); err != nil {
		t.Fatal(err)
	}
	account, err := m.UpdateHAAccount("admin", "ha-1", nil, false, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{RoleViewDevices, RoleViewOverview}
	if !reflect.DeepEqual(account.Roles, want) {
		t.Fatalf("custom roles start: got %v, want preset %v", account.Roles, want)
	}
	for _, stored := range m.HAAccounts() {
		if stored.HAUserID == "ha-1" && !reflect.DeepEqual(stored.Roles, want) {
			t.Fatalf("stored custom roles: got %v, want %v", stored.Roles, want)
		}
	}
}

func TestDisablingHAAccountDropsItsSessions(t *testing.T) {
	m := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
	addHAAccount(t, m, HAAccount{HAUserID: "ha-1", Username: "maria"})
	session := addHASession(t, m, "ha-1")
	if _, ok := m.Session(session.Token); !ok {
		t.Fatal("HA session must exist before the account is disabled")
	}
	if _, err := m.UpdateHAAccount("admin", "ha-1", nil, true, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Session(session.Token); ok {
		t.Fatal("HA session must be dropped when its account is disabled")
	}
}

func TestHAAccountsDoNotCountAsLastAdmin(t *testing.T) {
	manager, _ := loginManager(t, User{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleUserManagement}})
	addHAAccount(t, manager, HAAccount{HAUserID: "ha-1", Username: "maria", RolesCustom: true, Roles: []string{RoleUserManagement}})
	_, err := manager.UpdateUser("admin", "admin", []string{}, false)
	if !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("stripping the only local admin: got %v, want ErrLastAdmin", err)
	}
}

func TestCreateUserRejectsNameOfHAAccount(t *testing.T) {
	m := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
	addHAAccount(t, m, HAAccount{HAUserID: "ha-1", Username: "Maria"})
	if _, err := m.CreateUser("admin", "maria", usersTestPassword, nil); !errors.Is(err, ErrUserExists) {
		t.Fatalf("local user with HA account name: got %v, want ErrUserExists", err)
	}
}

func TestLocalUpdateDoesNotTouchHASessionWithSameName(t *testing.T) {
	manager, _ := loginManager(t,
		User{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleUserManagement}},
		User{Username: "maria", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleViewOverview}},
	)
	addHAAccount(t, manager, HAAccount{HAUserID: "ha-1", Username: "maria"})
	haSession := addHASession(t, manager, "ha-1")

	if _, err := manager.UpdateUser("admin", "maria", nil, true); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.Session(haSession.Token); !ok {
		t.Fatal("disabling the local maria must not drop the HA session named maria")
	}
}

func TestHASessionSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	m := mustManager(t, path, "", "")
	addHAAccount(t, m, HAAccount{HAUserID: "ha-1", Username: "maria"})
	session := addHASession(t, m, "ha-1")
	m.mu.Lock()
	stored := m.sessions[tokenHash(session.Token)]
	stored.Via = ViaCompanion
	m.sessions[tokenHash(session.Token)] = stored
	if err := m.saveSessionsLocked(); err != nil {
		t.Fatal(err)
	}
	m.mu.Unlock()

	reloaded := mustManager(t, path, "", "")
	got, ok := reloaded.Session(session.Token)
	if !ok {
		t.Fatal("HA session must survive a restart")
	}
	if got.User.Source != SourceHomeAssistant || got.Via != ViaCompanion {
		t.Fatalf("restored session: source %q via %q", got.User.Source, got.Via)
	}
}

// pairForTest legt einen Kopplungscode an, koppelt mit dem richtigen Nachweis
// und gibt das Token zurück.
func pairForTest(t *testing.T, m *Manager) string {
	t.Helper()
	if _, _, err := m.CreatePairingCode("admin"); err != nil {
		t.Fatal(err)
	}
	token, _, err := m.Pair(func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// newClockManager startet einen Manager mit einer Uhr, die der Test verschiebt.
func newClockManager(t *testing.T, start time.Time) (*Manager, *time.Time) {
	t.Helper()
	clock := start
	m, err := newManager(filepath.Join(t.TempDir(), "users.json"), "", "", func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	return m, &clock
}

func TestPairWritesHARolesFromGuestRolesOnce(t *testing.T) {
	m := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
	pairForTest(t, m)
	admin, user, set := m.HARoles()
	guest := m.GuestRoles()
	if !set || !reflect.DeepEqual(admin, guest) || !reflect.DeepEqual(user, guest) {
		t.Fatalf("first pairing: ha roles admin %v user %v set %v, want guest roles %v", admin, user, set, guest)
	}

	if err := m.SetHARoles("admin", []string{RoleViewOverview}, []string{RoleViewDevices}); err != nil {
		t.Fatal(err)
	}
	pairForTest(t, m)
	admin, user, _ = m.HARoles()
	if !reflect.DeepEqual(admin, []string{RoleViewOverview}) || !reflect.DeepEqual(user, []string{RoleViewDevices}) {
		t.Fatalf("second pairing must keep the roles: admin %v user %v", admin, user)
	}
}

func TestPairRejectsWrongProofAndExpiresCodeAfterFive(t *testing.T) {
	m := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
	if _, _, err := m.CreatePairingCode("admin"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, _, err := m.Pair(func(string) bool { return false }); !errors.Is(err, ErrPairingInvalid) {
			t.Fatalf("wrong proof %d: got %v, want ErrPairingInvalid", i+1, err)
		}
	}
	if _, _, err := m.Pair(func(string) bool { return true }); !errors.Is(err, ErrPairingInvalid) {
		t.Fatalf("right proof after five failures: got %v, want ErrPairingInvalid", err)
	}
	if m.CompanionStatus().Paired {
		t.Fatal("no companion may be paired after failed attempts")
	}
}

func TestPairingCodeExpiresAfterTenMinutes(t *testing.T) {
	m, clock := newClockManager(t, now)
	if _, expiresAt, err := m.CreatePairingCode("admin"); err != nil || !expiresAt.Equal(now.Add(PairingCodeLifetime)) {
		t.Fatalf("expiry: got %v, err %v", expiresAt, err)
	}
	*clock = clock.Add(10*time.Minute + time.Second)
	if _, _, err := m.Pair(func(string) bool { return true }); !errors.Is(err, ErrPairingInvalid) {
		t.Fatalf("code after ten minutes: got %v, want ErrPairingInvalid", err)
	}
}

func TestPairIsRateLimited(t *testing.T) {
	m := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
	for i := 0; i < 10; i++ {
		if _, _, err := m.CreatePairingCode("admin"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := m.Pair(func(string) bool { return false }); !errors.Is(err, ErrPairingInvalid) {
			t.Fatalf("attempt %d: got %v, want ErrPairingInvalid", i+1, err)
		}
	}
	if _, _, err := m.CreatePairingCode("admin"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Pair(func(string) bool { return true }); !errors.Is(err, ErrPairingInvalid) {
		t.Fatalf("eleventh attempt with right proof: got %v, want ErrPairingInvalid", err)
	}
}

func TestNewCodeReplacesOldOne(t *testing.T) {
	m := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
	oldCode, _, err := m.CreatePairingCode("admin")
	if err != nil {
		t.Fatal(err)
	}
	newCode, _, err := m.CreatePairingCode("admin")
	if err != nil {
		t.Fatal(err)
	}
	var seen string
	token, _, err := m.Pair(func(code string) bool {
		seen = code
		return code == companionpair.Normalize(newCode)
	})
	if err != nil || token == "" {
		t.Fatalf("pair with the new code: token %q, err %v", token, err)
	}
	if seen == companionpair.Normalize(oldCode) || seen != companionpair.Normalize(newCode) {
		t.Fatalf("check received %q, want the new code", seen)
	}
}

func TestCompanionTokenValid(t *testing.T) {
	m := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
	token := pairForTest(t, m)
	if !m.CompanionTokenValid(token) {
		t.Fatal("the paired token must be valid")
	}
	if m.CompanionTokenValid("wrong-token") {
		t.Fatal("a wrong token must be invalid")
	}
	if err := m.Unpair("admin"); err != nil {
		t.Fatal(err)
	}
	if m.CompanionTokenValid(token) {
		t.Fatal("the token must be invalid after unpairing")
	}
}

func TestUnpairDropsHASessionsAndToken(t *testing.T) {
	manager, _ := loginManager(t, User{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleUserManagement}})
	addHAAccount(t, manager, HAAccount{HAUserID: "ha-1", Username: "maria"})
	pairForTest(t, manager)
	haSession := addHASession(t, manager, "ha-1")
	localSession, err := manager.Login("admin", usersTestPassword, true)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Unpair("admin"); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.Session(haSession.Token); ok {
		t.Fatal("the HA session must be dropped by unpairing")
	}
	if _, ok := manager.Session(localSession.Token); !ok {
		t.Fatal("the local session must survive unpairing")
	}
	if len(manager.HAAccounts()) != 1 {
		t.Fatal("HA accounts must survive unpairing")
	}
	if manager.CompanionStatus().Paired {
		t.Fatal("companion must not be paired after unpairing")
	}
	if err := manager.Unpair("admin"); !errors.Is(err, ErrNotPaired) {
		t.Fatalf("second unpair: got %v, want ErrNotPaired", err)
	}
}

func TestCompanionStatusReportsPendingCodeExpiry(t *testing.T) {
	m, clock := newClockManager(t, now)
	_, expiresAt, err := m.CreatePairingCode("admin")
	if err != nil {
		t.Fatal(err)
	}
	status := m.CompanionStatus()
	if status.PairingExpiresAt == nil || !status.PairingExpiresAt.Equal(expiresAt) {
		t.Fatalf("pending code expiry: got %v, want %v", status.PairingExpiresAt, expiresAt)
	}
	*clock = clock.Add(PairingCodeLifetime + time.Second)
	if m.CompanionStatus().PairingExpiresAt != nil {
		t.Fatal("an expired code must not be reported")
	}
}
