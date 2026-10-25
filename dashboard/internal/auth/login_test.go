package auth

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const (
	loginMainPassword = "main-password-1"
	loginHTTPPassword = "http-password-1"
)

// loginManager schreibt eine users.json mit den gegebenen Konten und startet
// den Manager darauf. Der Pfad wird zurückgegeben, damit ein Neustart möglich ist.
func loginManager(t *testing.T, users ...User) (*Manager, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "users.json")
	writeJSON(t, path, userFile{Version: usersFileVersion, GuestRoles: []string{}, GuestRolesReviewed: true, Users: users})
	return mustManager(t, path, "", ""), path
}

func TestLoginOverHTTPSWithMainPasswordIsFull(t *testing.T) {
	manager, _ := loginManager(t, User{Username: "anna", PasswordHash: mustHash(t, loginMainPassword), HTTPPasswordHash: mustHash(t, loginHTTPPassword), Roles: []string{RoleHTTPLogin, RoleViewOverview}})
	session, err := manager.Login("anna", loginMainPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	if session.Restricted {
		t.Fatal("main password over HTTPS must give a full session")
	}
}

func TestLoginOverHTTPSWithHTTPPasswordIsRestricted(t *testing.T) {
	manager, _ := loginManager(t, User{Username: "anna", PasswordHash: mustHash(t, loginMainPassword), HTTPPasswordHash: mustHash(t, loginHTTPPassword), Roles: []string{RoleHTTPLogin, RoleViewOverview}})
	session, err := manager.Login("anna", loginHTTPPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	if !session.Restricted {
		t.Fatal("HTTP password over HTTPS must give a restricted session")
	}
}

func TestLoginOverHTTPWithoutHTTPLogin(t *testing.T) {
	manager, _ := loginManager(t, User{Username: "bert", PasswordHash: mustHash(t, loginMainPassword), Roles: []string{RoleViewOverview}})
	if _, err := manager.Login("bert", loginMainPassword, false); !errors.Is(err, ErrInsecureLoginNotAllowed) {
		t.Fatalf("correct password over HTTP without http_login: got %v, want ErrInsecureLoginNotAllowed", err)
	}
	if _, err := manager.Login("bert", "wrong-password-1", false); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password over HTTP without http_login: got %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginOverHTTPWithHTTPPasswordRejectsMainPassword(t *testing.T) {
	manager, _ := loginManager(t, User{Username: "anna", PasswordHash: mustHash(t, loginMainPassword), HTTPPasswordHash: mustHash(t, loginHTTPPassword), Roles: []string{RoleHTTPLogin, RoleViewOverview}})
	if _, err := manager.Login("anna", loginMainPassword, false); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("main password over HTTP with HTTP password set: got %v, want ErrInvalidCredentials", err)
	}
	session, err := manager.Login("anna", loginHTTPPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	if !session.Restricted {
		t.Fatal("HTTP password over HTTP must give a restricted session")
	}
}

func TestLoginOverHTTPWithoutHTTPPasswordUsesMainAndRestricts(t *testing.T) {
	manager, _ := loginManager(t, User{Username: "anna", PasswordHash: mustHash(t, loginMainPassword), Roles: []string{RoleHTTPLogin, RoleViewOverview}})
	session, err := manager.Login("anna", loginMainPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	if !session.Restricted {
		t.Fatal("HTTP login must always give a restricted session")
	}
}

func TestDisabledAccountCannotLogIn(t *testing.T) {
	manager, _ := loginManager(t, User{Username: "anna", PasswordHash: mustHash(t, loginMainPassword), HTTPPasswordHash: mustHash(t, loginHTTPPassword), Disabled: true, Roles: []string{RoleHTTPLogin, RoleViewOverview}})
	if _, err := manager.Login("anna", loginMainPassword, true); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("disabled account over HTTPS: got %v, want ErrInvalidCredentials", err)
	}
	if _, err := manager.Login("anna", loginHTTPPassword, false); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("disabled account over HTTP: got %v, want ErrInvalidCredentials", err)
	}
}

func TestEffectiveUserStripsHTTPSOnly(t *testing.T) {
	session := Session{User: User{Roles: []string{"view_diagnostics", "view_logs", "system_actions", "http_login", "install_updates"}}}
	if got := EffectiveUser(session, false).Roles; !reflect.DeepEqual(got, []string{"http_login", "install_updates", "view_diagnostics"}) {
		t.Fatal(got)
	}
	session.Restricted = true
	if got := EffectiveUser(session, true).Roles; len(got) != 3 {
		t.Fatal("restricted session stays restricted over HTTPS:", got)
	}
	if got := Withheld(session, true); !reflect.DeepEqual(got, []string{"view_logs", "system_actions"}) {
		t.Fatal(got)
	}
}

func TestRestrictedSurvivesRestart(t *testing.T) {
	manager, path := loginManager(t, User{Username: "anna", PasswordHash: mustHash(t, loginMainPassword), HTTPPasswordHash: mustHash(t, loginHTTPPassword), Roles: []string{RoleHTTPLogin, RoleViewOverview}})
	session, err := manager.Login("anna", loginHTTPPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	restarted := mustManager(t, path, "", "")
	restored, ok := restarted.Session(session.Token)
	if !ok || !restored.Restricted {
		t.Fatalf("restricted session after restart: ok=%v restricted=%v", ok, restored.Restricted)
	}

	// A sessions.json entry without the restricted field loads as a full session.
	sessionsPath := filepath.Join(filepath.Dir(path), "sessions.json")
	writeJSON(t, sessionsPath, sessionFile{Sessions: []storedSession{{TokenHash: tokenHash("session-legacy"), CSRFToken: "csrf-legacy", Username: "anna", ExpiresAt: now.Add(time.Hour)}}})
	legacy := mustManager(t, path, "", "")
	full, ok := legacy.Session("session-legacy")
	if !ok {
		t.Fatal("legacy session entry was not loaded")
	}
	if full.Restricted {
		t.Fatal("session entry without the restricted field must not be restricted")
	}
}
