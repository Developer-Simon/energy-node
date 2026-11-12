package auth

import (
	"reflect"
	"slices"
	"testing"
)

func TestRolesCatalogMatchesSpec(t *testing.T) {
	keys := AllRoles()
	if len(keys) != 37 || keys[0] != RoleViewOverview || keys[len(keys)-1] != RoleHTTPLogin {
		t.Fatalf("catalog order: %v", keys)
	}
	for _, key := range []string{RoleMQTTConfig, RoleUpdateSettings, RoleViewLogs, RoleSystemActions, RoleTailscale, RoleUserManagement} {
		info, _ := RoleByKey(key)
		if !info.HTTPSOnly || info.GuestAllowed {
			t.Fatalf("%s must be HTTPS-only and not guest-allowed: %+v", key, info)
		}
	}
	for _, key := range []string{RoleInstallUpdates, RoleHTTPLogin} {
		info, _ := RoleByKey(key)
		if info.HTTPSOnly || info.GuestAllowed {
			t.Fatalf("%s: %+v", key, info)
		}
	}
	if info, _ := RoleByKey(RoleHTTPLogin); info.HAAllowed {
		t.Fatal("http_login must not be allowed for HA accounts")
	}
	if info, _ := RoleByKey(RoleAutomations); info.Requires != RoleViewAutomations || info.Category != CategorySetup {
		t.Fatalf("automations: %+v", info)
	}
}

func TestNormalizeDropsUnknownAndUnmetRequirements(t *testing.T) {
	got := Normalize([]string{"automations", "bogus", "view_devices", "operate_devices", "operate_devices"})
	want := []string{"operate_devices", "view_devices"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestHasAnyView(t *testing.T) {
	if HasAnyView(User{}) {
		t.Fatal("user without roles must not have any view")
	}
	if !HasAnyView(User{Roles: []string{RoleViewStats}}) {
		t.Fatal("view_stats must count as a view")
	}
	if HasAnyView(User{Roles: []string{RoleOperateDevices}}) {
		t.Fatal("operate roles alone must not count as a view")
	}
}

func TestHasRoleRequiresTheViewPermission(t *testing.T) {
	user := User{Roles: []string{RoleAutomations}}
	if HasRole(user, RoleAutomations) {
		t.Fatal("automations without view_automations must not count")
	}
	user.Roles = append(user.Roles, RoleViewAutomations)
	if !HasRole(user, RoleAutomations) {
		t.Fatal("automations with view_automations must count")
	}
}

func TestBackupRolesInCatalog(t *testing.T) {
	keys := AllRoles()
	httpLogin := len(keys) - 1
	for _, key := range []string{RoleBackupExport, RoleBackupImport, RoleBackupSecrets} {
		info, ok := RoleByKey(key)
		if !ok {
			t.Fatalf("%s missing from the catalog", key)
		}
		if info.Category != CategorySystem || !info.HTTPSOnly || !info.HAAllowed || !info.Elevated || info.GuestAllowed || info.Requires != "" {
			t.Fatalf("%s: %+v", key, info)
		}
		if idx := slices.Index(keys, key); idx < 0 || idx >= httpLogin {
			t.Fatalf("%s must sit before http_login, index %d of %d", key, idx, len(keys))
		}
	}
}

func TestDeviceRightsInCatalog(t *testing.T) {
	analysis, ok := RoleByKey(RoleViewDeviceAnalysis)
	if !ok {
		t.Fatal("view_device_analysis missing from the catalog")
	}
	if analysis.Category != CategoryView || analysis.Requires != RoleViewDevices || !analysis.GuestAllowed || !analysis.HAAllowed {
		t.Fatalf("view_device_analysis: %+v", analysis)
	}
	appearance, ok := RoleByKey(RoleEditDeviceAppearance)
	if !ok {
		t.Fatal("edit_device_appearance missing from the catalog")
	}
	if appearance.Category != CategoryDesign || appearance.Requires != RoleViewDevices || !appearance.GuestAllowed || !appearance.HAAllowed {
		t.Fatalf("edit_device_appearance: %+v", appearance)
	}
	if got := Normalize([]string{RoleViewDeviceAnalysis}); len(got) != 0 {
		t.Fatalf("view_device_analysis without view_devices must be dropped, got %v", got)
	}
}

func TestDefaultRolesIncludeEfficiency(t *testing.T) {
	if !containsRole(defaultGuestRoles(), RoleViewEfficiency) {
		t.Errorf("defaultGuestRoles lacks view_efficiency: %v", defaultGuestRoles())
	}
	if !containsRole(defaultHAUserRoles(), RoleViewEfficiency) {
		t.Errorf("defaultHAUserRoles lacks view_efficiency: %v", defaultHAUserRoles())
	}
	if !containsRole(defaultHAAdminRoles(), RoleViewEfficiency) {
		t.Errorf("defaultHAAdminRoles lacks view_efficiency: %v", defaultHAAdminRoles())
	}
}

func TestStorageManageInCatalog(t *testing.T) {
	info, ok := RoleByKey(RoleStorageManage)
	if !ok {
		t.Fatal("storage_manage missing from the catalog")
	}
	if info.Category != CategorySystem || !info.HTTPSOnly || !info.HAAllowed || !info.Elevated || info.GuestAllowed || info.Requires != "" {
		t.Fatalf("storage_manage: %+v", info)
	}
}
