package auth

import (
	"os"
	"path/filepath"
	"reflect"
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
	if _, err := manager.Login("admin", "wrong", true); err != ErrInvalidCredentials {
		t.Fatalf("wrong password error = %v", err)
	}
	session, err := manager.Login("admin", "secret", true)
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
	if err := manager.SetGuestRoles("admin", []string{RoleViewOverview, RoleViewAutomations, RoleAutomations, RoleEditLayout, RoleCheckUpdates}); err != nil {
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

func TestManagerSessionsSurviveRestart(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	path := filepath.Join(t.TempDir(), "users.json")
	manager, err := newManager(path, "admin", "secret", clock)
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.Login("admin", "secret", true)
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
	session, err := manager.Login("admin", "secret", true)
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
	if _, err := manager.Login("admin", "secret", true); err != nil {
		t.Fatal(err)
	}
}

func TestNewGuestsOnlyView(t *testing.T) {
	manager, err := newManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.ContinueAsGuest()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{RoleViewDeviceMap, RoleViewDevices, RoleViewEnergy, RoleViewHistory, RoleViewOverview, RoleViewStats}
	if !reflect.DeepEqual(session.User.Roles, want) {
		t.Fatalf("new guest roles %v, want %v", session.User.Roles, want)
	}
	manager.mu.Lock()
	admin := manager.users["admin"]
	manager.mu.Unlock()
	if !HasRole(admin, RoleEditDeviceMap) || !HasRole(admin, RoleEditDeviceMapView) {
		t.Fatal("the bootstrap admin must keep edit_device_map and edit_device_map_view")
	}
}

func TestHADefaultRoles(t *testing.T) {
	user := defaultHAUserRoles()
	for _, key := range append(ViewRoles(), RoleOperateDevices, RoleOperateAutomations, RoleEditLayout, RoleEditDeviceAppearance, RoleEditDeviceMapView, RoleEditSettings, RoleCheckUpdates) {
		if !containsRole(user, key) {
			t.Errorf("ha user preset lacks %s", key)
		}
	}
	for _, key := range []string{RoleAutomations, RoleEditEnergy, RoleEditConfig, RoleEditSetup, RoleInstallUpdates, RoleViewLogs} {
		if containsRole(user, key) {
			t.Errorf("ha user preset has %s", key)
		}
	}
	admin := defaultHAAdminRoles()
	for _, key := range []string{RoleAutomations, RoleEditEnergy, RoleEditDeviceMap, RoleEditConfig, RoleDeleteDeviceDiscovery, RoleEditSetup, RoleInstallUpdates, RoleViewLogs, RoleCheckUpdates} {
		if !containsRole(admin, key) {
			t.Errorf("ha admin preset lacks %s", key)
		}
	}
	for _, key := range []string{RoleMQTTConfig, RoleSetupTinyTuya, RoleTailscale, RoleUpdateSettings, RoleSystemActions, RoleUserManagement, RoleHTTPLogin} {
		if containsRole(admin, key) {
			t.Errorf("ha admin preset has %s", key)
		}
	}
}
