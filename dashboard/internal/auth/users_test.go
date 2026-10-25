package auth

import (
	"bytes"
	"errors"
	"os"
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
