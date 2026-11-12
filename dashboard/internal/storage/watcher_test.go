package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

var watcherNow = time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)

// calls records the order of the stubbed steps.
type calls struct {
	mu   sync.Mutex
	list []string
}

func (c *calls) add(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list = append(c.list, s)
}

func (c *calls) get() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.list...)
}

// newTestWatcher wires a watcher to the temp paths. The drive check follows
// driveOK, Switch and Reload are recorded, Merge is the real one.
func newTestWatcher(t *testing.T, p Paths, rec *calls, driveOK *atomic.Bool, fallback Fallback) *Watcher {
	t.Helper()
	return &Watcher{
		Config: Config{UseData: true, UUID: "3f2a9c1e-0000-4000-8000-000000000001", Name: "Datenlaufwerk", Fallback: fallback},
		Paths:  p,
		NodeID: "0123456789abcdef0123456789abcdef",
		Now:    func() time.Time { return watcherNow },
		Check:  func(string, string) bool { return driveOK.Load() },
		Switch: func(p Paths, m Mode) error {
			rec.add("switch:" + string(m))
			return Switch(p, m)
		},
		Merge: func(fallbackDir, driveDir string) error {
			rec.add("merge")
			return Merge(fallbackDir, driveDir)
		},
		Reload: func() error {
			rec.add("reload")
			return nil
		},
	}
}

func writeTestFile(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestStartWithoutDriveGoesToFallback(t *testing.T) {
	p := testPaths(t)
	var drive atomic.Bool
	rec := &calls{}
	w := newTestWatcher(t, p, rec, &drive, FallbackSystem)

	st := w.Tick(context.Background())
	if st.Mode != ModeFallbackSystem {
		t.Fatalf("Mode = %q, want %q", st.Mode, ModeFallbackSystem)
	}
	target, err := os.Readlink(p.Runtime)
	if err != nil || target != p.SystemFallback {
		t.Fatalf("runtime -> %q (%v), want %q", target, err, p.SystemFallback)
	}
	if got, want := rec.get(), []string{"switch:fallback_system"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if st.Reason == "" {
		t.Fatal("Reason empty while the drive is missing")
	}
}

func TestDriveLossSwitchesAndRaisesFinding(t *testing.T) {
	p := testPaths(t)
	var drive atomic.Bool
	drive.Store(true)
	rec := &calls{}
	w := newTestWatcher(t, p, rec, &drive, FallbackSystem)

	if st := w.Tick(context.Background()); st.Mode != ModeDrive {
		t.Fatalf("Mode with drive = %q, want %q", st.Mode, ModeDrive)
	}
	if st := w.Status(); st.Reason != "" {
		t.Fatalf("Reason with drive = %q, want empty (no finding)", st.Reason)
	}

	drive.Store(false)
	rec.list = nil
	st := w.Tick(context.Background())
	if st.Mode != ModeFallbackSystem {
		t.Fatalf("Mode after loss = %q, want %q", st.Mode, ModeFallbackSystem)
	}
	if st.Reason == "" {
		t.Fatal("finding not raised after the drive was lost")
	}
	if got, want := rec.get(), []string{"switch:fallback_system", "reload"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v (switch first, then reload from the fallback)", got, want)
	}
}

func TestDriveReturnMergesThenSwitches(t *testing.T) {
	p := testPaths(t)
	var drive atomic.Bool
	rec := &calls{}
	w := newTestWatcher(t, p, rec, &drive, FallbackSystem)
	w.Tick(context.Background())

	// Data written while on the fallback, to be merged and cleared afterwards.
	fb := p.SystemFallback
	writeTestFile(t, filepath.Join(fb, automationHistoryFile), `{"entries":[1]}`)

	drive.Store(true)
	rec.list = nil
	st := w.Tick(context.Background())
	if st.Mode != ModeDrive {
		t.Fatalf("Mode = %q, want %q", st.Mode, ModeDrive)
	}
	want := []string{"reload", "merge", "switch:drive", "reload"}
	if got := rec.get(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if got := readTestFile(t, filepath.Join(DriveRuntimeDir(p), automationHistoryFile)); got != `{"entries":[1]}` {
		t.Fatalf("history not merged to the drive: %q", got)
	}
	if _, err := os.Stat(filepath.Join(fb, automationHistoryFile)); !os.IsNotExist(err) {
		t.Fatalf("fallback folder not cleared after the merge: %v", err)
	}
}

func TestWrongVolumeCountsAsMissing(t *testing.T) {
	p := testPaths(t)
	writeTestFile(t, filepath.Join(p.Mount, markerName), "ffffffffffffffffffffffffffffffff\n")
	rec := &calls{}
	var unused atomic.Bool
	w := newTestWatcher(t, p, rec, &unused, FallbackSystem)
	w.Check = nil // the real marker check

	w.NodeID = "0123456789abcdef0123456789abcdef"
	if st := w.Tick(context.Background()); st.Mode != ModeFallbackSystem {
		t.Fatalf("Mode with a foreign marker = %q, want %q", st.Mode, ModeFallbackSystem)
	}

	writeTestFile(t, filepath.Join(p.Mount, markerName), w.NodeID+"\n")
	if st := w.Tick(context.Background()); st.Mode != ModeDrive {
		t.Fatalf("Mode with the own marker = %q, want %q", st.Mode, ModeDrive)
	}
}

func TestHangingDriveDoesNotBlockTick(t *testing.T) {
	p := testPaths(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	rec := &calls{}
	var unused atomic.Bool
	w := newTestWatcher(t, p, rec, &unused, FallbackSystem)
	w.Check = func(string, string) bool {
		<-release
		return true
	}
	w.CheckTimeout = 50 * time.Millisecond

	start := time.Now()
	st := w.Tick(context.Background())
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Tick blocked for %v on a hanging drive", elapsed)
	}
	if st.Mode != ModeFallbackSystem {
		t.Fatalf("Mode with a hanging drive = %q, want %q", st.Mode, ModeFallbackSystem)
	}
}

func batteryJSON(savedAt float64, fresh bool, value string) string {
	return fmt.Sprintf(`{"saved_at": %g, "fresh_start": %t, "marker": %q}`, savedAt, fresh, value)
}

func TestMergeFreshBatteryStateLoses(t *testing.T) {
	fallback := t.TempDir()
	drive := t.TempDir()
	dstPath := filepath.Join(drive, "battery_soc", "b1.json")
	writeTestFile(t, dstPath, batteryJSON(100, false, "drive"))
	writeTestFile(t, filepath.Join(fallback, "battery_soc", "b1.json"), batteryJSON(200, true, "fallback"))

	if err := Merge(fallback, drive); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if got := readTestFile(t, dstPath); got != batteryJSON(100, false, "drive") {
		t.Fatalf("drive battery state overwritten by a fresh start: %s", got)
	}
}

func TestMergeNewerBatteryStateWins(t *testing.T) {
	fallback := t.TempDir()
	drive := t.TempDir()
	dstPath := filepath.Join(drive, "battery_soc", "b1.json")
	writeTestFile(t, dstPath, batteryJSON(100, false, "drive"))
	srcJSON := batteryJSON(200, false, "fallback")
	writeTestFile(t, filepath.Join(fallback, "battery_soc", "b1.json"), srcJSON)

	if err := Merge(fallback, drive); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if got := readTestFile(t, dstPath); got != srcJSON {
		t.Fatalf("newer battery state not taken: %s", got)
	}
}

func TestMergeNewerHistoryWins(t *testing.T) {
	fallback := t.TempDir()
	drive := t.TempDir()
	dstPath := filepath.Join(drive, automationHistoryFile)
	srcPath := filepath.Join(fallback, automationHistoryFile)
	writeTestFile(t, dstPath, `{"entries":["drive"]}`)
	writeTestFile(t, srcPath, `{"entries":["fallback"]}`)

	older := watcherNow.Add(-time.Hour)
	newer := watcherNow
	if err := os.Chtimes(dstPath, newer, newer); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(srcPath, older, older); err != nil {
		t.Fatal(err)
	}
	if err := Merge(fallback, drive); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if got := readTestFile(t, dstPath); got != `{"entries":["drive"]}` {
		t.Fatalf("older history replaced the newer one: %s", got)
	}

	if err := os.Chtimes(srcPath, newer.Add(time.Hour), newer.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := Merge(fallback, drive); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if got := readTestFile(t, dstPath); got != `{"entries":["fallback"]}` {
		t.Fatalf("newer history not taken: %s", got)
	}
}

func TestMergeCopiesEnergyMonths(t *testing.T) {
	fallback := t.TempDir()
	drive := t.TempDir()
	m := newMonthForTest(t, "2026-11-01T10:00:00Z", 1.5)
	writeTestFile(t, filepath.Join(fallback, "energystats", "2026-11.json"), m)

	if err := Merge(fallback, drive); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if got := readTestFile(t, filepath.Join(drive, "energystats", "2026-11.json")); got != m {
		t.Fatal("energy month not copied to the drive")
	}
}

func TestMergeFailureStaysOnFallback(t *testing.T) {
	p := testPaths(t)
	var drive atomic.Bool
	rec := &calls{}
	w := newTestWatcher(t, p, rec, &drive, FallbackSystem)
	w.Tick(context.Background())

	// A history file that cannot be read as a file blocks the merge.
	if err := os.Mkdir(filepath.Join(p.SystemFallback, automationHistoryFile), 0755); err != nil {
		t.Fatal(err)
	}
	drive.Store(true)
	st := w.Tick(context.Background())
	if st.Mode != ModeFallbackSystem {
		t.Fatalf("Mode after a failed merge = %q, want %q", st.Mode, ModeFallbackSystem)
	}
	if st.Reason == "" {
		t.Fatal("failed merge not reported in Reason")
	}
	for _, c := range rec.get() {
		if c == "switch:drive" {
			t.Fatal("switched to the drive although the merge failed")
		}
	}
}

func TestRAMFullKeepsRunningAndWarns(t *testing.T) {
	p := testPaths(t)
	var drive atomic.Bool
	drive.Store(true)
	rec := &calls{}
	w := newTestWatcher(t, p, rec, &drive, FallbackRAM)
	w.Tick(context.Background())

	// Losing the drive writes into RAM, which is full: the reload fails.
	w.Reload = func() error { return syscall.ENOSPC }
	drive.Store(false)
	done := make(chan Status, 1)
	go func() { done <- w.Tick(context.Background()) }()
	var st Status
	select {
	case st = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Tick did not return when the RAM write failed")
	}
	if st.Mode != ModeFallbackRAM {
		t.Fatalf("Mode = %q, want %q", st.Mode, ModeFallbackRAM)
	}
	if !strings.Contains(st.Reason, "no space") {
		t.Fatalf("Reason = %q, want the write error", st.Reason)
	}

	// The watcher keeps running: the next tick still answers.
	if st := w.Tick(context.Background()); st.Mode != ModeFallbackRAM {
		t.Fatalf("second Tick Mode = %q, want %q", st.Mode, ModeFallbackRAM)
	}
}

// newMonthForTest returns the file content of a month with one own hour.
func newMonthForTest(t *testing.T, hour string, kwh float64) string {
	t.Helper()
	at, err := time.Parse(time.RFC3339, hour)
	if err != nil {
		t.Fatal(err)
	}
	m := energystats.NewMonth(at)
	if !m.AddOwn(at, map[string]float64{"pv": kwh}, 3600) {
		t.Fatalf("AddOwn %s", hour)
	}
	data, err := m.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
