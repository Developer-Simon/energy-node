package learnrec

import (
	"bytes"
	"os"
	"path/filepath"
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
	if second != first {
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
