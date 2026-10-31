package onboarding

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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

// Ein Lesefehler, der nicht "Datei fehlt" ist, darf den gespeicherten Stand
// nicht durch einen leeren überschreiben. Ein Verzeichnis unter dem Dateinamen
// liefert genau so einen Fehler (EISDIR).
func TestSetStepDoesNotOverwriteAfterReadError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, fileName)
	if err := os.Mkdir(path, 0750); err != nil {
		t.Skipf("cannot create a directory in place of the file: %v", err)
	}
	if err := NewStore(dir).SetStep("x", StepDone); err == nil {
		t.Fatal("SetStep returned nil after a read error")
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("path was modified after a read error: info=%v err=%v", info, err)
	}
}

func TestEnsureUserSetsSinceOnceAndSeedsOnlyMissingUsers(t *testing.T) {
	store := NewStore(t.TempDir())
	first := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	calls := 0
	seed := func(since time.Time) UserProgress {
		calls++
		if !since.Equal(first) {
			t.Fatalf("seed since = %v, want %v", since, first)
		}
		return UserProgress{Seen: []string{"h1"}, Baseline: true}
	}
	progress := store.EnsureUser("anna", first, seed)
	if !progress.Since.Equal(first) || !progress.Users["anna"].Baseline {
		t.Fatalf("first call = %+v", progress)
	}
	progress = store.EnsureUser("anna", first.Add(time.Hour), seed)
	if calls != 1 || !progress.Since.Equal(first) {
		t.Fatalf("second call seeded again or moved since: calls=%d since=%v", calls, progress.Since)
	}
	store.EnsureUser("ben", first.Add(time.Hour), seed)
	if calls != 2 {
		t.Fatalf("ben was not seeded, calls=%d", calls)
	}
	if got := store.Load(); !got.Since.Equal(first) || len(got.Users) != 2 || got.Users["anna"].Seen[0] != "h1" {
		t.Fatalf("saved = %+v", got)
	}
}

func TestEnsureUserKeepsAnExistingEntry(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.UpdateUser("anna", func(p *UserProgress) { p.Welcomed = true }); err != nil {
		t.Fatal(err)
	}
	progress := store.EnsureUser("anna", time.Now(), func(time.Time) UserProgress {
		t.Fatal("seed must not run for an existing entry")
		return UserProgress{}
	})
	if !progress.Users["anna"].Welcomed {
		t.Fatalf("entry changed: %+v", progress.Users["anna"])
	}
}
