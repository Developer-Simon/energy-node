package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCycleRunnerResetClearsStateAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cycles.json")
	c := newCycleRunner(path, nil, nil, func(string, ...any) {})
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if err := c.Reset(); err != nil {
		t.Fatalf("Reset = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cycles.json still exists after Reset (err %v)", err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Save after Reset writes no file: %v", err)
	}
}
