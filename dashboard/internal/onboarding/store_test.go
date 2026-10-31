package onboarding

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLoadMissingFileIsEmpty(t *testing.T) {
	store := NewStore(t.TempDir())
	progress := store.Load()
	if progress.SetupDone {
		t.Fatal("SetupDone = true for a missing file")
	}
	if len(progress.SetupSteps) != 0 || len(progress.Users) != 0 {
		t.Fatalf("expected empty progress, got %+v", progress)
	}
}

func TestLoadCorruptFileIsEmptyAndLogs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	progress := NewStore(dir).Load()
	if progress.SetupDone || len(progress.SetupSteps) != 0 || len(progress.Users) != 0 {
		t.Fatalf("expected empty progress, got %+v", progress)
	}
	if logBuf.Len() == 0 {
		t.Fatal("expected a warning in the log for a corrupt file")
	}
}

func TestSetStepPersists(t *testing.T) {
	dir := t.TempDir()
	if err := NewStore(dir).SetStep("x", StepLater); err != nil {
		t.Fatal(err)
	}
	progress := NewStore(dir).Load()
	if progress.SetupSteps["x"] != StepLater {
		t.Fatalf("SetupSteps[x] = %q, want %q", progress.SetupSteps["x"], StepLater)
	}
}

func TestCompleteSetupIsPerInstallation(t *testing.T) {
	dir := t.TempDir()
	if NewStore(dir).Load().SetupDone {
		t.Fatal("SetupDone = true before CompleteSetup")
	}
	if err := NewStore(dir).CompleteSetup(); err != nil {
		t.Fatal(err)
	}
	if !NewStore(dir).Load().SetupDone {
		t.Fatal("SetupDone = false after CompleteSetup")
	}
}

func TestUpdateUserIsolatesUsers(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.UpdateUser("a", func(up *UserProgress) {
		up.Welcomed = true
		up.Seen = []string{"intro"}
	}); err != nil {
		t.Fatal(err)
	}
	progress := store.Load()
	b, ok := progress.Users["b"]
	if ok || b.Welcomed || len(b.Seen) != 0 {
		t.Fatalf("user b changed by update to a: %+v", b)
	}
	a := progress.Users["a"]
	if !a.Welcomed || len(a.Seen) != 1 || a.Seen[0] != "intro" {
		t.Fatalf("user a = %+v, want welcomed with seen intro", a)
	}
}

func TestConcurrentUpdateUser(t *testing.T) {
	store := NewStore(t.TempDir())
	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- store.UpdateUser("a", func(up *UserProgress) {
				up.Seen = append(up.Seen, fmt.Sprint(i))
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := len(store.Load().Users["a"].Seen); got != n {
		t.Fatalf("len(Seen) = %d, want %d", got, n)
	}
}
