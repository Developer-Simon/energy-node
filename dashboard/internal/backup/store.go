package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
)

const (
	archiveSuffix = ".tar.gz"
	lockedSuffix  = ".tar.gz.enc"
	sidecarSuffix = ".json"
	tmpSuffix     = ".tmp"
	timeLayout    = "20060102-150405"
)

var (
	idPattern      = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[a-z_]+$`)
	triggerPattern = regexp.MustCompile(`^[a-z_]+$`)
)

// Info beschreibt ein Archiv. Die Angaben stehen im Sidecar neben dem Archiv.
type Info struct {
	ID        string    `json:"id"`
	Trigger   string    `json:"trigger"`
	Version   string    `json:"version"`
	Place     string    `json:"place"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	Areas     []Area    `json:"areas"`
	Secrets   bool      `json:"secrets"`
	Locked    bool      `json:"locked"`
}

// Store verwaltet die Archive in einem Verzeichnis.
type Store struct {
	Dir string
}

// staleTmpAge ist das Alter, ab dem ein tmp-Archiv als Rest eines abgebrochenen Schreibvorgangs gilt.
const staleTmpAge = time.Hour

// NewStore legt das Verzeichnis an und entfernt alte Reste abgebrochener Schreibvorgaenge.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), tmpSuffix) {
			continue
		}
		// Ein frisches tmp-Archiv kann ein laufender Schreibvorgang sein, den ein
		// anderer Aufrufer des Speichers gerade anlegt. Nur alte Reste fallen weg.
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if time.Since(info.ModTime()) < staleTmpAge {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return &Store{Dir: dir}, nil
}

// Add schreibt ein neues Archiv und danach den Sidecar. Bleibt keep > 0,
// werden aeltere Archive desselben Auslosers entfernt.
func (s *Store) Add(meta Info, keep int, write func(io.Writer) error) (Info, error) {
	if !triggerPattern.MatchString(meta.Trigger) {
		return Info{}, fmt.Errorf("backup: ungueltiger Ausloeser %q", meta.Trigger)
	}
	created := meta.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	created = created.UTC().Truncate(time.Second)
	id := created.Format(timeLayout) + "-" + meta.Trigger
	for s.exists(id) {
		created = created.Add(time.Second)
		id = created.Format(timeLayout) + "-" + meta.Trigger
	}

	name := id + archiveSuffix
	if meta.Locked {
		name = id + lockedSuffix
	}
	tmp := filepath.Join(s.Dir, name+tmpSuffix)
	size, err := writeTemp(tmp, write)
	if err != nil {
		_ = os.Remove(tmp)
		return Info{}, err
	}
	if err := os.Rename(tmp, filepath.Join(s.Dir, name)); err != nil {
		_ = os.Remove(tmp)
		return Info{}, err
	}

	meta.ID = id
	meta.CreatedAt = created
	meta.Size = size
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return Info{}, err
	}
	if err := config.AtomicWrite(filepath.Join(s.Dir, id+sidecarSuffix), append(raw, '\n'), 0o600); err != nil {
		return Info{}, err
	}
	if keep > 0 {
		if err := s.Prune(meta.Trigger, keep); err != nil {
			return meta, err
		}
	}
	return meta, nil
}

// List liefert alle Archive mit Sidecar, neuestes zuerst.
func (s *Store) List() ([]Info, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		id, ok := archiveID(e.Name())
		if !ok {
			continue
		}
		info, err := s.readSidecar(id)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// Delete entfernt Archiv und Sidecar einer ID.
func (s *Store) Delete(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("backup: ungueltige ID %q", id)
	}
	removed := false
	for _, name := range []string{id + archiveSuffix, id + lockedSuffix, id + sidecarSuffix} {
		err := os.Remove(filepath.Join(s.Dir, name))
		switch {
		case err == nil:
			removed = true
		case errors.Is(err, fs.ErrNotExist):
		default:
			return err
		}
	}
	if !removed {
		return fmt.Errorf("backup: %s: %w", id, fs.ErrNotExist)
	}
	return nil
}

// Open oeffnet das Archiv einer ID zum Lesen.
func (s *Store) Open(id string) (io.ReadCloser, Info, error) {
	if !idPattern.MatchString(id) {
		return nil, Info{}, fmt.Errorf("backup: ungueltige ID %q", id)
	}
	info, err := s.readSidecar(id)
	if err != nil {
		return nil, Info{}, err
	}
	p, err := s.archiveFile(id)
	if err != nil {
		return nil, Info{}, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, Info{}, err
	}
	return f, info, nil
}

// Prune loescht Archive des Auslosers jenseits der neuesten keep. Der Ausloeser
// "manual" wird nie geloescht, keep < 1 ist ein No-op.
func (s *Store) Prune(trigger string, keep int) error {
	if trigger == "manual" || keep < 1 {
		return nil
	}
	infos, err := s.List()
	if err != nil {
		return err
	}
	seen := 0
	for _, info := range infos {
		if info.Trigger != trigger {
			continue
		}
		seen++
		if seen <= keep {
			continue
		}
		if err := s.Delete(info.ID); err != nil {
			return err
		}
	}
	return nil
}

// exists meldet, ob unter der ID bereits ein Archiv oder ein Sidecar liegt.
func (s *Store) exists(id string) bool {
	for _, name := range []string{id + archiveSuffix, id + lockedSuffix, id + sidecarSuffix} {
		if _, err := os.Stat(filepath.Join(s.Dir, name)); err == nil {
			return true
		}
	}
	return false
}

// archiveFile findet die Archivdatei einer ID, verschluesselt oder nicht.
func (s *Store) archiveFile(id string) (string, error) {
	for _, suffix := range []string{archiveSuffix, lockedSuffix} {
		p := filepath.Join(s.Dir, id+suffix)
		_, err := os.Stat(p)
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("backup: %s: %w", id, fs.ErrNotExist)
}

// readSidecar liest den Sidecar einer ID. Fehlt er, liefert es fs.ErrNotExist.
func (s *Store) readSidecar(id string) (Info, error) {
	raw, err := os.ReadFile(filepath.Join(s.Dir, id+sidecarSuffix))
	if err != nil {
		return Info{}, err
	}
	var info Info
	if err := json.Unmarshal(raw, &info); err != nil {
		return Info{}, fmt.Errorf("backup: Sidecar %s: %w", id, err)
	}
	info.ID = id
	return info, nil
}

// writeTemp schreibt in path mit Modus 0600, fsync inklusive, und liefert die Groesse.
func writeTemp(path string, write func(io.Writer) error) (int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	err = write(f)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return 0, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// archiveID liest die ID aus einem Archivnamen. Andere Dateien liefern false.
func archiveID(name string) (string, bool) {
	id := name
	switch {
	case strings.HasSuffix(name, lockedSuffix):
		id = strings.TrimSuffix(name, lockedSuffix)
	case strings.HasSuffix(name, archiveSuffix):
		id = strings.TrimSuffix(name, archiveSuffix)
	default:
		return "", false
	}
	return id, idPattern.MatchString(id)
}

// DriveLocation legt Archive auf dem zweiten Laufwerk ab, solange es bereit ist.
// Fehlt es, gilt SystemLocation mit der SystemFallback-Einstellung. Ohne beides
// liefert es ErrNoLocation.
type DriveLocation struct {
	// Drive liefert das Archivverzeichnis auf dem Laufwerk und ob das Laufwerk
	// gerade eingehaengt und eingerichtet ist. Nil oder false heisst: nicht bereit.
	Drive  func() (dir string, ready bool)
	System SystemLocation
}

// ArchiveDir liefert das Laufwerksverzeichnis mit dem Ort "drive", sonst den
// Ort aus System.
func (l DriveLocation) ArchiveDir() (string, string, error) {
	if l.Drive != nil {
		if dir, ready := l.Drive(); ready {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return "", "", err
			}
			return dir, "drive", nil
		}
	}
	return l.System.ArchiveDir()
}
