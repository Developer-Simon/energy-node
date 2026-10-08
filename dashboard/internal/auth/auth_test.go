package auth

import (
	"os"
	"path/filepath"
	"strings"
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
	if !HasRole(session.User, RoleCheckUpdates) {
		t.Fatalf("bootstrap admin missing RoleCheckUpdates")
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
	if !HasRole(guest.User, RoleCheckUpdates) {
		t.Fatalf("guest missing the provisional RoleCheckUpdates grant")
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

// An admin that predates a role keeps its stored role list, so every role
// added after the first bootstrap has to be backfilled on load -- otherwise
// the existing admin is the one account locked out of the new feature.
func TestManagerBackfillsRolesForExistingAdmin(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "users.json")
	stored := `{"users":[{"username":"admin","password_hash":"x","roles":["system_actions"],` +
		`"created_at":"2026-01-01T00:00:00Z","last_login_at":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(stored), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := newManager(path, "admin", "secret", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	admin := manager.users["admin"]
	manager.mu.Unlock()
	for _, role := range []string{RoleDeleteDeviceDiscovery, RoleTuneLiveUpdates, RoleMQTTConfig, RoleAutomations, RoleEditLayout, RoleCheckUpdates, RoleEditEnergy} {
		if !HasRole(admin, role) {
			t.Errorf("existing admin was not backfilled with %q", role)
		}
	}
	reloaded, err := newManager(path, "admin", "secret", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	reloaded.mu.Lock()
	defer reloaded.mu.Unlock()
	if !HasRole(reloaded.users["admin"], RoleCheckUpdates) {
		t.Error("backfilled roles were not persisted to users.json")
	}
}

func TestManagerSessionsSurviveRestart(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	path := filepath.Join(t.TempDir(), "users.json")
	manager, err := newManager(path, "admin", "secret", clock)
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.Login("admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	guest, err := manager.ContinueAsGuest()
	if err != nil {
		t.Fatal(err)
	}

	sessionsPath := filepath.Join(filepath.Dir(path), "sessions.json")
	raw, err := os.ReadFile(sessionsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), session.Token) || strings.Contains(string(raw), guest.Token) {
		t.Fatal("sessions.json holds a raw session token")
	}
	if info, err := os.Stat(sessionsPath); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0600 {
		t.Fatalf("sessions file mode = %o, want 600", info.Mode().Perm())
	}

	restarted, err := newManager(path, "admin", "secret", clock)
	if err != nil {
		t.Fatal(err)
	}
	restored, ok := restarted.Session(session.Token)
	if !ok || restored.User.Username != "admin" || restored.Token != session.Token || !HasRole(restored.User, RoleSystemActions) {
		t.Fatalf("admin session not restored: %#v ok=%v", restored, ok)
	}
	if !restarted.ValidateCSRF(session.Token, session.CSRFToken) {
		t.Fatal("restored session lost its CSRF token")
	}
	if _, ok := restarted.Session(guest.Token); !ok {
		t.Fatal("guest session not restored")
	}

	restarted.Logout(session.Token)
	again, err := newManager(path, "admin", "secret", clock)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Session(session.Token); ok {
		t.Fatal("logged-out session came back after a restart")
	}
	if _, ok := again.Session(guest.Token); !ok {
		t.Fatal("logout of one session dropped another")
	}
}

func TestManagerDropsExpiredAndOrphanedSessionsOnLoad(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "users.json")
	manager, err := newManager(path, "admin", "secret", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.Login("admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	guest, err := manager.ContinueAsGuest()
	if err != nil {
		t.Fatal(err)
	}

	// A day later the admin session has run out, the guest session not.
	later := now.Add(sessionLifetime + time.Minute)
	restarted, err := newManager(path, "admin", "secret", func() time.Time { return later })
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := restarted.Session(session.Token); ok {
		t.Fatal("expired session restored")
	}
	if _, ok := restarted.Session(guest.Token); !ok {
		t.Fatal("live guest session not restored")
	}

	// Past the guest lifetime the guest user itself is cleaned up.
	muchLater := now.Add(guestLifetime + time.Hour)
	cleaned, err := newManager(path, "admin", "secret", func() time.Time { return muchLater })
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cleaned.Session(guest.Token); ok {
		t.Fatal("session of a removed guest restored")
	}
}

func TestManagerStartsWithBrokenSessionsFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sessions.json"), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := newManager(filepath.Join(dir, "users.json"), "admin", "secret", time.Now)
	if err != nil {
		t.Fatalf("broken sessions.json blocked the start: %v", err)
	}
	if _, err := manager.Login("admin", "secret"); err != nil {
		t.Fatal(err)
	}
}

func TestManagerBackfillsEditEnergyForExistingAdminAndGuests(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "users.json")
	stored := `{"users":[` +
		`{"username":"admin","password_hash":"x","roles":["system_actions"],"created_at":"2026-10-01T00:00:00Z","last_login_at":"2026-10-08T00:00:00Z"},` +
		`{"username":"guest-old","guest":true,"roles":["automations","edit_layout","check_updates"],"created_at":"2026-10-07T00:00:00Z","last_login_at":"2026-10-08T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(stored), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := newManager(path, "admin", "secret", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	admin, guest := manager.users["admin"], manager.users["guest-old"]
	manager.mu.Unlock()
	if !HasRole(admin, RoleEditEnergy) {
		t.Error("existing admin was not backfilled with edit_energy")
	}
	if !HasRole(guest, RoleEditEnergy) {
		t.Error("existing guest was not backfilled with edit_energy")
	}
}

func TestNewGuestsGetEditEnergy(t *testing.T) {
	manager, err := newManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.ContinueAsGuest()
	if err != nil {
		t.Fatal(err)
	}
	if !HasRole(session.User, RoleEditEnergy) {
		t.Fatal("a new guest must get edit_energy until there is a user management")
	}
}
