package autolearn

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReactionsDefaultsWithoutValues(t *testing.T) {
	r := NewReactions()
	if got := r.Typical("light.a"); got != 5*time.Second {
		t.Fatalf("Typical without values = %v, want 5s", got)
	}
	if got := r.P95("light.a"); got != 5*time.Second {
		t.Fatalf("P95 without values = %v, want 5s", got)
	}
}

func TestReactionsMedianAndP95(t *testing.T) {
	r := NewReactions()
	for i := 1; i <= 20; i++ {
		r.Observe("light.a", time.Duration(i)*time.Second)
	}
	if got := r.Typical("light.a"); got != 10500*time.Millisecond {
		t.Fatalf("Typical = %v, want 10.5s", got)
	}
	if got := r.P95("light.a"); got != 19*time.Second {
		t.Fatalf("P95 = %v, want 19s", got)
	}
}

func TestReactionsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reaction.json")
	r := LoadReactions(path, nil)
	r.Observe("light.a", 1*time.Second)
	r.Observe("light.a", 5*time.Second)
	r.Observe("light.a", 2*time.Second)
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded := LoadReactions(path, nil)
	if got, want := loaded.Typical("light.a"), 2*time.Second; got != want {
		t.Fatalf("loaded Typical = %v, want %v", got, want)
	}
}

func TestReactionsBrokenFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reaction.json")
	if err := os.WriteFile(path, []byte(`{"version":9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	logged := 0
	r := LoadReactions(path, func(string, ...any) { logged++ })
	if got := r.Typical("light.a"); got != 5*time.Second {
		t.Fatalf("Typical after broken file = %v, want 5s", got)
	}
	if logged != 1 {
		t.Fatalf("logged %d times, want 1", logged)
	}
}

func TestReactionsResetDeletesStateAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reaction.json")
	r := LoadReactions(path, nil)
	r.Observe("light.a", 2*time.Second)
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := r.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if got := r.Typical("light.a"); got != 5*time.Second {
		t.Fatalf("Typical after Reset = %v, want 5s", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file after Reset: stat err = %v, want not exist", err)
	}
	if got := LoadReactions(path, nil).Typical("light.a"); got != 5*time.Second {
		t.Fatalf("reloaded Typical after Reset = %v, want 5s", got)
	}
}
