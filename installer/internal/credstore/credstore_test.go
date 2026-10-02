package credstore_test

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/Developer-Simon/energy-node-installer/internal/credstore"
)

var testRing = credstore.Keyring{Service: "energy-node-installer-test", Account: "last"}

func TestLoadReportsNothingSavedOnAnEmptyKeyring(t *testing.T) {
	keyring.MockInit()
	_, found, err := testRing.Load()
	if err != nil || found {
		t.Fatalf("Load() found=%v err=%v, want nothing and no error", found, err)
	}
}

func TestSaveThenLoadRoundTripsEveryField(t *testing.T) {
	keyring.MockInit()
	want := credstore.Credentials{Host: "node.local", User: "pi", Kind: "password", Secret: "hunter2"}
	if err := testRing.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, found, err := testRing.Load()
	if err != nil || !found || got != want {
		t.Fatalf("Load() = %+v found=%v err=%v, want %+v", got, found, err, want)
	}
}

func TestDeleteRemovesTheEntryAndToleratesAMissingOne(t *testing.T) {
	keyring.MockInit()
	if err := testRing.Delete(); err != nil {
		t.Fatalf("Delete on an empty keyring: %v", err)
	}
	_ = testRing.Save(credstore.Credentials{Host: "node.local", User: "pi"})
	if err := testRing.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, found, _ := testRing.Load(); found {
		t.Fatalf("the entry survived Delete")
	}
}

func TestLoadFailsWhenTheKeyringIsUnusable(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	if _, _, err := testRing.Load(); err == nil {
		t.Fatalf("Load must fail without a usable keyring")
	}
}

func TestAnUnreadableEntryCountsAsNothingSaved(t *testing.T) {
	keyring.MockInit()
	_ = keyring.Set(testRing.Service, testRing.Account, "not json")
	_, found, err := testRing.Load()
	if err != nil || found {
		t.Fatalf("Load() found=%v err=%v, want nothing and no error", found, err)
	}
}

func TestMemoryCountsSavesAndDeletes(t *testing.T) {
	store := &credstore.Memory{}
	_ = store.Save(credstore.Credentials{Host: "a"})
	_ = store.Delete()
	if store.Saves != 1 || store.Deletes != 1 || store.Saved != nil {
		t.Fatalf("Memory = %+v", store)
	}
}
