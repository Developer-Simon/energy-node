package learnrec

import (
	"encoding/binary"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// msTime ist eine Minute am Testtag, in UTC.
func msTime(h, m int) time.Time {
	return time.Date(2026, 11, 21, h, m, 0, 0, time.UTC)
}

// msEnv ist die veraenderbare Umgebung eines Tests: Einstellungen, Laufwerk und Stufe.
type msEnv struct {
	plan   MinutePlan
	dirs   MinuteDirs
	up     bool
	limits Limits
}

func newMsEnv(t *testing.T, loc MinuteLocation) *msEnv {
	t.Helper()
	return &msEnv{
		plan:   MinutePlan{On: true, Location: loc, Days: 60},
		dirs:   MinuteDirs{Drive: t.TempDir(), System: t.TempDir()},
		up:     true,
		limits: Limits{RAMMinuteDays: 7, RAMMinuteBytes: 16 << 20},
	}
}

func newMsStore(env *msEnv) *MinuteStore {
	return NewMinuteStore(time.UTC,
		func() MinutePlan { return env.plan },
		func() MinuteDirs { return env.dirs },
		func() bool { return env.up },
		func() Limits { return env.limits },
		func(string, ...any) {},
	)
}

// msFrame ist ein Minutenrahmen mit einem Geraet, einem Wert und einer Bilanz.
func msFrame(at time.Time, watts float64) Frame {
	return Frame{
		Minute:  at,
		Devices: []DeviceMinute{{ID: "dev", Power: map[string]float64{"uid1": watts}}},
		Balance: map[string]float64{"grid": 42},
	}
}

func msPath(base, day string) string {
	return filepath.Join(base, "learnrec", "minutes", day+".enm")
}

// msBlockMinutes zaehlt alle Minuten in den Wertbloecken der Datei. Doppelte
// Minuten wuerden hier auffallen.
func msBlockMinutes(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pos, err := checkMinuteHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for pos < len(data) {
		body, next, ok := nextMinuteFrame(data, pos)
		if !ok {
			t.Fatalf("invalid block at offset %d", pos)
		}
		if body[0] == minuteValues {
			total += int(binary.LittleEndian.Uint16(body[3:5]))
		}
		pos = next
	}
	return total
}

func msCountFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMinuteStoreFlushWritesRAMMinutesToSystem(t *testing.T) {
	env := newMsEnv(t, LocSystem)
	s := newMsStore(env)
	for i := 0; i < 10; i++ {
		s.ObserveMinute(msFrame(msTime(10, i), float64(i)))
	}
	if err := s.Flush(msTime(10, 10)); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	path := msPath(env.dirs.System, testMinuteDay)
	if got := msBlockMinutes(t, path); got != 10 {
		t.Fatalf("minutes in blocks = %d, want 10", got)
	}
	_, values := readMinuteFile(t, path)
	for i := 0; i < 10; i++ {
		requireMinuteValue(t, values, "e:uid1", 600+i, float32(i))
		requireMinuteValue(t, values, "b:grid", 600+i, 42)
	}
}

func TestMinuteStoreRAMLocationWritesNoFile(t *testing.T) {
	env := newMsEnv(t, LocRAM)
	s := newMsStore(env)
	for i := 0; i < 10; i++ {
		s.ObserveMinute(msFrame(msTime(10, i), float64(i)))
	}
	if err := s.Flush(msTime(10, 10)); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if n := msCountFiles(t, env.dirs.System) + msCountFiles(t, env.dirs.Drive); n != 0 {
		t.Fatalf("files written on ram location: %d", n)
	}
	values, ok := s.Day(testMinuteDay)
	if !ok {
		t.Fatal("Day: no RAM day")
	}
	requireMinuteValue(t, values, "e:uid1", 609, 9)
}

func TestMinuteStoreDriveOutageBackfillsWithoutGapOrDuplicate(t *testing.T) {
	env := newMsEnv(t, LocDrive)
	env.up = false
	s := newMsStore(env)
	const minutes = 180 // drei Stunden
	for i := 0; i < minutes; i++ {
		at := msTime(0, 0).Add(time.Duration(i) * time.Minute)
		s.ObserveMinute(msFrame(at, float64(i)))
		if i%5 == 4 {
			if err := s.Flush(at); err != nil {
				t.Fatalf("Flush during outage: %v", err)
			}
		}
	}
	path := msPath(env.dirs.Drive, testMinuteDay)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file written while drive is down: %v", err)
	}
	if !s.Usage().DriveDown {
		t.Fatal("Usage().DriveDown = false during outage")
	}

	env.up = true
	if err := s.Flush(msTime(3, 0)); err != nil {
		t.Fatalf("Flush after return: %v", err)
	}
	if got := msBlockMinutes(t, path); got != minutes {
		t.Fatalf("minutes in blocks = %d, want %d (no gap, no duplicate)", got, minutes)
	}
	_, values := readMinuteFile(t, path)
	for i := 0; i < minutes; i++ {
		requireMinuteValue(t, values, "e:uid1", i, float32(i))
	}
	if s.Usage().DriveDown {
		t.Fatal("Usage().DriveDown = true after return")
	}
}

func TestMinuteStoreLimitsShrinkRAMDays(t *testing.T) {
	env := newMsEnv(t, LocSystem)
	env.limits = Limits{RAMMinuteDays: 14, RAMMinuteBytes: 64 << 20}
	s := newMsStore(env)
	for d := 0; d < 6; d++ {
		at := time.Date(2026, 11, 10+d, 12, 0, 0, 0, time.UTC)
		s.ObserveMinute(msFrame(at, 1))
		if err := s.Flush(at); err != nil {
			t.Fatalf("Flush: %v", err)
		}
	}
	if got := s.Usage().RAMDays; got != 6 {
		t.Fatalf("RAMDays on full = %d, want 6", got)
	}

	env.limits = Limits{RAMMinuteDays: 3, RAMMinuteBytes: 4 << 20}
	if err := s.Flush(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := s.Usage().RAMDays; got != 3 {
		t.Fatalf("RAMDays on eco = %d, want 3", got)
	}
	if got := msCountFiles(t, filepath.Join(env.dirs.System, "learnrec", "minutes")); got != 6 {
		t.Fatalf("files after shrink = %d, want 6", got)
	}
}

func TestMinuteStoreDaysPrunesOldFiles(t *testing.T) {
	env := newMsEnv(t, LocSystem)
	env.plan.Days = 2
	s := newMsStore(env)

	old := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s.ObserveMinute(msFrame(old, 1))
	if err := s.Flush(old); err != nil {
		t.Fatalf("Flush old: %v", err)
	}
	if _, err := os.Stat(msPath(env.dirs.System, "2026-11-18")); err != nil {
		t.Fatalf("old file not written: %v", err)
	}

	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	s.ObserveMinute(msFrame(now, 1))
	if err := s.Flush(now); err != nil {
		t.Fatalf("Flush now: %v", err)
	}
	if _, err := os.Stat(msPath(env.dirs.System, "2026-11-18")); !os.IsNotExist(err) {
		t.Fatalf("file from three days ago still there: %v", err)
	}
	if _, err := os.Stat(msPath(env.dirs.System, "2026-11-21")); err != nil {
		t.Fatalf("current file missing: %v", err)
	}
}

func TestMinuteStoreResetClearsRAMAndFiles(t *testing.T) {
	env := newMsEnv(t, LocSystem)
	s := newMsStore(env)
	for i := 0; i < 10; i++ {
		s.ObserveMinute(msFrame(msTime(10, i), float64(i)))
	}
	if err := s.Flush(msTime(10, 10)); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Ortswechsel auf ram: das System-Verzeichnis ist jetzt der vorige Ort.
	env.plan.Location = LocRAM
	s.ObserveMinute(msFrame(msTime(10, 10), 1))
	if err := s.Flush(msTime(10, 11)); err != nil {
		t.Fatalf("Flush ram: %v", err)
	}

	if err := s.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if got := s.Usage().Bytes; got != 0 {
		t.Fatalf("Usage().Bytes after Reset = %d, want 0", got)
	}
	if n := msCountFiles(t, filepath.Join(env.dirs.System, "learnrec", "minutes")); n != 0 {
		t.Fatalf("files left after Reset: %d", n)
	}
	if _, ok := s.Day(testMinuteDay); ok {
		t.Fatal("Day still returns data after Reset")
	}
}

func TestMinuteStoreWrittenPerDayMatchesBytesOverDays(t *testing.T) {
	env := newMsEnv(t, LocSystem)
	s := newMsStore(env)
	start := msTime(12, 0)
	for i := 0; i < 10; i++ {
		s.ObserveMinute(msFrame(start.Add(time.Duration(i)*time.Minute), float64(i)))
	}
	end := start.Add(2 * 24 * time.Hour)
	if err := s.Flush(end); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	u := s.Usage()
	if u.Bytes <= 0 {
		t.Fatalf("Usage().Bytes = %d, want > 0", u.Bytes)
	}
	if want := int64(float64(u.Bytes) / 2); u.WrittenPerDay != want {
		t.Fatalf("WrittenPerDay = %d, want %d (Bytes over 2 days)", u.WrittenPerDay, want)
	}
	if u.Since != start.Unix() {
		t.Fatalf("Since = %d, want %d", u.Since, start.Unix())
	}
}

func TestMinuteStorePausedRecordsNothingUntilResumed(t *testing.T) {
	env := newMsEnv(t, LocSystem)
	s := newMsStore(env)
	env.plan.Paused = true
	s.ObserveMinute(msFrame(msTime(10, 0), 1))
	s.ObserveMinute(msFrame(msTime(10, 1), 2))
	env.plan.Paused = false
	s.ObserveMinute(msFrame(msTime(10, 2), 3))
	if err := s.Flush(msTime(10, 3)); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	path := msPath(env.dirs.System, testMinuteDay)
	if got := msBlockMinutes(t, path); got != 1 {
		t.Fatalf("minutes in blocks = %d, want 1 (only the minute after the pause)", got)
	}
	_, values := readMinuteFile(t, path)
	requireMinuteValue(t, values, "e:uid1", 600+2, 3)
}
