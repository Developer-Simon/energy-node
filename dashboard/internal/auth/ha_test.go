package auth

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
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
