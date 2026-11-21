package learnrec

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var stateT0 = time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)

func noStateLog(string, ...any) {}

func TestLoadStateFreshWhenNothingExisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "learnrec", "state.json")
	got := LoadState(path, stateT0, false, noStateLog)
	if !got.Fresh || got.FirstStart != stateT0.Unix() {
		t.Fatalf("LoadState = %+v, want fresh with first start %d", got, stateT0.Unix())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not written: %v", err)
	}
}

func TestLoadStateNotFreshWhenLearningExisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	got := LoadState(path, stateT0, true, noStateLog)
	if got.Fresh {
		t.Fatalf("LoadState = %+v, want not fresh", got)
	}
}

func TestLoadStateStableOnSecondCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	first := LoadState(path, stateT0, false, noStateLog)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	second := LoadState(path, stateT0.Add(72*time.Hour), true, noStateLog)
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("second LoadState = %+v, want %+v", second, first)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("state file rewritten:\n before %s\n after  %s", before, after)
	}
}

func TestLoadStateCorruptFileIsLoggedAndKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	broken := []byte("{kaputt")
	if err := os.WriteFile(path, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	var logs int
	count := func(string, ...any) { logs++ }

	got := LoadState(path, stateT0, false, count)
	if !got.Fresh || got.FirstStart != stateT0.Unix() {
		t.Fatalf("LoadState = %+v, want the computed fresh state", got)
	}
	if logs != 1 {
		t.Fatalf("logged %d times, want 1", logs)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, broken) {
		t.Fatalf("corrupt file was overwritten: %s", after)
	}
}

func TestLowerAutoLevelStepsDownAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := LoadState(path, stateT0, true, noStateLog)
	if got := ResolveLevel("auto", 4, st.AutoLevel); got != LevelBalanced {
		t.Fatalf("auto level before downgrade = %s, want balanced", got)
	}

	st, err := LowerAutoLevel(path, st, 4)
	if err != nil {
		t.Fatalf("LowerAutoLevel: %v", err)
	}
	if st.AutoLevel != LevelEco {
		t.Fatalf("AutoLevel after downgrade = %q, want eco", st.AutoLevel)
	}

	reloaded := LoadState(path, stateT0.Add(time.Hour), true, noStateLog)
	if reloaded.AutoLevel != LevelEco {
		t.Fatalf("persisted AutoLevel = %q, want eco", reloaded.AutoLevel)
	}
	if reloaded.FirstStart != stateT0.Unix() {
		t.Fatalf("first start changed to %d", reloaded.FirstStart)
	}
	if got := ResolveLevel("auto", 4, reloaded.AutoLevel); got != LevelEco {
		t.Fatalf("auto level after restart = %s, want eco", got)
	}
}

func TestLowerAutoLevelEcoStaysAndWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := LoadState(path, stateT0, true, noStateLog)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	got, err := LowerAutoLevel(path, st, 1)
	if err != nil {
		t.Fatalf("LowerAutoLevel on one core: %v", err)
	}
	if !reflect.DeepEqual(got, st) {
		t.Fatalf("state changed on eco: %+v, want %+v", got, st)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("state file rewritten on eco:\n before %s\n after  %s", before, after)
	}
}
