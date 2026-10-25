package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// now ist der feste Zeitpunkt aller Migrationstests. Der Gast darf nicht
// abgelaufen sein.
var now = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

func viewKeys() []string {
	return []string{"view_overview", "view_devices", "view_history", "view_stats", "view_energy", "view_device_map", "view_config", "view_automations", "view_diagnostics"}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func mustHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func mustManager(t *testing.T, path, bootstrapUser, bootstrapPassword string) *Manager {
	t.Helper()
	manager, err := newManager(path, bootstrapUser, bootstrapPassword, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func readUsersFile(t *testing.T, path string) userFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file userFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestMigrationKeepsBehaviorOfFieldFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	// So sieht users.json heute auf einem Gerät aus: keine Version, Admin mit zehn Rollen, ein Gast mit sechs.
	writeJSON(t, path, map[string]any{"users": []map[string]any{
		{"username": "admin", "password_hash": mustHash(t, "secret-pass"), "roles": []string{"system_actions", "delete_device_discovery", "tune_live_updates", "mqtt_config", "automations", "edit_layout", "check_updates", "edit_energy", "edit_device_map", "edit_device_map_view"}},
		{"username": "guest-abc", "guest": true, "roles": []string{"automations", "edit_layout", "check_updates", "edit_energy", "edit_device_map", "edit_device_map_view"}, "last_login_at": now},
	}})
	manager := mustManager(t, path, "admin", "")
	admin := manager.users["admin"]
	for _, key := range append(viewKeys(), "operate_devices", "edit_settings", "edit_config", "operate_automations", "install_updates", "update_settings", "tailscale", "view_logs", "user_management", "system_actions") {
		if !HasRole(admin, key) {
			t.Errorf("admin lacks %s", key)
		}
	}
	if HasRole(admin, RoleHTTPLogin) {
		t.Error("admin must not get http_login")
	}
	guest := manager.users["guest-abc"]
	for _, key := range []string{"system_actions", "user_management", "install_updates", "view_logs"} {
		if HasRole(guest, key) {
			t.Errorf("guest got %s", key)
		}
	}
	for _, key := range append(viewKeys(), "automations", "operate_automations", "edit_settings", "edit_config", "operate_devices", "edit_layout") {
		if !HasRole(guest, key) {
			t.Errorf("guest lacks %s", key)
		}
	}
	file := readUsersFile(t, path)
	if file.Version != 2 || !reflect.DeepEqual(file.GuestRoles, guest.Roles) {
		t.Fatalf("file not migrated: version %d guest_roles %v", file.Version, file.GuestRoles)
	}
	if !manager.MigrationNotice() {
		t.Fatal("migration must raise the guest notice")
	}
}

func TestMigrationRunsOnlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	writeJSON(t, path, map[string]any{"users": []map[string]any{
		{"username": "admin", "password_hash": mustHash(t, "secret-pass"), "roles": []string{"system_actions"}},
	}})
	first := mustManager(t, path, "admin", "")

	// Der Admin verliert view_history nach der Migration. Das ist eine
	// bewusste Entscheidung und darf beim naechsten Start nicht rueckgaengig
	// gemacht werden.
	first.mu.Lock()
	admin := first.users["admin"]
	admin.Roles = Normalize(removeRole(admin.Roles, "view_history"))
	first.users["admin"] = admin
	if err := first.saveLocked(); err != nil {
		first.mu.Unlock()
		t.Fatal(err)
	}
	first.mu.Unlock()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	second := mustManager(t, path, "admin", "")
	if HasRole(second.users["admin"], "view_history") {
		t.Fatal("second start migrated again and restored view_history")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("second start changed users.json")
	}
}

func TestBootstrapOnlyWithoutNonGuestUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	writeJSON(t, path, map[string]any{"users": []any{}})
	manager := mustManager(t, path, "admin", "secret-pass")
	admin := manager.users["admin"]
	if !reflect.DeepEqual(admin.Roles, Normalize(bootstrapAdminRoles())) {
		t.Fatalf("bootstrap admin roles = %v", admin.Roles)
	}
	if HasRole(admin, RoleHTTPLogin) {
		t.Fatal("bootstrap admin must not get http_login")
	}
	file := readUsersFile(t, path)
	if file.Version != 2 || !reflect.DeepEqual(file.GuestRoles, defaultGuestRoles()) {
		t.Fatalf("bootstrap file version %d guest_roles %v", file.Version, file.GuestRoles)
	}
	if manager.MigrationNotice() {
		t.Fatal("a fresh install must not show the migration notice")
	}

	// A second bootstrap password does not replace the existing admin.
	again := mustManager(t, path, "admin", "other-pass")
	if _, err := again.Login("admin", "secret-pass"); err != nil {
		t.Fatalf("existing admin lost its password: %v", err)
	}
}

func TestStartNoLongerBackfillsRoles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	writeJSON(t, path, map[string]any{
		"version":     2,
		"guest_roles": []string{"view_overview"},
		"users": []map[string]any{
			{"username": "admin", "password_hash": mustHash(t, "secret-pass"), "roles": []string{"view_overview"}},
		},
	})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manager := mustManager(t, path, "admin", "")
	if !reflect.DeepEqual(manager.users["admin"].Roles, []string{"view_overview"}) {
		t.Fatalf("admin roles changed on start: %v", manager.users["admin"].Roles)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("start rewrote a version 2 file")
	}
}

func TestUnknownVersionAbortsStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	writeJSON(t, path, map[string]any{"version": 3, "users": []any{}})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = newManager(path, "admin", "secret-pass", func() time.Time { return now })
	if !errors.Is(err, ErrUnsupportedUsersVersion) {
		t.Fatalf("error = %v, want ErrUnsupportedUsersVersion", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("unsupported file was changed")
	}
}

func TestNewGuestGetsGuestRolesFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	writeJSON(t, path, map[string]any{
		"version":              2,
		"guest_roles":          []string{"view_overview"},
		"guest_roles_reviewed": true,
		"users": []map[string]any{
			{"username": "admin", "password_hash": mustHash(t, "secret-pass"), "roles": []string{"system_actions"}},
		},
	})
	manager := mustManager(t, path, "admin", "")
	guest, err := manager.ContinueAsGuest()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(guest.User.Roles, []string{"view_overview"}) {
		t.Fatalf("guest roles = %v, want the file's guest_roles", guest.User.Roles)
	}
}

func removeRole(roles []string, role string) []string {
	out := make([]string, 0, len(roles))
	for _, candidate := range roles {
		if candidate != role {
			out = append(out, candidate)
		}
	}
	return out
}
