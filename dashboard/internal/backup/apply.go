package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

const (
	pendingDirName  = "backup-pending"
	pendingFile     = "pending.json"
	journalFile     = "journal.json"
	oldDirName      = "old"
	stagingDirName  = "staging"
	resultFile      = "backup-result.json"
	energystatsName = "energystats"
	usersLiveFile   = "users.json"
)

// Pending beschreibt einen Import, der beim nächsten Start eingespielt wird.
type Pending struct {
	Areas          map[Area]bool `json:"areas"`
	ArchiveSecrets bool          `json:"archive_secrets"`
	PreArchive     string        `json:"pre_archive,omitempty"`
}

// Result hält das Ergebnis des letzten Einspielens. Code ist leer bei Erfolg.
type Result struct {
	OK            bool      `json:"ok"`
	Code          string    `json:"code,omitempty"`
	Applied       []Area    `json:"applied,omitempty"`
	DisabledUsers []string  `json:"disabled_users,omitempty"`
	FinishedAt    time.Time `json:"finished_at"`
}

// journalEntry hält ein Ziel, bevor es verändert wird. Old ist der Name der
// Sicherungskopie unter old/. Dir markiert das Stats-Verzeichnis.
type journalEntry struct {
	Target  string `json:"target"`
	Existed bool   `json:"existed"`
	Old     string `json:"old,omitempty"`
	Dir     bool   `json:"dir,omitempty"`
}

// journal listet alle Ziele eines Einspielens. Done ist erst gesetzt, wenn alle geschrieben sind.
type journal struct {
	Done    bool           `json:"done"`
	Entries []journalEntry `json:"entries"`
}

// fileWrite ist eine Datei, die das Einspielen ersetzt.
type fileWrite struct {
	target string
	data   []byte
}

// applyPlan sammelt alles, was ein Einspielen tut, bevor es die erste Datei anfasst.
type applyPlan struct {
	writes   []fileWrite
	statsDst string
	statsSrc string
	disabled []string
}

// WritePending legt das Staging unter dataDir/backup-pending/staging ab und
// schreibt zuletzt pending.json. Ohne pending.json gilt der Import nicht.
func WritePending(dataDir, stagingDir string, p Pending) error {
	pendingDir := filepath.Join(dataDir, pendingDirName)
	if err := os.MkdirAll(pendingDir, 0o700); err != nil {
		return err
	}
	target := filepath.Join(pendingDir, stagingDirName)
	if filepath.Clean(stagingDir) != filepath.Clean(target) {
		if err := moveDir(stagingDir, target); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return config.AtomicWrite(filepath.Join(pendingDir, pendingFile), data, 0o600)
}

// moveDir verschiebt from nach to. Liegt das Ziel auf einem anderen Dateisystem, kopiert es.
func moveDir(from, to string) error {
	if err := os.RemoveAll(to); err != nil {
		return err
	}
	err := os.Rename(from, to)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := copyTree(from, to); err != nil {
		return err
	}
	return os.RemoveAll(from)
}

// ApplyPending spielt einen vorgemerkten Import ein. Ohne Import und ohne
// unfertiges Journal ist das (nil, nil). Ein unfertiges Journal rollt zuerst
// zurück, das Ergebnis lautet dann "interrupted".
func ApplyPending(src Sources) (*Result, error) {
	pendingDir := filepath.Join(src.DataDir, pendingDirName)
	j, err := readJournal(pendingDir)
	if err != nil {
		return nil, err
	}
	if j != nil && !j.Done {
		return recoverInterrupted(src.DataDir, pendingDir, *j)
	}
	if j != nil {
		// Der Import ist schon abgeschlossen, nur die Aufräumarbeit fehlte.
		if err := os.RemoveAll(pendingDir); err != nil {
			return nil, err
		}
		return nil, nil
	}
	p, err := readPending(pendingDir)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, nil
	}
	return runApply(src, pendingDir, *p)
}

// ReadResult liest backup-result.json. Fehlt die Datei, ist das (nil, nil).
func ReadResult(dataDir string) (*Result, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, resultFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Result
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// recoverInterrupted rollt ein unfertiges Einspielen zurück.
func recoverInterrupted(dataDir, pendingDir string, j journal) (*Result, error) {
	if err := rollback(pendingDir, j); err != nil {
		return nil, fmt.Errorf("Zurückrollen nach unterbrochener Wiederherstellung: %w", err)
	}
	res := &Result{OK: false, Code: "interrupted", FinishedAt: time.Now().UTC()}
	if err := writeResult(dataDir, res); err != nil {
		return res, err
	}
	return res, os.RemoveAll(pendingDir)
}

// runApply plant, sichert, schreibt und schließt das Einspielen ab.
func runApply(src Sources, pendingDir string, p Pending) (*Result, error) {
	pl, err := buildPlan(src, filepath.Join(pendingDir, stagingDirName), p)
	if err != nil {
		return applyFailed(src.DataDir, pendingDir, journal{}, err)
	}
	j, err := snapshot(pendingDir, pl)
	if err != nil {
		return applyFailed(src.DataDir, pendingDir, j, err)
	}
	if err := writeJournal(pendingDir, j); err != nil {
		return applyFailed(src.DataDir, pendingDir, j, err)
	}
	partial, err := execute(pl)
	if err != nil {
		return applyFailed(src.DataDir, pendingDir, j, err)
	}

	j.Done = true
	if err := writeJournal(pendingDir, j); err != nil {
		return applyFailed(src.DataDir, pendingDir, journal{Entries: j.Entries}, err)
	}
	if err := os.Remove(filepath.Join(pendingDir, pendingFile)); err != nil {
		return nil, err
	}
	res := &Result{
		OK:            true,
		Applied:       ChosenAreas(p.Areas),
		DisabledUsers: pl.disabled,
		FinishedAt:    time.Now().UTC(),
	}
	if partial {
		res.Code = "partial_stats"
	}
	if err := writeResult(src.DataDir, res); err != nil {
		return res, err
	}
	return res, os.RemoveAll(pendingDir)
}

// applyFailed rollt zurück, schreibt das Fehlerergebnis und räumt auf. Gelingt
// das Zurückrollen nicht, bleibt das Journal stehen, damit der nächste Start es erneut versucht.
func applyFailed(dataDir, pendingDir string, j journal, cause error) (*Result, error) {
	if err := rollback(pendingDir, j); err != nil {
		return nil, errors.Join(cause, fmt.Errorf("Zurückrollen: %w", err))
	}
	res := &Result{OK: false, Code: "apply_failed", FinishedAt: time.Now().UTC()}
	if err := writeResult(dataDir, res); err != nil {
		return res, errors.Join(cause, err)
	}
	if err := os.RemoveAll(pendingDir); err != nil {
		return res, errors.Join(cause, err)
	}
	return res, cause
}

// buildPlan liest das Staging und die Live-Dateien und legt die Schreibvorgänge fest.
func buildPlan(src Sources, staging string, p Pending) (applyPlan, error) {
	var pl applyPlan
	staged, err := stagedFiles(staging)
	if err != nil {
		return pl, err
	}
	names := make([]string, 0, len(staged))
	for name := range staged {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		area, ok := areaOf(name)
		if !ok || !p.Areas[area] {
			continue
		}
		f := staged[name]
		data, err := os.ReadFile(f.path)
		if err != nil {
			return pl, err
		}
		switch {
		case name == usersArchiveName:
			target, err := safeJoin(src.DataDir, usersLiveFile)
			if err != nil {
				return pl, err
			}
			merged, disabled, err := mergeUsersFile(target, data, p.ArchiveSecrets)
			if err != nil {
				return pl, err
			}
			pl.disabled = disabled
			pl.writes = append(pl.writes, fileWrite{target: target, data: merged})
		case area == AreaBattery:
			id := strings.TrimSuffix(strings.TrimPrefix(name, "battery/"), ".json")
			state, ok := src.BatteryStates[id]
			if !ok {
				continue
			}
			pl.writes = append(pl.writes, fileWrite{target: state, data: data})
		case area == AreaStats:
			// Monatsdateien laufen über MergeDir, siehe unten.
			continue
		case strings.HasPrefix(name, "data/"):
			target, err := safeJoin(src.DataDir, strings.TrimPrefix(name, "data/"))
			if err != nil {
				return pl, err
			}
			pl.writes = append(pl.writes, fileWrite{target: target, data: data})
		case strings.HasPrefix(name, "devices/"):
			if src.DevicesDir == "" {
				return pl, errors.New("Geräteverzeichnis fehlt")
			}
			target, err := safeJoin(src.DevicesDir, strings.TrimPrefix(name, "devices/"))
			if err != nil {
				return pl, err
			}
			pl.writes = append(pl.writes, fileWrite{target: target, data: data})
		}
	}

	if p.Areas[AreaStats] {
		srcDir := filepath.Join(staging, "data", energystatsName)
		if info, err := os.Stat(srcDir); err == nil && info.IsDir() {
			pl.statsSrc = srcDir
			pl.statsDst = filepath.Join(src.DataDir, energystatsName)
		}
	}
	return pl, nil
}

// mergeUsersFile führt die Benutzer aus dem Archiv mit dem Live-Stand zusammen.
// Fehlt der Live-Stand, gilt das Archiv unverändert.
func mergeUsersFile(liveTarget string, archived []byte, archiveSecrets bool) ([]byte, []string, error) {
	current, err := os.ReadFile(liveTarget)
	if errors.Is(err, fs.ErrNotExist) {
		return archived, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return MergeUsers(current, archived, archiveSecrets)
}

// snapshot sichert alle Ziele des Plans unter old/ und liefert das Journal dazu.
// Ein Eintrag entsteht erst, wenn seine Sicherung steht.
func snapshot(pendingDir string, pl applyPlan) (journal, error) {
	var j journal
	oldDir := filepath.Join(pendingDir, oldDirName)
	if err := os.MkdirAll(oldDir, 0o700); err != nil {
		return j, err
	}
	for i, w := range pl.writes {
		e := journalEntry{Target: w.target, Old: fmt.Sprintf("file-%d", i)}
		data, err := os.ReadFile(w.target)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return j, err
		default:
			e.Existed = true
			if err := os.WriteFile(filepath.Join(oldDir, e.Old), data, 0o600); err != nil {
				return j, err
			}
		}
		j.Entries = append(j.Entries, e)
	}
	if pl.statsSrc != "" {
		e := journalEntry{Target: pl.statsDst, Dir: true, Old: energystatsName}
		info, err := os.Stat(pl.statsDst)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return j, err
		case !info.IsDir():
			return j, fmt.Errorf("%s ist kein Verzeichnis", pl.statsDst)
		default:
			e.Existed = true
			if err := copyTree(pl.statsDst, filepath.Join(oldDir, energystatsName)); err != nil {
				return j, err
			}
		}
		j.Entries = append(j.Entries, e)
	}
	return j, nil
}

// execute schreibt die Dateien und führt die Monatsdaten zusammen. partial meldet
// Monatsdateien des Archivs, die nicht lesbar waren. Das ist kein Fehler.
func execute(pl applyPlan) (partial bool, err error) {
	for _, w := range pl.writes {
		if err := config.AtomicWrite(w.target, w.data, 0o600); err != nil {
			return false, err
		}
	}
	if pl.statsSrc == "" {
		return false, nil
	}
	if err := os.MkdirAll(pl.statsDst, 0o750); err != nil {
		return false, err
	}
	err = energystats.MergeDir(pl.statsDst, pl.statsSrc)
	var skipped *energystats.MergeSkipped
	if errors.As(err, &skipped) {
		return true, nil
	}
	return false, err
}

// writeJournal schreibt das Journal als letzten Schritt vor dem ersten Ziel.
func writeJournal(pendingDir string, j journal) error {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return config.AtomicWrite(filepath.Join(pendingDir, journalFile), data, 0o600)
}

// readJournal liest das Journal. Fehlt es, ist das (nil, nil).
func readJournal(pendingDir string) (*journal, error) {
	data, err := os.ReadFile(filepath.Join(pendingDir, journalFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j journal
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("Journal des Wiederherstellens: %w", err)
	}
	return &j, nil
}

// readPending liest pending.json. Fehlt es, ist das (nil, nil).
func readPending(pendingDir string) (*Pending, error) {
	data, err := os.ReadFile(filepath.Join(pendingDir, pendingFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p Pending
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("pending.json: %w", err)
	}
	return &p, nil
}

// writeResult schreibt backup-result.json.
func writeResult(dataDir string, r *Result) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return config.AtomicWrite(filepath.Join(dataDir, resultFile), data, 0o600)
}

// rollback stellt die Ziele des Journals in umgekehrter Reihenfolge wieder her.
// Ziele ohne Vorgänger verschwinden, das Stats-Verzeichnis kommt aus seiner Kopie.
func rollback(pendingDir string, j journal) error {
	var errs []error
	for i := len(j.Entries) - 1; i >= 0; i-- {
		if err := restoreEntry(pendingDir, j.Entries[i]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func restoreEntry(pendingDir string, e journalEntry) error {
	oldPath := filepath.Join(pendingDir, oldDirName, e.Old)
	if e.Dir {
		if err := os.RemoveAll(e.Target); err != nil {
			return err
		}
		if !e.Existed {
			return nil
		}
		return copyTree(oldPath, e.Target)
	}
	if !e.Existed {
		err := os.Remove(e.Target)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	data, err := os.ReadFile(oldPath)
	if err != nil {
		return err
	}
	return config.AtomicWrite(e.Target, data, 0o600)
}

// copyTree kopiert den Verzeichnisbaum from nach to mit Modus 0600 für Dateien.
func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o600)
	})
}
