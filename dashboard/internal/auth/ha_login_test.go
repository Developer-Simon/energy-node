package auth

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

// fakeHAVerifier zählt die Abfragen und liefert eine feste Antwort. Mit block
// wartet er, bis der Test ihn freigibt; entered meldet den Beginn der Abfrage.
type fakeHAVerifier struct {
	mu       sync.Mutex
	calls    int
	identity HAIdentity
	err      error
	block    chan struct{}
	entered  chan struct{}
}

func (f *fakeHAVerifier) VerifyHA(ctx context.Context, username, password string) (HAIdentity, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return HAIdentity{}, ctx.Err()
		}
	}
	return f.identity, f.err
}

func (f *fakeHAVerifier) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

var mariaIdentity = HAIdentity{UserID: "u1", Username: "maria", Name: "Maria", Admin: false}

// haLoginManager startet einen gekoppelten Manager mit dem Fake als Verifier.
func haLoginManager(t *testing.T, fake *fakeHAVerifier) *Manager {
	t.Helper()
	m := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
	pairForTest(t, m)
	m.SetHAVerifier(fake)
	return m
}

func TestLoginWithLocalNameNeverAsksHomeAssistant(t *testing.T) {
	fake := &fakeHAVerifier{identity: mariaIdentity}
	m, _ := loginManager(t, User{Username: "maria", PasswordHash: mustHash(t, loginMainPassword), Roles: []string{RoleViewOverview}})
	pairForTest(t, m)
	m.SetHAVerifier(fake)
	if _, err := m.Login("maria", "falsch", true); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("local name with wrong password: got %v, want ErrInvalidCredentials", err)
	}
	if fake.callCount() != 0 {
		t.Fatalf("verifier calls: got %d, want 0", fake.callCount())
	}
	if _, err := m.Login("Maria", "falsch", true); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("local name in other case: got %v, want ErrInvalidCredentials", err)
	}
	if fake.callCount() != 0 {
		t.Fatalf("verifier calls for other case: got %d, want 0", fake.callCount())
	}
}

func TestHALoginOverHTTPIsRejectedWithoutAsking(t *testing.T) {
	fake := &fakeHAVerifier{identity: mariaIdentity}
	m := haLoginManager(t, fake)
	if _, err := m.Login("maria", "pw", false); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("HA login over HTTP: got %v, want ErrInvalidCredentials", err)
	}
	if fake.callCount() != 0 {
		t.Fatalf("verifier calls: got %d, want 0", fake.callCount())
	}
}

func TestHALoginWithoutPairingIsRejected(t *testing.T) {
	fake := &fakeHAVerifier{identity: mariaIdentity}
	m := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
	m.SetHAVerifier(fake)
	if _, err := m.Login("maria", "pw", true); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("HA login without pairing: got %v, want ErrInvalidCredentials", err)
	}
	if fake.callCount() != 0 {
		t.Fatalf("verifier calls: got %d, want 0", fake.callCount())
	}
}

func TestHALoginCreatesAccountWithPreset(t *testing.T) {
	fake := &fakeHAVerifier{identity: mariaIdentity}
	m := haLoginManager(t, fake)
	if err := m.SetHARoles("admin", []string{RoleViewOverview}, []string{RoleViewOverview, RoleViewDevices}); err != nil {
		t.Fatal(err)
	}
	session, err := m.Login("maria", "pw", true)
	if err != nil {
		t.Fatal(err)
	}
	if session.User.Source != SourceHomeAssistant {
		t.Fatalf("source: got %q, want %q", session.User.Source, SourceHomeAssistant)
	}
	_, user, _ := m.HARoles()
	if !reflect.DeepEqual(session.User.Roles, Normalize(user)) {
		t.Fatalf("roles: got %v, want preset %v", session.User.Roles, Normalize(user))
	}
	if session.Via != "" {
		t.Fatalf("via: got %q, want empty", session.Via)
	}
	if got := m.HAAccounts(); len(got) != 1 || got[0].HAUserID != "u1" {
		t.Fatalf("ha accounts: got %+v, want one account u1", got)
	}
}

func TestHALoginUnavailableMapsToError(t *testing.T) {
	fake := &fakeHAVerifier{err: errors.New("x")}
	m := haLoginManager(t, fake)
	if _, err := m.Login("maria", "pw", true); !errors.Is(err, ErrHomeAssistantUnavailable) {
		t.Fatalf("unavailable: got %v, want ErrHomeAssistantUnavailable", err)
	}
}

func TestHALoginOfDisabledAccountFails(t *testing.T) {
	fake := &fakeHAVerifier{identity: mariaIdentity}
	m := haLoginManager(t, fake)
	addHAAccount(t, m, HAAccount{HAUserID: "u1", Username: "maria", Disabled: true, CreatedAt: now, LastLoginAt: now})
	if _, err := m.Login("maria", "pw", true); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("disabled account: got %v, want ErrInvalidCredentials", err)
	}
}

func TestHALoginFailuresAreLimited(t *testing.T) {
	fake := &fakeHAVerifier{err: ErrInvalidCredentials}
	m := haLoginManager(t, fake)
	for i := 0; i < 5; i++ {
		if _, err := m.Login("maria", "falsch", true); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: got %v, want ErrInvalidCredentials", i+1, err)
		}
	}
	if fake.callCount() != 5 {
		t.Fatalf("calls after five failures: got %d, want 5", fake.callCount())
	}
	if _, err := m.Login("maria", "falsch", true); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("sixth attempt: got %v, want ErrInvalidCredentials", err)
	}
	if fake.callCount() != 5 {
		t.Fatalf("sixth attempt must not ask Home Assistant: calls %d, want 5", fake.callCount())
	}
	if _, err := m.Login("anna", "falsch", true); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("other name: got %v, want ErrInvalidCredentials", err)
	}
	if fake.callCount() != 6 {
		t.Fatalf("other name must still be asked: calls %d, want 6", fake.callCount())
	}
}

func TestHALoginDoesNotHoldTheLockWhileVerifying(t *testing.T) {
	fake := &fakeHAVerifier{identity: mariaIdentity, block: make(chan struct{}), entered: make(chan struct{}, 1)}
	m := haLoginManager(t, fake)
	done := make(chan error, 1)
	go func() {
		_, err := m.Login("maria", "pw", true)
		done <- err
	}()
	select {
	case <-fake.entered:
	case <-time.After(time.Second):
		t.Fatal("verifier was not called")
	}
	listed := make(chan struct{})
	go func() {
		m.Users()
		close(listed)
	}()
	select {
	case <-listed:
	case <-time.After(time.Second):
		t.Fatal("Users blocked while the verifier was pending: Login holds m.mu")
	}
	close(fake.block)
	if err := <-done; err != nil {
		t.Fatalf("login after release: %v", err)
	}
}

func TestCompanionSessionIsMarkedAndRefreshesAccount(t *testing.T) {
	m := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
	pairForTest(t, m)
	if err := m.SetHARoles("admin", []string{RoleViewOverview}, []string{RoleViewDevices}); err != nil {
		t.Fatal(err)
	}
	addHAAccount(t, m, HAAccount{HAUserID: "u1", Username: "maria", DisplayName: "Alt", CreatedAt: now, LastLoginAt: now})
	session, err := m.CompanionSession(HAIdentity{UserID: "u1", Username: "maria", Name: "Maria neu", Admin: true})
	if err != nil {
		t.Fatal(err)
	}
	if session.Via != ViaCompanion || session.Restricted {
		t.Fatalf("via %q restricted %v, want companion and not restricted", session.Via, session.Restricted)
	}
	if session.User.DisplayName != "Maria neu" {
		t.Fatalf("display name: got %q, want Maria neu", session.User.DisplayName)
	}
	admin, _, _ := m.HARoles()
	if !reflect.DeepEqual(session.User.Roles, Normalize(admin)) {
		t.Fatalf("roles: got %v, want admin preset %v", session.User.Roles, Normalize(admin))
	}
	if accounts := m.HAAccounts(); !accounts[0].HAAdmin || accounts[0].DisplayName != "Maria neu" {
		t.Fatalf("account not refreshed: %+v", accounts[0])
	}
}

func TestCompanionSessionOfDisabledAccount(t *testing.T) {
	m := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
	pairForTest(t, m)
	addHAAccount(t, m, HAAccount{HAUserID: "u1", Username: "maria", Disabled: true, CreatedAt: now, LastLoginAt: now})
	if _, err := m.CompanionSession(mariaIdentity); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("disabled account: got %v, want ErrAccountDisabled", err)
	}
}

func TestHAUserChangedDropsSessionsOfInactiveUser(t *testing.T) {
	m := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
	addHAAccount(t, m, HAAccount{HAUserID: "u1", Username: "maria", CreatedAt: now, LastLoginAt: now})
	session := addHASession(t, m, "u1")
	if err := m.HAUserChanged(mariaIdentity, false, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Session(session.Token); ok {
		t.Fatal("session of inactive HA user still valid")
	}
	if got := m.HAAccounts(); len(got) != 1 {
		t.Fatalf("account must stay: got %d accounts", len(got))
	}
}

func TestHAUserChangedAppliesAdminFlagAtOnce(t *testing.T) {
	m := mustManager(t, filepath.Join(t.TempDir(), "users.json"), "", "")
	if err := m.SetHARoles("admin", []string{RoleViewOverview}, []string{RoleViewDevices}); err != nil {
		t.Fatal(err)
	}
	addHAAccount(t, m, HAAccount{HAUserID: "u1", Username: "maria", CreatedAt: now, LastLoginAt: now})
	session := addHASession(t, m, "u1")
	admin, _, _ := m.HARoles()
	if err := m.HAUserChanged(HAIdentity{UserID: "u1", Username: "maria", Name: "Maria", Admin: true}, true, false); err != nil {
		t.Fatal(err)
	}
	got, ok := m.Session(session.Token)
	if !ok {
		t.Fatal("session dropped although the user is active")
	}
	if !reflect.DeepEqual(got.User.Roles, Normalize(admin)) {
		t.Fatalf("roles after admin change: got %v, want %v", got.User.Roles, Normalize(admin))
	}
}
