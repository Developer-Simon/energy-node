package auth

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCompanionHTTPDefaultsOffAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	manager := mustManager(t, path, "", "")
	if manager.CompanionHTTPAllowed() {
		t.Fatal("companion HTTP must default to off")
	}
	if err := manager.SetCompanionHTTP("admin", true); err != nil {
		t.Fatal(err)
	}
	if !manager.CompanionHTTPAllowed() {
		t.Fatal("companion HTTP must be on after enabling")
	}
	restarted := mustManager(t, path, "", "")
	if !restarted.CompanionHTTPAllowed() {
		t.Fatal("companion HTTP must survive a restart")
	}
}

func TestTrustedMatrix(t *testing.T) {
	cases := []struct {
		name     string
		via      string
		secure   bool
		switchOn bool
		want     bool
	}{
		{"local HTTPS", "", true, false, true},
		{"local HTTP with switch", "", false, true, false},
		{"companion HTTP without switch", ViaCompanion, false, false, false},
		{"companion HTTP with switch", ViaCompanion, false, true, true},
		{"companion HTTPS without switch", ViaCompanion, true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manager := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
			if tc.switchOn {
				if err := manager.SetCompanionHTTP("admin", true); err != nil {
					t.Fatal(err)
				}
			}
			if got := manager.Trusted(Session{Via: tc.via}, tc.secure); got != tc.want {
				t.Fatalf("Trusted(via=%q, secure=%v, switch=%v): got %v, want %v", tc.via, tc.secure, tc.switchOn, got, tc.want)
			}
		})
	}
}

func TestUnpairSwitchesCompanionHTTPOff(t *testing.T) {
	manager, path := loginManager(t, User{Username: "admin", PasswordHash: mustHash(t, usersTestPassword), Roles: []string{RoleUserManagement}})
	pairForTest(t, manager)
	if err := manager.SetCompanionHTTP("admin", true); err != nil {
		t.Fatal(err)
	}
	if err := manager.Unpair("admin"); err != nil {
		t.Fatal(err)
	}
	if manager.CompanionHTTPAllowed() {
		t.Fatal("unpairing must switch companion HTTP off")
	}
	restarted := mustManager(t, path, "", "")
	if restarted.CompanionHTTPAllowed() {
		t.Fatal("companion HTTP must stay off after a restart following unpairing")
	}
}

func TestTrustedContextRoundTrip(t *testing.T) {
	if trusted, ok := TrustedFromContext(context.Background()); ok || trusted {
		t.Fatalf("empty context: got (%v, %v), want (false, false)", trusted, ok)
	}
	if trusted, ok := TrustedFromContext(WithTrusted(context.Background(), true)); !ok || !trusted {
		t.Fatalf("WithTrusted true: got (%v, %v), want (true, true)", trusted, ok)
	}
	if trusted, ok := TrustedFromContext(WithTrusted(context.Background(), false)); !ok || trusted {
		t.Fatalf("WithTrusted false: got (%v, %v), want (false, true)", trusted, ok)
	}
}
