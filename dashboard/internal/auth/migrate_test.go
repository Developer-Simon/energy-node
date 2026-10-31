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
	if !reflect.DeepEqual(guest.Roles, defaultGuestRoles()) {
		t.Errorf("guest roles %v, want the new view-only default", guest.Roles)
	}
	file := readUsersFile(t, path)
	if file.Version != 5 || !reflect.DeepEqual(file.GuestRoles, guest.Roles) {
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
	if file.Version != 5 || !reflect.DeepEqual(file.GuestRoles, defaultGuestRoles()) {
		t.Fatalf("bootstrap file version %d guest_roles %v", file.Version, file.GuestRoles)
	}
	if manager.MigrationNotice() {
		t.Fatal("a fresh install must not show the migration notice")
	}

	// A second bootstrap password does not replace the existing admin.
	again := mustManager(t, path, "admin", "other-pass")
	if _, err := again.Login("admin", "secret-pass", true); err != nil {
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
	manager := mustManager(t, path, "admin", "")
	if !reflect.DeepEqual(manager.users["admin"].Roles, []string{"view_overview"}) {
		t.Fatalf("admin roles changed on start: %v", manager.users["admin"].Roles)
	}
	// Die Version 2 wird nur auf Version 3 gehoben, die Rollen bleiben gleich.
	if file := readUsersFile(t, path); file.Version != 5 || !reflect.DeepEqual(file.GuestRoles, []string{"view_overview"}) {
		t.Fatalf("version 2 file after start: version %d guest_roles %v", file.Version, file.GuestRoles)
	}
}

func TestUnknownVersionAbortsStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	writeJSON(t, path, map[string]any{"version": usersFileVersion + 1, "users": []any{}})
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

// TestMigrateV2SplitsSetupPermissions prueft die Rollenumschreibung von
// Version 2 nach Version 3 an den Eingaben, die eine Datei haben kann.
func TestMigrateV2SplitsSetupPermissions(t *testing.T) {
	cases := []struct {
		name  string
		roles []string
		guest bool
		want  []string
	}{
		{"live updates become setup", []string{RoleViewConfig, "tune_live_updates"}, false, []string{RoleViewConfig, RoleEditSetup}},
		{"design settings give setup", []string{RoleEditSettings}, false, []string{RoleEditSettings, RoleEditSetup}},
		{"config editor gets tiny tuya", []string{RoleViewConfig, RoleEditConfig}, false, []string{RoleViewConfig, RoleEditConfig, RoleSetupTinyTuya}},
		{"guest config editor does not get tiny tuya", []string{RoleViewConfig, RoleEditConfig}, true, []string{RoleViewConfig, RoleEditConfig}},
		{"edit_config without view_config is no right", []string{RoleEditConfig}, false, []string{RoleEditConfig}},
		{"nothing to add", []string{RoleViewOverview}, false, []string{RoleViewOverview}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Normalize(migrateV2(tc.roles, tc.guest))
			if !reflect.DeepEqual(got, Normalize(tc.want)) {
				t.Fatalf("migrateV2(%v, guest=%v) = %v, want %v", tc.roles, tc.guest, got, Normalize(tc.want))
			}
		})
	}
}

func TestMigrationFromVersion2KeepsEveryRight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	writeJSON(t, path, map[string]any{
		"version":              2,
		"guest_roles":          []string{RoleViewOverview, RoleViewConfig, RoleEditSettings, RoleEditConfig, "tune_live_updates"},
		"guest_roles_reviewed": true,
		"users": []map[string]any{
			{"username": "admin", "password_hash": mustHash(t, "secret-pass"), "roles": []string{RoleViewConfig, RoleViewHistory, RoleEditSettings, RoleEditConfig, "tune_live_updates"}},
			{"username": "setup", "password_hash": mustHash(t, "secret-pass"), "roles": []string{RoleViewConfig, "tune_live_updates"}},
		},
	})
	manager := mustManager(t, path, "admin", "")

	admin := manager.users["admin"]
	for _, key := range []string{RoleViewHistory, RoleEditSettings, RoleEditConfig, RoleEditSetup, RoleSetupTinyTuya} {
		if !HasRole(admin, key) {
			t.Errorf("admin lacks %s after migration: %v", key, admin.Roles)
		}
	}
	if containsRole(admin.Roles, "tune_live_updates") {
		t.Fatalf("admin still has the removed key: %v", admin.Roles)
	}

	setup := manager.users["setup"]
	if !HasRole(setup, RoleEditSetup) || HasRole(setup, RoleSetupTinyTuya) {
		t.Errorf("setup account roles after migration: %v", setup.Roles)
	}
	if HasRole(setup, RoleEditSettings) {
		t.Errorf("setup account gained the design permission: %v", setup.Roles)
	}

	if !HasRole(User{Roles: manager.GuestRoles(), Guest: true}, RoleEditSetup) {
		t.Fatalf("guest lost the live update right: %v", manager.GuestRoles())
	}
	if containsRole(manager.GuestRoles(), RoleSetupTinyTuya) {
		t.Fatalf("guest got TinyTuya setup: %v", manager.GuestRoles())
	}

	file := readUsersFile(t, path)
	if file.Version != 5 {
		t.Fatalf("file version %d, want 5", file.Version)
	}
	if manager.MigrationNotice() || !file.GuestRolesReviewed {
		t.Fatal("version 2 review state must be kept, no new guest notice")
	}
	// Ein zweiter Start aendert nichts mehr.
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mustManager(t, path, "admin", "")
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("second start after the version 3 migration changed users.json")
	}
}

func TestMigrationFromVersion1GivesSetupPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	writeJSON(t, path, map[string]any{"users": []map[string]any{
		{"username": "admin", "password_hash": mustHash(t, "secret-pass"), "roles": []string{"system_actions", "tune_live_updates"}},
	}})
	manager := mustManager(t, path, "admin", "")
	admin := manager.users["admin"]
	if !HasRole(admin, RoleEditSetup) || !HasRole(admin, RoleSetupTinyTuya) {
		t.Fatalf("version 1 admin lacks the setup rights: %v", admin.Roles)
	}
	if containsRole(admin.Roles, "tune_live_updates") {
		t.Fatalf("version 1 admin keeps the removed key: %v", admin.Roles)
	}
	if !reflect.DeepEqual(manager.GuestRoles(), defaultGuestRoles()) {
		t.Fatalf("default guest roles: %v", manager.GuestRoles())
	}
	if file := readUsersFile(t, path); file.Version != 5 {
		t.Fatalf("file version %d, want 5", file.Version)
	}
}

func TestMigrationFromVersion3GivesDeviceRights(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	writeJSON(t, path, map[string]any{
		"version":              3,
		"guest_roles":          []string{RoleViewOverview, RoleViewDevices, RoleEditLayout},
		"guest_roles_reviewed": true,
		"users": []map[string]any{
			{"username": "admin", "password_hash": mustHash(t, "secret-pass"), "roles": []string{RoleViewDevices, RoleEditLayout, RoleViewOverview}},
			{"username": "viewer", "password_hash": mustHash(t, "secret-pass"), "roles": []string{RoleViewDevices}},
		},
		"ha_roles": map[string]any{
			"admin": []string{RoleViewDevices, RoleEditLayout, RoleViewOverview},
			"user":  []string{RoleViewDevices},
		},
		"ha_users": []map[string]any{
			{"ha_user_id": "ha-1", "username": "maria", "display_name": "Maria", "roles_custom": true, "roles": []string{RoleViewDevices}, "created_at": now, "last_login_at": now},
		},
	})
	manager := mustManager(t, path, "admin", "")

	admin := manager.users["admin"]
	if !HasRole(admin, RoleViewDeviceAnalysis) || !HasRole(admin, RoleEditDeviceAppearance) {
		t.Errorf("admin lacks the device rights: %v", admin.Roles)
	}
	viewer := manager.users["viewer"]
	if !HasRole(viewer, RoleViewDeviceAnalysis) || HasRole(viewer, RoleEditDeviceAppearance) {
		t.Errorf("viewer device rights: %v", viewer.Roles)
	}
	if !HasRole(User{Roles: manager.GuestRoles(), Guest: true}, RoleViewDeviceAnalysis) || !HasRole(User{Roles: manager.GuestRoles(), Guest: true}, RoleEditDeviceAppearance) {
		t.Errorf("guest default roles: %v", manager.GuestRoles())
	}

	admin18, user18, set := manager.HARoles()
	if !set || !containsRole(admin18, RoleViewDeviceAnalysis) || !containsRole(admin18, RoleEditDeviceAppearance) {
		t.Errorf("ha admin roles: %v", admin18)
	}
	if !containsRole(user18, RoleViewDeviceAnalysis) || containsRole(user18, RoleEditDeviceAppearance) {
		t.Errorf("ha user roles: %v", user18)
	}
	accounts := manager.HAAccounts()
	if len(accounts) != 1 || !containsRole(accounts[0].Roles, RoleViewDeviceAnalysis) {
		t.Errorf("ha account roles: %v", accounts)
	}

	file := readUsersFile(t, path)
	if file.Version != 5 {
		t.Fatalf("file version %d, want 5", file.Version)
	}
	if manager.MigrationNotice() {
		t.Fatal("version 3 migration must not raise the guest notice")
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mustManager(t, path, "admin", "")
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("second start after the version 4 migration changed users.json")
	}
}

func TestMigrateV3WithoutSourceRightsAddsNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	writeJSON(t, path, map[string]any{
		"version":              3,
		"guest_roles":          []string{RoleViewOverview},
		"guest_roles_reviewed": true,
		"users": []map[string]any{
			{"username": "admin", "password_hash": mustHash(t, "secret-pass"), "roles": []string{RoleViewOverview}},
		},
	})
	manager := mustManager(t, path, "admin", "")
	if roles := manager.users["admin"].Roles; !reflect.DeepEqual(roles, []string{RoleViewOverview}) {
		t.Fatalf("admin roles %v, want only view_overview", roles)
	}
	if roles := manager.GuestRoles(); !reflect.DeepEqual(roles, []string{RoleViewOverview}) {
		t.Fatalf("guest roles %v, want only view_overview", roles)
	}
}

func TestMigrationFromVersion4ReplacesUntouchedPresets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	legacy := legacyDefaultGuestRoles()
	writeJSON(t, path, map[string]any{
		"version":              4,
		"guest_roles":          legacy,
		"guest_roles_reviewed": true,
		"users": []map[string]any{
			{"username": "admin", "password_hash": mustHash(t, "secret-pass"), "roles": bootstrapAdminRoles()},
			{"username": "guest-abc", "guest": true, "roles": legacy, "last_login_at": now},
		},
		"ha_roles": map[string]any{"admin": legacy, "user": legacy},
		"ha_users": []map[string]any{
			{"ha_user_id": "ha-1", "username": "maria", "display_name": "Maria", "roles_custom": true, "roles": legacy, "created_at": now, "last_login_at": now},
		},
	})
	manager := mustManager(t, path, "admin", "")
	if got := manager.GuestRoles(); !reflect.DeepEqual(got, defaultGuestRoles()) {
		t.Errorf("guest preset %v, want %v", got, defaultGuestRoles())
	}
	if got := manager.users["guest-abc"].Roles; !reflect.DeepEqual(got, defaultGuestRoles()) {
		t.Errorf("guest account roles %v", got)
	}
	admin, user, _ := manager.HARoles()
	if !reflect.DeepEqual(admin, defaultHAAdminRoles()) || !reflect.DeepEqual(user, defaultHAUserRoles()) {
		t.Errorf("ha presets admin %v user %v", admin, user)
	}
	if accounts := manager.HAAccounts(); !reflect.DeepEqual(accounts[0].Roles, Normalize(legacy)) {
		t.Errorf("custom ha account changed: %v", accounts[0].Roles)
	}
	if !manager.MigrationNotice() {
		t.Error("replacing the guest preset must raise the notice")
	}
	if file := readUsersFile(t, path); file.Version != 5 {
		t.Fatalf("file version %d, want 5", file.Version)
	}
	before, _ := os.ReadFile(path)
	mustManager(t, path, "admin", "")
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("second start after the version 5 migration changed users.json")
	}
}

func TestMigrationFromVersion4KeepsChangedPresets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	writeJSON(t, path, map[string]any{
		"version":              4,
		"guest_roles":          []string{RoleViewOverview, RoleEditLayout},
		"guest_roles_reviewed": true,
		"users": []map[string]any{
			{"username": "admin", "password_hash": mustHash(t, "secret-pass"), "roles": bootstrapAdminRoles()},
		},
		"ha_roles": map[string]any{"admin": legacyDefaultGuestRoles(), "user": []string{RoleViewOverview}},
	})
	manager := mustManager(t, path, "admin", "")
	if got := manager.GuestRoles(); !reflect.DeepEqual(got, []string{RoleEditLayout, RoleViewOverview}) {
		t.Errorf("changed guest preset replaced: %v", got)
	}
	admin, user, _ := manager.HARoles()
	if !reflect.DeepEqual(admin, Normalize(legacyDefaultGuestRoles())) || !reflect.DeepEqual(user, []string{RoleViewOverview}) {
		t.Errorf("ha presets with one changed side must stay: admin %v user %v", admin, user)
	}
	if manager.MigrationNotice() {
		t.Error("no replacement, no notice")
	}
}

func TestMigrationFromVersion4RestoredGuestSessionGetsNewRoles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	legacy := legacyDefaultGuestRoles()
	writeJSON(t, path, map[string]any{
		"version":              4,
		"guest_roles":          legacy,
		"guest_roles_reviewed": true,
		"users": []map[string]any{
			{"username": "admin", "password_hash": mustHash(t, "secret-pass"), "roles": bootstrapAdminRoles()},
			{"username": "guest-abc", "guest": true, "roles": legacy, "last_login_at": now},
		},
	})
	const token = "guest-token"
	writeJSON(t, filepath.Join(dir, "sessions.json"), sessionFile{Sessions: []storedSession{
		{TokenHash: tokenHash(token), CSRFToken: "csrf", Username: "guest-abc", ExpiresAt: now.Add(time.Hour)},
	}})
	manager := mustManager(t, path, "admin", "")
	session, ok := manager.Session(token)
	if !ok {
		t.Fatal("restored guest session missing")
	}
	if !reflect.DeepEqual(session.User.Roles, defaultGuestRoles()) {
		t.Errorf("restored guest session roles %v, want %v", session.User.Roles, defaultGuestRoles())
	}
}

func TestMigrationFromVersion4LegacyGuestAccountGetsNewDefaultWithCustomPreset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	customPreset := []string{RoleViewOverview, RoleEditLayout}
	writeJSON(t, path, map[string]any{
		"version":              4,
		"guest_roles":          customPreset,
		"guest_roles_reviewed": true,
		"users": []map[string]any{
			{"username": "admin", "password_hash": mustHash(t, "secret-pass"), "roles": bootstrapAdminRoles()},
			{"username": "guest-abc", "guest": true, "roles": legacyDefaultGuestRoles(), "last_login_at": now},
		},
	})
	manager := mustManager(t, path, "admin", "")
	if got := manager.GuestRoles(); !reflect.DeepEqual(got, Normalize(customPreset)) {
		t.Errorf("customized guest preset changed: %v", got)
	}
	if got := manager.users["guest-abc"].Roles; !reflect.DeepEqual(got, defaultGuestRoles()) {
		t.Errorf("legacy guest account roles %v, want %v", got, defaultGuestRoles())
	}
	if manager.MigrationNotice() {
		t.Error("no preset replacement, no notice")
	}
}
