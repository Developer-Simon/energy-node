package backup

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeBytes(data string) func(io.Writer) error {
	return func(w io.Writer) error {
		_, err := io.WriteString(w, data)
		return err
	}
}

func at(hour int) time.Time {
	return time.Date(2026, 11, 8, hour, 0, 0, 0, time.UTC)
}

func TestDefaultsAndRoundTrip(t *testing.T) {
	dir := t.TempDir()

	d := DefaultSettings()
	if !d.BeforeUpdate || d.Schedule || d.Every != "week" || d.Keep != 5 || !d.SystemFallback || d.Secrets {
		t.Fatalf("DefaultSettings = %+v", d)
	}
	wantContent := map[Area]bool{
		AreaSettings: true, AreaDevices: true, AreaUsers: true, AreaStats: true, AreaHistory: true,
		AreaSecrets: false, AreaBattery: false,
	}
	if !reflect.DeepEqual(d.Content, wantContent) {
		t.Fatalf("DefaultSettings().Content = %v, want %v", d.Content, wantContent)
	}

	got, err := LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings ohne Datei: %v", err)
	}
	if !reflect.DeepEqual(got, d) {
		t.Fatalf("LoadSettings ohne Datei = %+v, want defaults", got)
	}

	changed := DefaultSettings()
	changed.Schedule = true
	changed.Every = "day"
	changed.Keep = 9
	changed.Secrets = true
	changed.Content[AreaBattery] = true
	if err := SaveSettings(dir, changed); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	got, err = LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings nach Speichern: %v", err)
	}
	if !reflect.DeepEqual(got, changed) {
		t.Fatalf("Round Trip = %+v, want %+v", got, changed)
	}

	// Fehlende Felder behalten Defaults, ungueltige Werte fallen auf Defaults zurueck.
	partial := filepath.Join(dir, "backup.json")
	if err := os.WriteFile(partial, []byte(`{"keep": 3, "every": "monthly"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings partiell: %v", err)
	}
	if got.Keep != 3 || got.Every != "week" || !got.BeforeUpdate || !reflect.DeepEqual(got.Content, wantContent) {
		t.Fatalf("LoadSettings partiell = %+v", got)
	}
	if err := os.WriteFile(partial, []byte(`{"keep": 0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings keep 0: %v", err)
	}
	if got.Keep != 5 {
		t.Fatalf("Keep 0 wurde zu %d, want 5", got.Keep)
	}

	// Ohne Systemlaufwerk gibt es keinen Ort.
	off := SystemLocation{DataDir: dir, Settings: func() Settings { return Settings{SystemFallback: false} }}
	if _, _, err := off.ArchiveDir(); !errors.Is(err, ErrNoLocation) {
		t.Fatalf("ArchiveDir ohne Fallback: err = %v, want ErrNoLocation", err)
	}
	on := SystemLocation{DataDir: dir, Settings: func() Settings { return DefaultSettings() }}
	archDir, place, err := on.ArchiveDir()
	if err != nil || place != "system" || archDir != filepath.Join(dir, "backups") {
		t.Fatalf("ArchiveDir = %q, %q, %v", archDir, place, err)
	}
}

func TestArchiveFilesAreMode0600(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, locked := range []bool{false, true} {
		info, err := store.Add(Info{Trigger: "update", CreatedAt: at(10), Locked: locked}, 0, writeBytes("daten"))
		if err != nil {
			t.Fatalf("Add(locked=%v): %v", locked, err)
		}
		files := []string{info.ID + ".tar.gz", info.ID + ".json"}
		if locked {
			files[0] = info.ID + ".tar.gz.enc"
		}
		for _, name := range files {
			fi, err := os.Stat(filepath.Join(store.Dir, name))
			if err != nil {
				t.Fatalf("Stat %s: %v", name, err)
			}
			if fi.Mode().Perm() != 0o600 {
				t.Errorf("%s hat Modus %o, want 600", name, fi.Mode().Perm())
			}
		}
	}
}

func TestListReadsSidecarOfLockedArchive(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	added, err := store.Add(Info{Trigger: "manual", CreatedAt: at(12), Locked: true, Secrets: true,
		Areas: []Area{AreaSettings, AreaSecrets}}, 0, writeBytes("geheim"))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Archiv ohne Sidecar wird ignoriert.
	if err := os.WriteFile(filepath.Join(store.Dir, "20260101-000000-manual.tar.gz"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List = %d Eintraege, want 1", len(list))
	}
	got := list[0]
	if got.ID != added.ID || !got.Locked || !got.Secrets || got.Trigger != "manual" || got.Size != int64(len("geheim")) {
		t.Fatalf("List[0] = %+v", got)
	}
	if !reflect.DeepEqual(got.Areas, []Area{AreaSettings, AreaSecrets}) {
		t.Fatalf("Areas = %v", got.Areas)
	}
}

func TestPruneKeepsManualAndNewest(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add(Info{Trigger: "manual", CreatedAt: at(1)}, 0, writeBytes("m")); err != nil {
		t.Fatal(err)
	}
	var lastAuto string
	for h := 2; h <= 5; h++ {
		info, err := store.Add(Info{Trigger: "update", CreatedAt: at(h)}, 2, writeBytes("a"))
		if err != nil {
			t.Fatalf("Add update %d: %v", h, err)
		}
		lastAuto = info.ID
	}
	if err := store.Prune("manual", 1); err != nil {
		t.Fatalf("Prune manual: %v", err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	var triggers []string
	for _, info := range list {
		triggers = append(triggers, info.Trigger)
	}
	if len(list) != 3 || strings.Count(strings.Join(triggers, ","), "manual") != 1 || list[0].ID != lastAuto {
		t.Fatalf("nach Prune: %v, want 1 manual und 2 update, neuester %s", triggers, lastAuto)
	}
}

func TestFailedWriteLeavesNoArchiveAndKeepsOldOnes(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []int{1, 2} {
		if _, err := store.Add(Info{Trigger: "update", CreatedAt: at(h)}, 0, writeBytes("alt")); err != nil {
			t.Fatal(err)
		}
	}
	boom := errors.New("platte voll")
	_, err = store.Add(Info{Trigger: "update", CreatedAt: at(3)}, 1, func(w io.Writer) error {
		_, _ = io.WriteString(w, "teil")
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Add err = %v, want %v", err, boom)
	}

	entries, err := os.ReadDir(store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") || strings.HasPrefix(e.Name(), "20261108-030000") {
			t.Errorf("Rest nach fehlgeschlagenem Schreiben: %s", e.Name())
		}
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("List = %d Eintraege, want 2 alte (kein Prune bei Fehler)", len(list))
	}
}

func TestStaleTmpRemovedOnOpen(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "20260101-000000-update.tar.gz.tmp")
	if err := os.WriteFile(stale, []byte("kaputt"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(dir); err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale tmp existiert noch: err = %v", err)
	}
}

// TestFreshTmpSurvivesOpen prüft, dass ein frisch geschriebenes tmp-Archiv
// (ein laufender Schreibvorgang anderer Aufrufer) beim Öffnen des Speichers bleibt.
func TestFreshTmpSurvivesOpen(t *testing.T) {
	dir := t.TempDir()
	fresh := filepath.Join(dir, "20260101-000000-update.tar.gz.tmp")
	if err := os.WriteFile(fresh, []byte("in Arbeit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(dir); err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("frisches tmp wurde entfernt: %v", err)
	}
}

func TestDeleteRejectsPathsOutsideDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "backups")
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "x")
	if err := os.WriteFile(outside, []byte("nicht anfassen"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("../x"); err == nil {
		t.Fatal("Delete(\"../x\") ohne Fehler")
	}
	if _, _, err := store.Open("../x"); err == nil {
		t.Fatal("Open(\"../x\") ohne Fehler")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("Datei ausserhalb wurde angefasst: %v", err)
	}
}
