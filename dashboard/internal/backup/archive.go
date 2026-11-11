package backup

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FormatVersion ist das Archivformat, das Create schreibt. Ein neueres Format lehnt ExtractToStaging ab.
const FormatVersion = 1

// MaxFileSize und MaxTotalSize begrenzen die Dateien eines Archivs.
const MaxFileSize, MaxTotalSize int64 = 64 << 20, 256 << 20

const (
	manifestName      = "manifest.json"
	configArchiveName = "config/config.json"
	usersArchiveName  = "data/users.json"
	maxManifestBytes  = 4 << 20
)

var (
	// ErrCorrupt meldet ein beschädigtes oder manipuliertes Archiv.
	ErrCorrupt = errors.New("Backup-Archiv ist beschädigt")
	// ErrIncompatible meldet eine Datei, die kein Backup-Archiv ist.
	ErrIncompatible = errors.New("Datei ist kein Backup-Archiv")
	// ErrPasswordRequired meldet ein geschütztes Archiv ohne Passwort.
	ErrPasswordRequired = errors.New("Backup-Archiv braucht ein Passwort")
	// ErrWrongPassword meldet ein falsches Passwort.
	ErrWrongPassword = errors.New("Passwort passt nicht zum Backup-Archiv")
	// ErrTooNew meldet ein Archiv aus einem neueren Format oder einer neueren Version.
	ErrTooNew = errors.New("Backup-Archiv stammt aus einer neueren Version")
)

// NodeVersion ist die Version dieses Knotens, gegen die ein Archiv geprüft wird.
// Der Build "dev" lehnt nie ab. Die Verdrahtung mit buildVersion folgt in einer späteren Aufgabe.
var NodeVersion = "dev"

// stripUsers ist der Platzhalter für Task 4. Bei Secrets=false läuft users.json
// vor dem Schreiben durch diese Funktion. Nil lässt die Datei unverändert.
var stripUsers func([]byte) ([]byte, error)

// Sources nennt, woher Create die Dateien des Knotens liest.
type Sources struct {
	DataDir       string
	DevicesDir    string
	AppConfigPath string
	BatteryStates map[string]string // id -> state_file
}

// CreateOptions legt fest, was in das Archiv kommt.
type CreateOptions struct {
	Areas       map[Area]bool
	Secrets     bool
	Trigger     string
	NodeVersion string
	Password    string
}

// Create schreibt ein Archiv nach w. Die Dateien werden nacheinander gestreamt,
// das Manifest mit den Prüfsummen steht zuletzt im Tar.
func Create(ctx context.Context, src Sources, opts CreateOptions, w io.Writer) (Manifest, error) {
	allowed := make(map[string]bool)
	for _, e := range ArchiveEntries(opts.Areas) {
		allowed[string(e.Root)+"/"+e.Pattern] = true
	}
	keep := func(e Entry) bool {
		if !allowed[string(e.Root)+"/"+e.Pattern] {
			return false
		}
		return e.Class != ClassSecret || opts.Secrets
	}

	// Mit Passwort wandert das ganze tar.gz durch den verschlüsselten Strom.
	out := w
	var enc io.WriteCloser
	if opts.Password != "" {
		var err error
		enc, err = NewEncryptWriter(w, opts.Password)
		if err != nil {
			return Manifest{}, err
		}
		out = enc
	}

	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	aw := &archiveWriter{
		ctx:     ctx,
		tw:      tw,
		files:   map[string]string{},
		secrets: opts.Secrets,
	}

	if err := aw.addTree(RootData, src.DataDir, keep); err != nil {
		return Manifest{}, err
	}
	if err := aw.addTree(RootDevices, src.DevicesDir, keep); err != nil {
		return Manifest{}, err
	}
	if src.AppConfigPath != "" {
		if err := aw.addFile(configArchiveName, src.AppConfigPath); err != nil {
			return Manifest{}, err
		}
	}
	if opts.Areas[AreaBattery] {
		ids := make([]string, 0, len(src.BatteryStates))
		for id := range src.BatteryStates {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			base := id + ".json"
			if !validBatteryFile(base) {
				return Manifest{}, fmt.Errorf("Batteriezustand %q hat keinen gültigen Namen", id)
			}
			state := src.BatteryStates[id]
			if _, err := os.Stat(state); errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err := aw.addFile("battery/"+base, state); err != nil {
				return Manifest{}, err
			}
		}
	}

	m := Manifest{
		Format:      FormatVersion,
		NodeVersion: opts.NodeVersion,
		Hostname:    hostname(),
		Trigger:     opts.Trigger,
		CreatedAt:   time.Now().UTC(),
		Areas:       ChosenAreas(opts.Areas),
		Secrets:     opts.Secrets && (aw.secretFiles > 0 || aw.hasFile(usersArchiveName)),
		Files:       aw.files,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	if _, err := aw.writeEntry(manifestName, int64(len(data)), time.Now(), bytes.NewReader(data)); err != nil {
		return Manifest{}, err
	}
	if err := tw.Close(); err != nil {
		return Manifest{}, err
	}
	if err := gz.Close(); err != nil {
		return Manifest{}, err
	}
	if enc != nil {
		if err := enc.Close(); err != nil {
			return Manifest{}, err
		}
	}
	return m, nil
}

// archiveWriter schreibt Einträge in das Tar und sammelt die Prüfsummen.
type archiveWriter struct {
	ctx     context.Context
	tw      *tar.Writer
	files   map[string]string
	total   int64
	secrets bool
	// secretFiles zählt die ClassSecret-Dateien, die ins Archiv kamen.
	secretFiles int
}

// hasFile meldet, ob unter name ein Eintrag geschrieben wurde.
func (aw *archiveWriter) hasFile(name string) bool {
	_, ok := aw.files[name]
	return ok
}

// addTree nimmt alle Dateien unter dir auf, die der Katalog der Wurzel zuordnet
// und keep erlaubt. Fehlt das Verzeichnis, ist das kein Fehler.
func (aw *archiveWriter) addTree(root Root, dir string, keep func(Entry) bool) error {
	if dir == "" {
		return nil
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		e, ok := Lookup(root, rel)
		if !ok || !keep(e) {
			return nil
		}
		if err := aw.addFile(string(root)+"/"+rel, p); err != nil {
			return err
		}
		if e.Class == ClassSecret {
			aw.secretFiles++
		}
		return nil
	})
}

// addFile streamt eine Datei unter dem Archivnamen name ins Tar.
func (aw *archiveWriter) addFile(name, file string) error {
	if err := aw.ctx.Err(); err != nil {
		return err
	}
	if name == usersArchiveName && stripUsers != nil && !aw.secrets {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		data, err = stripUsers(data)
		if err != nil {
			return err
		}
		return aw.addBytes(name, data)
	}

	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s ist keine reguläre Datei", file)
	}
	size := info.Size()
	if err := aw.reserve(name, size); err != nil {
		return err
	}
	sum, err := aw.writeEntry(name, size, info.ModTime(), f)
	if err != nil {
		return err
	}
	aw.files[name] = sum
	return nil
}

// addBytes nimmt Inhalte auf, die schon im Speicher liegen (nur kleine Dateien).
func (aw *archiveWriter) addBytes(name string, data []byte) error {
	if err := aw.ctx.Err(); err != nil {
		return err
	}
	size := int64(len(data))
	if err := aw.reserve(name, size); err != nil {
		return err
	}
	sum, err := aw.writeEntry(name, size, time.Now(), bytes.NewReader(data))
	if err != nil {
		return err
	}
	aw.files[name] = sum
	return nil
}

// reserve prüft die Größenlimits und zählt die Summe hoch.
func (aw *archiveWriter) reserve(name string, size int64) error {
	if size > MaxFileSize {
		return fmt.Errorf("%s ist größer als %d Byte", name, MaxFileSize)
	}
	if aw.total+size > MaxTotalSize {
		return fmt.Errorf("Das Archiv wird größer als %d Byte", MaxTotalSize)
	}
	aw.total += size
	return nil
}

// writeEntry schreibt einen Tar-Eintrag und liefert die sha256-Summe des Inhalts.
func (aw *archiveWriter) writeEntry(name string, size int64, mod time.Time, r io.Reader) (string, error) {
	hdr := &tar.Header{Name: name, Mode: 0o600, Typeflag: tar.TypeReg, Size: size, ModTime: mod}
	if err := aw.tw.WriteHeader(hdr); err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(aw.tw, h), r, size); err != nil {
		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("%s hat sich während des Backups verkürzt", name)
		}
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ExtractToStaging liest ein Archiv und legt die Dateien unter stagingDir ab,
// mit Modus 0600. Schlägt die Prüfung fehl, bleibt das Staging für den Aufrufer
// zum Entfernen. Außerhalb von stagingDir wird nichts angelegt.
// Ist das Archiv verschlüsselt, braucht es password. Bei einem unverschlüsselten
// Archiv wird password ignoriert.
func ExtractToStaging(ctx context.Context, r io.Reader, password, stagingDir string) (Manifest, error) {
	br := bufio.NewReader(r)
	var src io.Reader = br
	head, _ := br.Peek(len(cryptMagic))
	if IsEncrypted(head) {
		if password == "" {
			return Manifest{}, ErrPasswordRequired
		}
		dec, err := NewDecryptReader(br, password)
		if err != nil {
			return Manifest{}, err
		}
		src = dec
	}
	gz, err := gzip.NewReader(src)
	if err != nil {
		return Manifest{}, streamError(err, ErrIncompatible)
	}
	defer gz.Close()
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return Manifest{}, err
	}

	tr := tar.NewReader(gz)
	got := map[string]string{}
	var foreign []string
	var manifestData []byte
	haveManifest := false
	var total int64
	for first := true; ; first = false {
		if err := ctx.Err(); err != nil {
			return Manifest{}, err
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if first {
				return Manifest{}, streamError(err, ErrIncompatible)
			}
			return Manifest{}, streamError(err, ErrCorrupt)
		}
		name := hdr.Name
		if unsafeName(name) {
			return Manifest{}, fmt.Errorf("%w: Pfad %q ist nicht erlaubt", ErrCorrupt, name)
		}
		if hdr.Typeflag != tar.TypeReg {
			return Manifest{}, fmt.Errorf("%w: %q ist keine reguläre Datei", ErrCorrupt, name)
		}
		if name == manifestName {
			if haveManifest {
				return Manifest{}, fmt.Errorf("%w: manifest.json steht doppelt im Archiv", ErrCorrupt)
			}
			data, err := io.ReadAll(io.LimitReader(tr, maxManifestBytes+1))
			if err != nil {
				return Manifest{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
			}
			if len(data) > maxManifestBytes {
				return Manifest{}, fmt.Errorf("%w: manifest.json ist zu groß", ErrCorrupt)
			}
			manifestData, haveManifest = data, true
			continue
		}
		known, err := checkEntry(name)
		if err != nil {
			return Manifest{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		if !known {
			foreign = append(foreign, name)
			continue
		}
		if hdr.Size > MaxFileSize {
			return Manifest{}, fmt.Errorf("%w: %s ist größer als %d Byte", ErrCorrupt, name, MaxFileSize)
		}
		total += hdr.Size
		if total > MaxTotalSize {
			return Manifest{}, fmt.Errorf("%w: das Archiv ist größer als %d Byte", ErrCorrupt, MaxTotalSize)
		}
		if _, dup := got[name]; dup {
			return Manifest{}, fmt.Errorf("%w: %s steht doppelt im Archiv", ErrCorrupt, name)
		}
		sum, err := extractFile(stagingDir, name, hdr.Size, tr)
		if err != nil {
			return Manifest{}, err
		}
		got[name] = sum
	}

	// Das Ende des gzip-Stroms prüft die Prüfsumme des Stroms, die der Tar-Leser nicht liest.
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return Manifest{}, streamError(err, ErrCorrupt)
	}
	if !haveManifest {
		return Manifest{}, fmt.Errorf("%w: kein manifest.json", ErrIncompatible)
	}
	var m Manifest
	if err := json.Unmarshal(manifestData, &m); err != nil {
		return Manifest{}, fmt.Errorf("%w: manifest.json: %v", ErrCorrupt, err)
	}
	if m.Format == 0 {
		return Manifest{}, fmt.Errorf("%w: manifest.json hat kein Format", ErrIncompatible)
	}
	if m.Format > FormatVersion || newerThanNode(m.NodeVersion) {
		return Manifest{}, ErrTooNew
	}
	if len(foreign) > 0 {
		return Manifest{}, fmt.Errorf("%w: unbekannter Eintrag %q", ErrCorrupt, foreign[0])
	}

	// Jede Datei hat einen Manifesteintrag, jeder Eintrag eine Datei.
	for _, name := range sortedKeys(m.Files) {
		if _, ok := got[name]; !ok {
			return Manifest{}, fmt.Errorf("%w: %s fehlt im Archiv", ErrCorrupt, name)
		}
	}
	for _, name := range sortedKeys(got) {
		want, ok := m.Files[name]
		if !ok {
			return Manifest{}, fmt.Errorf("%w: %s steht nicht im Manifest", ErrCorrupt, name)
		}
		if want != got[name] {
			return Manifest{}, fmt.Errorf("%w: Prüfsumme von %s stimmt nicht", ErrCorrupt, name)
		}
	}
	return m, nil
}

// streamError ordnet einen Lesefehler des Stroms sentinel zu. Ein falsches
// Passwort oder ein beschädigter Strom bleibt, wie er ist.
func streamError(err error, sentinel error) error {
	if errors.Is(err, ErrWrongPassword) || errors.Is(err, ErrCorrupt) {
		return err
	}
	return fmt.Errorf("%w: %v", sentinel, err)
}

// extractFile schreibt genau size Bytes aus tr nach stagingDir/name und liefert die sha256-Summe.
func extractFile(stagingDir, name string, size int64, tr io.Reader) (string, error) {
	target, err := safeJoin(stagingDir, name)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), tr)
	if closeErr := f.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrCorrupt, name, copyErr)
	}
	if n != size {
		return "", fmt.Errorf("%w: %s ist abgeschnitten", ErrCorrupt, name)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// checkEntry prüft einen Eintragsnamen gegen das Archivlayout. known ist false
// für Namen außerhalb des Layouts. Ein ungültiger Name im Layout ist ein Fehler.
func checkEntry(name string) (known bool, err error) {
	switch {
	case name == configArchiveName:
		return true, nil
	case strings.HasPrefix(name, "data/"):
		e, ok := Lookup(RootData, strings.TrimPrefix(name, "data/"))
		if !ok || !storable(e) {
			return true, fmt.Errorf("%q liegt außerhalb des Katalogs", name)
		}
		return true, nil
	case strings.HasPrefix(name, "devices/"):
		e, ok := Lookup(RootDevices, strings.TrimPrefix(name, "devices/"))
		if !ok || !storable(e) {
			return true, fmt.Errorf("%q liegt außerhalb des Katalogs", name)
		}
		return true, nil
	case strings.HasPrefix(name, "battery/"):
		if !validBatteryFile(strings.TrimPrefix(name, "battery/")) {
			return true, fmt.Errorf("%q hat keinen gültigen Namen", name)
		}
		return true, nil
	}
	return false, nil
}

// storable meldet, ob ein Katalogeintrag ins Archiv gehört.
func storable(e Entry) bool {
	return e.Class == ClassConfig || e.Class == ClassSecret || e.Class == ClassRuntime
}

// validBatteryFile prüft den Namen eines Batteriezustands ohne Verzeichnisteile.
func validBatteryFile(base string) bool {
	return len(base) > len(".json") && strings.HasSuffix(base, ".json") &&
		!strings.ContainsAny(base, `/\`) && !strings.HasPrefix(base, ".")
}

// unsafeName weist absolute Pfade und alles ab, das aus dem Archiv herausführt.
func unsafeName(name string) bool {
	clean := path.Clean(name)
	return name == "" || path.IsAbs(name) || clean != name || clean == ".." || strings.HasPrefix(clean, "../")
}

// safeJoin ist das Vorbild aus internal/bundlefetch: Der Name bleibt unter root.
func safeJoin(root, name string) (string, error) {
	rel := path.Clean(name)
	if rel == "." || path.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("Pfad %q verlässt das Staging", name)
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	if !strings.HasPrefix(target, filepath.Clean(root)+string(filepath.Separator)) {
		return "", fmt.Errorf("Pfad %q verlässt das Staging", name)
	}
	return target, nil
}

// ChosenAreas liefert die gewählten Bereiche sortiert.
func ChosenAreas(areas map[Area]bool) []Area {
	out := make([]Area, 0, len(areas))
	for a, on := range areas {
		if on {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}
