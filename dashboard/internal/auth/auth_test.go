package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerBootstrapsAdminAndCreatesSession(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	manager, err := newManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Login("admin", "wrong"); err != ErrInvalidCredentials {
		t.Fatalf("wrong password error = %v", err)
	}
	session, err := manager.Login("admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if session.User.Username != "admin" || !HasRole(session.User, RoleSystemActions) {
		t.Fatalf("unexpected session user: %#v", session.User)
	}
	if !HasRole(session.User, RoleAutomations) {
		t.Fatalf("bootstrap admin missing RoleAutomations")
	}
	if !HasRole(session.User, RoleEditLayout) {
		t.Fatalf("bootstrap admin missing RoleEditLayout")
	}
	if !manager.ValidateCSRF(session.Token, session.CSRFToken) || manager.ValidateCSRF(session.Token, "wrong") {
		t.Fatal("CSRF validation result is incorrect")
	}
	if info, err := os.Stat(filepath.Join(filepath.Dir(manager.path), "users.json")); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0600 {
		t.Fatalf("users file mode = %o, want 600", info.Mode().Perm())
	}
}

func TestManagerCreatesAndCleansGuestUsers(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "users.json")
	manager, err := newManager(path, "", "", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	guest, err := manager.ContinueAsGuest()
	if err != nil {
		t.Fatal(err)
	}
	if !guest.User.Guest || guest.User.Username == "" {
		t.Fatalf("unexpected guest session: %#v", guest.User)
	}
	if !HasRole(guest.User, RoleAutomations) {
		t.Fatalf("guest missing the provisional RoleAutomations grant")
	}
	if !HasRole(guest.User, RoleEditLayout) {
		t.Fatalf("guest missing the provisional RoleEditLayout grant")
	}
	manager.mu.Lock()
	old := guest.User
	old.LastLoginAt = now.Add(-guestLifetime - time.Minute)
	manager.users[old.Username] = old
	if err := manager.saveLocked(); err != nil {
		manager.mu.Unlock()
		t.Fatal(err)
	}
	manager.mu.Unlock()
	cleaned, err := newManager(path, "", "", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	cleaned.mu.Lock()
	defer cleaned.mu.Unlock()
	if _, ok := cleaned.users[guest.User.Username]; ok {
		t.Fatal("expired guest was not removed")
	}
}
