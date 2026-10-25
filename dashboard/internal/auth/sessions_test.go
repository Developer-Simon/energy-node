package auth

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sessionIDFor is the ID Sessions shows for the session of token.
func sessionIDFor(token string) string {
	return tokenHash(token)[:16]
}

func TestUsernameRejectsPasswordPolicyRoute(t *testing.T) {
	manager, _ := loginManager(t, User{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleUserManagement}})

	for _, name := range []string{"password-policy", "Password-Policy"} {
		if _, err := manager.CreateUser("admin", name, usersTestPassword, nil); !errors.Is(err, ErrInvalidUsername) {
			t.Fatalf("%q: got %v, want ErrInvalidUsername", name, err)
		}
	}
	if _, err := manager.CreateUser("admin", "password-policy-2", usersTestPassword, nil); err != nil {
		t.Fatalf("a longer name must stay allowed: %v", err)
	}
}

func TestSessionsListsAccountsAndGuestsWithoutTokens(t *testing.T) {
	manager, _ := loginManager(t,
		User{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleUserManagement}},
		User{Username: "bob", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleViewDevices}},
	)
	admin, err := manager.Login("admin", usersTestPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := manager.Login("bob", usersTestPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	guest, err := manager.ContinueAsGuest()
	if err != nil {
		t.Fatal(err)
	}

	sessions := manager.Sessions(admin.Token)
	if len(sessions) != 3 {
		t.Fatalf("got %d sessions, want 3: %+v", len(sessions), sessions)
	}
	wantNames := []string{"admin", "bob", guest.User.Username}
	for i, want := range wantNames {
		if sessions[i].Username != want {
			t.Fatalf("position %d: got %q, want %q (sorted by username)", i, sessions[i].Username, want)
		}
	}
	tokens := map[string]string{"admin": admin.Token, "bob": bob.Token, guest.User.Username: guest.Token}
	for _, info := range sessions {
		if want := sessionIDFor(tokens[info.Username]); info.ID != want {
			t.Fatalf("%s: ID %q, want %q", info.Username, info.ID, want)
		}
		if info.Current != (info.Username == "admin") {
			t.Fatalf("%s: Current %v, want only the admin session marked", info.Username, info.Current)
		}
		if info.Guest != (info.Username == guest.User.Username) {
			t.Fatalf("%s: Guest %v", info.Username, info.Guest)
		}
	}

	body, err := json.Marshal(sessions)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{admin.Token, bob.Token, guest.Token} {
		if strings.Contains(string(body), token) {
			t.Fatal("session listing must never contain a token")
		}
	}
}

func TestSessionsSkipsExpiredSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	writeJSON(t, path, userFile{Version: usersFileVersion, GuestRoles: []string{}, GuestRolesReviewed: true, Users: []User{
		{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleUserManagement}},
	}})
	clock := time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)
	manager, err := newManager(path, "", "", func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Login("admin", usersTestPassword, true); err != nil {
		t.Fatal(err)
	}
	if got := len(manager.Sessions("")); got != 1 {
		t.Fatalf("live sessions: got %d, want 1", got)
	}
	clock = clock.Add(sessionLifetime + time.Minute)
	if got := len(manager.Sessions("")); got != 0 {
		t.Fatalf("expired session still listed: %d entries", got)
	}
}

func TestEndSessionEndsOnlyTheChosenSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	writeJSON(t, path, userFile{Version: usersFileVersion, GuestRoles: []string{}, GuestRolesReviewed: true, Users: []User{
		{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleUserManagement}},
		{Username: "bob", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleHTTPLogin, RoleViewDevices}},
	}})
	manager := mustManager(t, path, "", "")
	admin, err := manager.Login("admin", usersTestPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	bobFull, err := manager.Login("bob", usersTestPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	bobRestricted, err := manager.Login("bob", usersTestPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bobRestricted.Restricted || bobFull.Restricted {
		t.Fatal("test setup: bob needs one full and one restricted session")
	}

	if err := manager.EndSession("admin", sessionIDFor(bobFull.Token), admin.Token); err != nil {
		t.Fatalf("end bob's full session: %v", err)
	}
	if _, ok := manager.Session(bobFull.Token); ok {
		t.Fatal("bob's full session must be ended")
	}
	if _, ok := manager.Session(bobRestricted.Token); !ok {
		t.Fatal("bob's other session must stay")
	}
	if _, ok := manager.Session(admin.Token); !ok {
		t.Fatal("the admin session must stay")
	}

	reloaded := mustManager(t, path, "", "")
	if _, ok := reloaded.Session(bobFull.Token); ok {
		t.Fatal("ending a session must be persisted to sessions.json")
	}
	if _, ok := reloaded.Session(bobRestricted.Token); !ok {
		t.Fatal("the other session must survive a restart")
	}
}

func TestEndSessionRefusesOwnCurrentSessionAndUnknownID(t *testing.T) {
	manager, _ := loginManager(t, User{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleUserManagement}})
	admin, err := manager.Login("admin", usersTestPassword, true)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.EndSession("admin", sessionIDFor(admin.Token), admin.Token); !errors.Is(err, ErrSelfProtection) {
		t.Fatalf("own current session: got %v, want ErrSelfProtection", err)
	}
	if _, ok := manager.Session(admin.Token); !ok {
		t.Fatal("own session must stay after a refused end")
	}
	if err := manager.EndSession("admin", "0123456789abcdef", admin.Token); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("unknown id: got %v, want ErrUserNotFound", err)
	}
}
