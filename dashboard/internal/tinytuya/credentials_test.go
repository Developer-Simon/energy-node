package tinytuya

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialStoreRoundTripUsesPrivateFile(t *testing.T) {
	store := NewCredentialStore(t.TempDir())
	want := Credentials{Region: "eu", AccessID: "access-id", AccessSecret: "access-secret"}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("credentials = %#v, want %#v", got, want)
	}
	info, err := os.Stat(filepath.Join(filepath.Dir(store.path), "tinytuya_credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("credentials file mode = %o, want 600", info.Mode().Perm())
	}
}

func TestCredentialStoreRejectsIncompleteCredentials(t *testing.T) {
	store := NewCredentialStore(t.TempDir())
	if err := store.Save(Credentials{Region: "eu", AccessID: "id"}); err == nil {
		t.Fatal("incomplete credentials were accepted")
	}
	if _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "open") {
		t.Fatalf("load error = %v, want missing-file error", err)
	}
}
