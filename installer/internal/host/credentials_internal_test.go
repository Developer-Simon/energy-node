package host

import (
	"errors"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/credstore"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func savedPassword() *credstore.Credentials {
	return &credstore.Credentials{Host: "node.local", User: "pi", Kind: "password", Secret: "hunter2"}
}

func TestWithoutAStoreThereIsNoView(t *testing.T) {
	creds := loadCredentials(nil)
	if creds.view() != nil {
		t.Fatalf("view() = %+v, want nil", creds.view())
	}
}

func TestAnUnusableKeychainHidesTheFeature(t *testing.T) {
	creds := loadCredentials(&credstore.Memory{LoadErr: errors.New("locked")})
	if creds.view() != nil {
		t.Fatalf("view() = %+v, want nil", creds.view())
	}
	if code := creds.remember(hostapi.ConnectRequest{Remember: true}, "x"); code != "" {
		t.Fatalf("remember on an unusable keychain = %q, want nothing", code)
	}
}

func TestAnEmptyKeychainGivesAnEmptyView(t *testing.T) {
	view := loadCredentials(&credstore.Memory{}).view()
	if view == nil || *view != (hostapi.SavedCredentials{}) {
		t.Fatalf("view() = %+v, want the zero value", view)
	}
}

func TestTheViewNeverCarriesTheSecret(t *testing.T) {
	view := loadCredentials(&credstore.Memory{Saved: savedPassword()}).view()
	want := hostapi.SavedCredentials{Host: "node.local", User: "pi", Kind: hostapi.AuthPassword, HasSecret: true}
	if view == nil || *view != want {
		t.Fatalf("view() = %+v, want %+v", view, want)
	}
}

func TestTheSavedSecretOnlyServesTheSameHostAndUser(t *testing.T) {
	creds := loadCredentials(&credstore.Memory{Saved: savedPassword()})
	if got, ok := creds.secret("node.local", "pi"); !ok || got != "hunter2" {
		t.Fatalf("secret(same) = %q %v", got, ok)
	}
	if _, ok := creds.secret("other.local", "pi"); ok {
		t.Fatalf("a different host got the saved password")
	}
	if _, ok := creds.secret("node.local", "root"); ok {
		t.Fatalf("a different user got the saved password")
	}
}

func TestRememberSavesThePasswordLogin(t *testing.T) {
	store := &credstore.Memory{}
	creds := loadCredentials(store)
	code := creds.remember(hostapi.ConnectRequest{Host: "node.local", User: "pi", Kind: hostapi.AuthPassword, Remember: true}, "hunter2")
	if code != "" || store.Saved == nil || *store.Saved != *savedPassword() {
		t.Fatalf("code=%q saved=%+v", code, store.Saved)
	}
	if !creds.view().HasSecret {
		t.Fatalf("the cached view did not follow the save")
	}
}

func TestRememberStoresTheKeyPathButNoSecretForAKeyLogin(t *testing.T) {
	store := &credstore.Memory{}
	creds := loadCredentials(store)
	creds.remember(hostapi.ConnectRequest{Host: "node.local", User: "pi", Kind: hostapi.AuthKey, KeyPath: "/k", Secret: "passphrase", Remember: true}, "passphrase")
	if store.Saved == nil || store.Saved.KeyPath != "/k" || store.Saved.Secret != "" {
		t.Fatalf("saved = %+v", store.Saved)
	}
}

func TestSwitchingOffDeletesTheEntry(t *testing.T) {
	store := &credstore.Memory{Saved: savedPassword()}
	creds := loadCredentials(store)
	if code := creds.remember(hostapi.ConnectRequest{Host: "node.local", User: "pi"}, "hunter2"); code != "" {
		t.Fatalf("code = %q", code)
	}
	if store.Saved != nil || store.Deletes != 1 || creds.view().Host != "" {
		t.Fatalf("store=%+v view=%+v", store, creds.view())
	}
}

func TestSwitchingOffWithNothingSavedTouchesNothing(t *testing.T) {
	store := &credstore.Memory{}
	loadCredentials(store).remember(hostapi.ConnectRequest{}, "")
	if store.Deletes != 0 || store.Saves != 0 {
		t.Fatalf("store = %+v", store)
	}
}

func TestAFailingKeychainWriteBecomesACode(t *testing.T) {
	save := loadCredentials(&credstore.Memory{SaveErr: errors.New("denied")})
	if code := save.remember(hostapi.ConnectRequest{Remember: true}, "x"); code != CodeCredentialsStoreFailed {
		t.Fatalf("save code = %q", code)
	}
	del := loadCredentials(&credstore.Memory{Saved: savedPassword(), DeleteErr: errors.New("denied")})
	if code := del.remember(hostapi.ConnectRequest{}, ""); code != CodeCredentialsStoreFailed {
		t.Fatalf("delete code = %q", code)
	}
	if del.view().Host != "node.local" {
		t.Fatalf("a failed delete must keep the cached entry")
	}
}
