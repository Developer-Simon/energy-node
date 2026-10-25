package auth

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const usersTestPassword = "user-password-1"

func findUser(users []User, username string) (User, bool) {
	for _, user := range users {
		if user.Username == username {
			return user, true
		}
	}
	return User{}, false
}

func TestCreateUserValidates(t *testing.T) {
	manager, _ := loginManager(t, User{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleUserManagement}})

	if _, err := manager.CreateUser("admin", "ab", usersTestPassword, nil); !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("too short username: got %v, want ErrInvalidUsername", err)
	}
	if _, err := manager.CreateUser("admin", "guest-x", usersTestPassword, nil); !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("reserved guest- prefix: got %v, want ErrInvalidUsername", err)
	}
	if _, err := manager.CreateUser("admin", "Admin", usersTestPassword, nil); !errors.Is(err, ErrUserExists) {
		t.Fatalf("case-insensitive duplicate: got %v, want ErrUserExists", err)
	}
	if _, err := manager.CreateUser("admin", "dana", "123456789", nil); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("9 character password: got %v, want ErrWeakPassword", err)
	}
	if _, err := manager.CreateUser("admin", "dana", usersTestPassword, []string{"bogus"}); !errors.Is(err, ErrUnknownRole) {
		t.Fatalf("unknown role: got %v, want ErrUnknownRole", err)
	}
	if _, ok := findUser(manager.Users(), "dana"); ok {
		t.Fatal("rejected user must not be stored")
	}
}

func TestCreateUserNormalizesRoles(t *testing.T) {
	manager, _ := loginManager(t, User{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleUserManagement}})

	// automations needs view_automations, without it the role is dropped.
	dana, err := manager.CreateUser("admin", "dana", usersTestPassword, []string{"automations"})
	if err != nil {
		t.Fatal(err)
	}
	if len(dana.Roles) != 0 {
		t.Fatalf("automations without view_automations: got %v, want none", dana.Roles)
	}

	erik, err := manager.CreateUser("admin", "erik", usersTestPassword, []string{RoleViewStats, RoleViewDevices})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{RoleViewDevices, RoleViewStats}; !reflect.DeepEqual(erik.Roles, want) {
		t.Fatalf("roles: got %v, want sorted %v", erik.Roles, want)
	}
}

func TestLastAdminCountsOnlyActiveLocalAccounts(t *testing.T) {
	adminRoles := []string{RoleUserManagement, RoleViewOverview}
	manager, path := loginManager(t,
		User{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: adminRoles},
		User{Username: "bob", PasswordHash: mustHash(t, usersTestPassword), Roles: adminRoles, Disabled: true},
	)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// bob is disabled, so admin is the only active admin.
	if _, err := manager.UpdateUser("admin", "admin", []string{RoleViewOverview}, false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("stripping the last admin: got %v, want ErrLastAdmin", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("users.json changed after a rejected update")
	}

	if err := manager.DeleteUser("admin", "admin"); !errors.Is(err, ErrSelfProtection) {
		t.Fatalf("deleting own account: got %v, want ErrSelfProtection", err)
	}

	// Enabling bob makes him an active admin, so admin may now step down.
	if _, err := manager.UpdateUser("admin", "bob", adminRoles, false); err != nil {
		t.Fatalf("enabling bob: %v", err)
	}
	if _, err := manager.UpdateUser("admin", "admin", []string{RoleViewOverview}, false); err != nil {
		t.Fatalf("stripping admin while bob is an active admin: %v", err)
	}
}

func TestSelfProtection(t *testing.T) {
	adminRoles := []string{RoleUserManagement, RoleViewOverview}
	manager, _ := loginManager(t,
		User{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: adminRoles},
		User{Username: "anna", PasswordHash: mustHash(t, usersTestPassword), Roles: adminRoles},
	)

	if _, err := manager.UpdateUser("admin", "admin", adminRoles, true); !errors.Is(err, ErrSelfProtection) {
		t.Fatalf("disabling own account: got %v, want ErrSelfProtection", err)
	}
	if err := manager.DeleteUser("admin", "admin"); !errors.Is(err, ErrSelfProtection) {
		t.Fatalf("deleting own account: got %v, want ErrSelfProtection", err)
	}
	admin, ok := findUser(manager.Users(), "admin")
	if !ok || admin.Disabled {
		t.Fatalf("admin must still exist and be enabled, got %+v", admin)
	}
}

func TestUsersNeverExposeHashes(t *testing.T) {
	manager, _ := loginManager(t,
		User{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), HTTPPasswordHash: mustHash(t, "http-password-1"), Roles: []string{RoleUserManagement, RoleHTTPLogin}},
		User{Username: "bob", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleViewOverview}},
	)
	if _, err := manager.ContinueAsGuest(); err != nil {
		t.Fatal(err)
	}

	users := manager.Users()
	if len(users) != 2 {
		t.Fatalf("Users: got %d entries, want 2", len(users))
	}
	for _, user := range users {
		if user.PasswordHash != "" || user.HTTPPasswordHash != "" {
			t.Fatalf("Users exposes a hash for %s", user.Username)
		}
	}
	if admin, _ := findUser(users, "admin"); !admin.HasHTTPPassword {
		t.Fatal("admin has an HTTP password, HasHTTPPassword must be set")
	}
	if bob, _ := findUser(users, "bob"); bob.HasHTTPPassword {
		t.Fatal("bob has no HTTP password, HasHTTPPassword must stay false")
	}

	guests := manager.Guests()
	if len(guests) != 1 {
		t.Fatalf("Guests: got %d entries, want 1", len(guests))
	}
	if guests[0].PasswordHash != "" || guests[0].HTTPPasswordHash != "" {
		t.Fatal("Guests exposes a hash")
	}
}

func TestSessionEffectsUpdateAndDelete(t *testing.T) {
	manager, _ := loginManager(t,
		User{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleUserManagement}},
		User{Username: "bob", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleViewDevices, RoleViewOverview, RoleOperateDevices}},
		User{Username: "carol", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleViewOverview}},
	)
	bobSession, err := manager.Login("bob", usersTestPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	carolSession, err := manager.Login("carol", usersTestPassword, true)
	if err != nil {
		t.Fatal(err)
	}

	// A role change updates bob's live session in place.
	if _, err := manager.UpdateUser("admin", "bob", []string{RoleViewOverview}, false); err != nil {
		t.Fatal(err)
	}
	session, ok := manager.Session(bobSession.Token)
	if !ok {
		t.Fatal("bob's session must survive a role change")
	}
	if want := []string{RoleViewOverview}; !reflect.DeepEqual(session.User.Roles, want) {
		t.Fatalf("bob's session roles: got %v, want %v", session.User.Roles, want)
	}

	// Disabling bob discards his sessions.
	if _, err := manager.UpdateUser("admin", "bob", []string{RoleViewOverview}, true); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.Session(bobSession.Token); ok {
		t.Fatal("bob's session must be discarded when his account is disabled")
	}

	// Deleting carol discards her sessions too.
	if err := manager.DeleteUser("admin", "carol"); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.Session(carolSession.Token); ok {
		t.Fatal("carol's session must be discarded when her account is deleted")
	}
}

func TestHTTPPasswordRules(t *testing.T) {
	adminRoles := []string{RoleUserManagement, RoleViewOverview, RoleHTTPLogin}
	manager, path := loginManager(t,
		User{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: adminRoles},
		User{Username: "bob", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleViewOverview}},
	)

	if err := manager.SetHTTPPassword("admin", "bob", "http-password-1"); !errors.Is(err, ErrHTTPLoginRequired) {
		t.Fatalf("account without http_login: got %v, want ErrHTTPLoginRequired", err)
	}
	if err := manager.SetHTTPPassword("admin", "admin", usersTestPassword); !errors.Is(err, ErrHTTPPasswordReused) {
		t.Fatalf("HTTP password equal to main password: got %v, want ErrHTTPPasswordReused", err)
	}
	if err := manager.SetHTTPPassword("admin", "admin", "short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("short HTTP password: got %v, want ErrWeakPassword", err)
	}
	if err := manager.SetHTTPPassword("admin", "admin", "http-password-1"); err != nil {
		t.Fatal(err)
	}
	if admin, _ := findUser(manager.Users(), "admin"); !admin.HasHTTPPassword {
		t.Fatal("HasHTTPPassword must be set after SetHTTPPassword")
	}

	session, err := manager.Login("admin", usersTestPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetOwnHTTPPassword(session.Token, usersTestPassword); !errors.Is(err, ErrHTTPPasswordReused) {
		t.Fatalf("own HTTP password equal to main password: got %v, want ErrHTTPPasswordReused", err)
	}
	if err := manager.SetOwnHTTPPassword("session-unknown", "http-password-2"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("own HTTP password without session: got %v, want ErrInvalidCredentials", err)
	}
	if err := manager.SetOwnHTTPPassword(session.Token, "http-password-2"); err != nil {
		t.Fatal(err)
	}

	// An empty value removes the HTTP password.
	if err := manager.SetHTTPPassword("admin", "admin", ""); err != nil {
		t.Fatal(err)
	}
	if admin, _ := findUser(manager.Users(), "admin"); admin.HasHTTPPassword {
		t.Fatal("HasHTTPPassword must be false after the HTTP password was removed")
	}

	// Stripping http_login removes the stored HTTP password hash as well.
	if err := manager.SetHTTPPassword("admin", "admin", "http-password-3"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.UpdateUser("admin", "admin", []string{RoleUserManagement, RoleViewOverview}, false); err != nil {
		t.Fatal(err)
	}
	if got := readUsersFile(t, path).Users[0]; got.Username != "admin" || got.HTTPPasswordHash != "" {
		t.Fatalf("stored admin after stripping http_login: %+v, want no HTTP password hash", got)
	}
}

func TestGuestRolesRejectNonGuestKeys(t *testing.T) {
	manager, path := loginManager(t, User{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleUserManagement}})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.SetGuestRoles("admin", []string{"system_actions"}); !errors.Is(err, ErrGuestRoleNotAllowed) {
		t.Fatalf("non-guest role: got %v, want ErrGuestRoleNotAllowed", err)
	}
	if err := manager.SetGuestRoles("admin", []string{"bogus"}); !errors.Is(err, ErrUnknownRole) {
		t.Fatalf("unknown role: got %v, want ErrUnknownRole", err)
	}
	if got := manager.GuestRoles(); len(got) != 0 {
		t.Fatalf("rejected guest roles were stored: %v", got)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("users.json changed after a rejected guest role update")
	}

	if err := manager.SetGuestRoles("admin", []string{RoleViewOverview, RoleViewDevices}); err != nil {
		t.Fatal(err)
	}
	if want := []string{RoleViewDevices, RoleViewOverview}; !reflect.DeepEqual(manager.GuestRoles(), want) {
		t.Fatalf("GuestRoles: got %v, want %v", manager.GuestRoles(), want)
	}
}

func TestSessionEffects(t *testing.T) {
	bobRoles := []string{RoleViewOverview, RoleHTTPLogin}
	bobUser := func(t *testing.T) User {
		return User{Username: "bob", PasswordHash: mustHash(t, usersTestPassword), HTTPPasswordHash: mustHash(t, "http-password-1"), Roles: bobRoles}
	}
	carolUser := func(t *testing.T) User {
		return User{Username: "carol", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleViewOverview}}
	}
	adminUser := func(t *testing.T) User {
		return User{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleUserManagement}}
	}

	t.Run("guest default applies in place to all guest sessions", func(t *testing.T) {
		manager, _ := loginManager(t, adminUser(t))
		first, err := manager.ContinueAsGuest()
		if err != nil {
			t.Fatal(err)
		}
		second, err := manager.ContinueAsGuest()
		if err != nil {
			t.Fatal(err)
		}
		want := []string{RoleViewDevices, RoleViewOverview}
		if err := manager.SetGuestRoles("admin", want); err != nil {
			t.Fatal(err)
		}
		for _, token := range []string{first.Token, second.Token} {
			session, ok := manager.Session(token)
			if !ok {
				t.Fatal("guest session must survive a guest default change")
			}
			if !reflect.DeepEqual(session.User.Roles, want) {
				t.Fatalf("guest session roles: got %v, want %v", session.User.Roles, want)
			}
		}
		for _, guest := range manager.Guests() {
			if !reflect.DeepEqual(guest.Roles, want) {
				t.Fatalf("guest %s roles: got %v, want %v", guest.Username, guest.Roles, want)
			}
		}
	})

	t.Run("SetPassword discards all sessions of the user", func(t *testing.T) {
		manager, _ := loginManager(t, adminUser(t), bobUser(t), carolUser(t))
		bobFull, err := manager.Login("bob", usersTestPassword, true)
		if err != nil {
			t.Fatal(err)
		}
		bobRestricted, err := manager.Login("bob", "http-password-1", true)
		if err != nil {
			t.Fatal(err)
		}
		carol, err := manager.Login("carol", usersTestPassword, true)
		if err != nil {
			t.Fatal(err)
		}

		if err := manager.SetPassword("admin", "bob", "new-password-2"); err != nil {
			t.Fatal(err)
		}
		if _, ok := manager.Session(bobFull.Token); ok {
			t.Fatal("bob's full session must be discarded by an admin password reset")
		}
		if _, ok := manager.Session(bobRestricted.Token); ok {
			t.Fatal("bob's restricted session must be discarded by an admin password reset")
		}
		if _, ok := manager.Session(carol.Token); !ok {
			t.Fatal("carol's session must survive a reset of bob's password")
		}
		if _, err := manager.Login("bob", "new-password-2", true); err != nil {
			t.Fatalf("login with the new password: %v", err)
		}
	})

	t.Run("SetHTTPPassword discards only restricted sessions", func(t *testing.T) {
		manager, _ := loginManager(t, adminUser(t), bobUser(t))
		bobFull, err := manager.Login("bob", usersTestPassword, true)
		if err != nil {
			t.Fatal(err)
		}
		bobRestricted, err := manager.Login("bob", "http-password-1", true)
		if err != nil {
			t.Fatal(err)
		}

		if err := manager.SetHTTPPassword("admin", "bob", "http-password-2"); err != nil {
			t.Fatal(err)
		}
		if _, ok := manager.Session(bobRestricted.Token); ok {
			t.Fatal("bob's restricted session must be discarded when the HTTP password changes")
		}
		if _, ok := manager.Session(bobFull.Token); !ok {
			t.Fatal("bob's full session must survive an HTTP password change")
		}
	})

	t.Run("ChangeOwnPassword keeps the calling session and drops the others", func(t *testing.T) {
		manager, _ := loginManager(t, adminUser(t), bobUser(t), carolUser(t))
		bobKept, err := manager.Login("bob", usersTestPassword, true)
		if err != nil {
			t.Fatal(err)
		}
		bobOther, err := manager.Login("bob", usersTestPassword, true)
		if err != nil {
			t.Fatal(err)
		}
		carol, err := manager.Login("carol", usersTestPassword, true)
		if err != nil {
			t.Fatal(err)
		}

		if err := manager.ChangeOwnPassword(bobKept.Token, "wrong-password-x", "new-password-2"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("wrong old password: got %v, want ErrInvalidCredentials", err)
		}
		if _, ok := manager.Session(bobOther.Token); !ok {
			t.Fatal("a rejected password change must not discard sessions")
		}

		if err := manager.ChangeOwnPassword(bobKept.Token, usersTestPassword, "new-password-2"); err != nil {
			t.Fatal(err)
		}
		if _, ok := manager.Session(bobKept.Token); !ok {
			t.Fatal("the calling session must survive its own password change")
		}
		if _, ok := manager.Session(bobOther.Token); ok {
			t.Fatal("bob's other session must be discarded by his own password change")
		}
		if _, ok := manager.Session(carol.Token); !ok {
			t.Fatal("carol's session must survive bob's password change")
		}
		if _, err := manager.Login("bob", usersTestPassword, true); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("old password after change: got %v, want ErrInvalidCredentials", err)
		}
	})
}

func TestMigrationNoticeClearsAfterSetGuestRoles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	writeJSON(t, path, map[string]any{"users": []map[string]any{
		{"username": "admin", "password_hash": mustHash(t, usersTestPassword), "roles": []string{RoleUserManagement}},
	}})

	first := mustManager(t, path, "", "")
	if !first.MigrationNotice() {
		t.Fatal("MigrationNotice must be true after a migration")
	}
	if err := first.SetGuestRoles("admin", []string{RoleViewOverview}); err != nil {
		t.Fatal(err)
	}
	if first.MigrationNotice() {
		t.Fatal("MigrationNotice must be false after SetGuestRoles")
	}

	restarted := mustManager(t, path, "", "")
	if restarted.MigrationNotice() {
		t.Fatal("MigrationNotice must stay false after a restart")
	}
	if want := []string{RoleViewOverview}; !reflect.DeepEqual(restarted.GuestRoles(), want) {
		t.Fatalf("GuestRoles after restart: got %v, want %v", restarted.GuestRoles(), want)
	}
}
