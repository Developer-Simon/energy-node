package autolearn

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestStoreRoundTripKeepsSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observer.json")
	o := LoadObserver(path, time.UTC, t.Logf)
	o.ObserveTriggers(testBase, map[string]int{"r1": 0, "r2": 4})
	o.ObserveTriggers(testBase.Add(time.Minute), map[string]int{"r1": 2, "r2": 6})
	o.ObserveValues(testBase, map[string]float64{"k": 3.5})
	o.ObserveValues(testBase.Add(time.Hour), map[string]float64{"k": -1.25})
	want := o.Snapshot()
	if err := o.Save(); err != nil {
		t.Fatal(err)
	}

	again := LoadObserver(path, time.UTC, t.Logf)
	if got := again.Snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot after reload differs:\n got %+v\nwant %+v", got, want)
	}
}

func TestStoreMissingFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	o := LoadObserver(path, time.UTC, t.Logf)
	if st := o.Snapshot(); len(st.Rules) != 0 || len(st.Values) != 0 {
		t.Fatalf("missing file gave data: %+v", st)
	}
}

func TestStoreBrokenFileStartsEmptyAndLogsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observer.json")
	writeTestFile(t, path, `{"version":7}`)

	logs := 0
	o := LoadObserver(path, time.UTC, func(string, ...any) { logs++ })
	if st := o.Snapshot(); len(st.Rules) != 0 {
		t.Fatalf("broken file gave data: %+v", st)
	}
	if logs != 1 {
		t.Fatalf("logged %d times, want 1", logs)
	}
}

func TestObserverSaveWritesWhenDirty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observer.json")
	o := LoadObserver(path, time.UTC, t.Logf)
	o.ObserveTriggers(testBase, map[string]int{"r1": 0})
	o.ObserveTriggers(testBase.Add(time.Minute), map[string]int{"r1": 1})
	// Observing never writes, only Save does.
	if fileExists(path) {
		t.Fatal("file written by an observation")
	}
	if err := o.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !fileExists(path) {
		t.Fatal("file not written by Save")
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
