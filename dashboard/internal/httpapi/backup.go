package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/backup"
)

const (
	backupStagingDir     = "backup-staging"
	backupDownloadMaxAge = time.Hour
	backupMinPassword    = 8
)

// backupIDPattern ist dieselbe Form wie die Archiv-IDs im Paket backup. Jede ID
// wird geprueft, bevor sie einen Pfad bildet.
var backupIDPattern = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[a-z_]+$`)

// backupAreaOrder legt die Reihenfolge der Bereiche in der Statusantwort fest.
var backupAreaOrder = []backup.Area{
	backup.AreaSettings,
	backup.AreaDevices,
	backup.AreaUsers,
	backup.AreaSecrets,
	backup.AreaStats,
	backup.AreaHistory,
	backup.AreaBattery,
}

// BackupDeps bündelt, was die Backup-Routen unter /api/v1/backup/ brauchen.
// Nil in RouterDependencies heisst: die Routen fehlen.
type BackupDeps struct {
	// DataDir ist das Datenverzeichnis, in dem Staging und Einstellungen liegen.
	DataDir string
	// Sources nennt die Dateien, die ein Archiv liest.
	Sources backup.Sources
	// Version ist die Versionsangabe des Knotens für den Sidecar.
	Version string
	// Settings liefert die aktuellen Einstellungen, bei jedem Aufruf neu gelesen.
	Settings func() backup.Settings
	// Location liefert den Ablageort der Archive.
	Location backup.Location
	// Jobs führt Export-Aufträge aus, höchstens einer gleichzeitig. Pflicht.
	Jobs *backup.Jobs
	// Scheduler liefert den letzten übersprungenen automatischen Lauf. Nil ist erlaubt.
	Scheduler *backup.Scheduler
	// Now liefert die Uhrzeit, Standard time.Now.
	Now func() time.Time
}

// backupStatus ist die Antwort von GET /api/v1/backup/status.
type backupStatus struct {
	Settings    backup.Settings  `json:"settings"`
	Archives    []backup.Info    `json:"archives"`
	Job         backup.JobStatus `json:"job"`
	LastSkipped backupSkipped    `json:"last_skipped"`
	Place       string           `json:"place"`
	Areas       []backupAreaSize `json:"areas"`
}

// backupSkipped nennt den letzten übersprungenen automatischen Lauf. At ist
// null, solange nichts übersprungen wurde.
type backupSkipped struct {
	At     *time.Time `json:"at"`
	Reason string     `json:"reason"`
}

// backupAreaSize nennt die Größe der aktuellen Dateien eines Bereichs in Bytes.
type backupAreaSize struct {
	Key  backup.Area `json:"key"`
	Size int64       `json:"size"`
}

// backupAccess prüft Anmeldung, eine der Rollen und HTTPS. Es liefert den Nutzer,
// wenn alles passt, sonst schreibt es die Antwort selbst.
func backupAccess(w http.ResponseWriter, r *http.Request, manager *auth.Manager, anyOf ...string) (auth.User, bool) {
	if manager == nil {
		writeError(w, http.StatusNotImplemented, "authentication_unavailable", "Anmeldung ist nicht konfiguriert")
		return auth.User{}, false
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
		return auth.User{}, false
	}
	if !slices.ContainsFunc(anyOf, func(role string) bool { return auth.HasRole(user, role) }) {
		writeErrorKey(w, http.StatusForbidden, "backup_forbidden", "error.backup_forbidden", nil, "Für das Backup fehlt die Berechtigung")
		return auth.User{}, false
	}
	if !requireHTTPS(w, r) {
		return auth.User{}, false
	}
	return user, true
}

// backupWrite prüft wie backupAccess und zusätzlich das CSRF-Token.
func backupWrite(w http.ResponseWriter, r *http.Request, manager *auth.Manager, role string) (auth.User, bool) {
	user, ok := backupAccess(w, r, manager, role)
	if !ok {
		return auth.User{}, false
	}
	if !requireCSRF(w, r, manager) {
		return auth.User{}, false
	}
	return user, true
}

func (d *BackupDeps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *BackupDeps) settings() backup.Settings {
	if d.Settings != nil {
		return d.Settings()
	}
	return backup.DefaultSettings()
}

// archiveStore öffnet den Ablageort. Ohne Ort liefert es backup.ErrNoLocation.
func (d *BackupDeps) archiveStore() (*backup.Store, string, error) {
	if d.Location == nil {
		return nil, "", backup.ErrNoLocation
	}
	dir, place, err := d.Location.ArchiveDir()
	if err != nil {
		return nil, "", err
	}
	store, err := backup.NewStore(dir)
	if err != nil {
		return nil, "", err
	}
	return store, place, nil
}

func (d *BackupDeps) stagingDir() string {
	return filepath.Join(d.DataDir, backupStagingDir)
}

// downloadNames liefert die Dateinamen eines Downloads mit und ohne Passwort.
func downloadNames(id string) []string {
	return []string{"download-" + id + archiveSuffixPlain, "download-" + id + archiveSuffixLocked}
}

const (
	archiveSuffixPlain  = ".tar.gz"
	archiveSuffixLocked = ".tar.gz.enc"
)

// downloadPath findet die Datei eines Downloads. Ohne Treffer ist der Pfad leer.
func (d *BackupDeps) downloadPath(id string) (path string, locked bool) {
	for i, name := range downloadNames(id) {
		p := filepath.Join(d.stagingDir(), name)
		if _, err := os.Stat(p); err == nil {
			return p, i == 1
		}
	}
	return "", false
}

// cleanupDownloads entfernt Downloads, die älter als eine Stunde sind.
func (d *BackupDeps) cleanupDownloads(now time.Time) {
	entries, err := os.ReadDir(d.stagingDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "download-") {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < backupDownloadMaxAge {
			continue
		}
		_ = os.Remove(filepath.Join(d.stagingDir(), e.Name()))
	}
}

// newDownloadID liefert eine freie Archiv-ID mit dem Auslöser manual.
func (d *BackupDeps) newDownloadID() string {
	at := d.now().UTC().Truncate(time.Second)
	for {
		id := at.Format("20060102-150405") + "-manual"
		if p, _ := d.downloadPath(id); p == "" {
			return id
		}
		at = at.Add(time.Second)
	}
}

func (d *BackupDeps) handleStatus(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if _, ok := backupAccess(w, r, manager, auth.RoleBackupExport, auth.RoleBackupImport); !ok {
			return
		}
		d.cleanupDownloads(d.now())

		status := backupStatus{Settings: d.settings(), Archives: []backup.Info{}, Areas: d.areaSizes()}
		store, place, err := d.archiveStore()
		switch {
		case errors.Is(err, backup.ErrNoLocation):
		case err != nil:
			writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
			return
		default:
			archives, err := store.List()
			if err != nil {
				writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
				return
			}
			if archives != nil {
				status.Archives = archives
			}
			status.Place = place
		}
		if d.Jobs != nil {
			status.Job = d.Jobs.Status()
		}
		if d.Scheduler != nil {
			at, reason := d.Scheduler.LastSkipped()
			if !at.IsZero() {
				status.LastSkipped = backupSkipped{At: &at, Reason: reason}
			}
		}
		writeJSON(w, status)
	}
}

// areaSizes summiert die Größe der live vorhandenen Dateien je Bereich.
func (d *BackupDeps) areaSizes() []backupAreaSize {
	sizes := map[backup.Area]int64{}
	walk := func(dir string, root backup.Root) {
		if dir == "" {
			return
		}
		_ = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return nil
			}
			entry, ok := backup.Lookup(root, filepath.ToSlash(rel))
			if !ok || entry.Area == "" {
				return nil
			}
			if info, err := e.Info(); err == nil {
				sizes[entry.Area] += info.Size()
			}
			return nil
		})
	}
	walk(d.Sources.DataDir, backup.RootData)
	walk(d.Sources.DevicesDir, backup.RootDevices)
	for _, state := range d.Sources.BatteryStates {
		if info, err := os.Stat(state); err == nil {
			sizes[backup.AreaBattery] += info.Size()
		}
	}
	out := make([]backupAreaSize, 0, len(backupAreaOrder))
	for _, area := range backupAreaOrder {
		out = append(out, backupAreaSize{Key: area, Size: sizes[area]})
	}
	return out
}

func (d *BackupDeps) handleSettings(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodPut)
			return
		}
		user, ok := backupWrite(w, r, manager, auth.RoleBackupExport)
		if !ok {
			return
		}
		var body backup.Settings
		if err := decodeBody(r, &body); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "backup_bad_request", err)
			return
		}
		if (body.Secrets || body.Content[backup.AreaSecrets]) && !auth.HasRole(user, auth.RoleBackupSecrets) {
			writeErrorKey(w, http.StatusForbidden, "backup_forbidden", "error.backup_forbidden", nil, "Für das Backup fehlt die Berechtigung")
			return
		}
		if err := backup.SaveSettings(d.DataDir, body); err != nil {
			writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
			return
		}
		// Gespeichert wird bereinigt. Die Antwort zeigt, was tatsächlich gilt.
		saved, err := backup.LoadSettings(d.DataDir)
		if err != nil {
			writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
			return
		}
		writeJSON(w, saved)
	}
}

// backupStartRequest ist der Körper von POST /api/v1/backup/archives.
type backupStartRequest struct {
	Areas    map[backup.Area]bool `json:"areas"`
	Secrets  bool                 `json:"secrets"`
	Password string               `json:"password"`
	Target   string               `json:"target"`
}

func (d *BackupDeps) handleArchives(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		user, ok := backupWrite(w, r, manager, auth.RoleBackupExport)
		if !ok {
			return
		}
		var body backupStartRequest
		if err := decodeBody(r, &body); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "backup_bad_request", err)
			return
		}
		if body.Secrets && !auth.HasRole(user, auth.RoleBackupSecrets) {
			writeErrorKey(w, http.StatusForbidden, "backup_forbidden", "error.backup_forbidden", nil, "Für das Backup fehlt die Berechtigung")
			return
		}
		if body.Target != "store" && body.Target != "download" {
			writeErrorKey(w, http.StatusBadRequest, "backup_bad_request", "error.backup_bad_request", nil, "Die Anfrage zum Backup ist ungültig")
			return
		}
		if body.Password != "" && utf8.RuneCountInString(body.Password) < backupMinPassword {
			writeErrorKey(w, http.StatusBadRequest, "backup_password_short", "error.backup_password_short", nil, "Das Passwort braucht mindestens 8 Zeichen")
			return
		}

		opts := backup.CreateOptions{
			Areas:       body.Areas,
			Secrets:     body.Secrets,
			Trigger:     "manual",
			NodeVersion: d.Version,
			Password:    body.Password,
		}
		var (
			id    string
			store *backup.Store
			place string
		)
		if body.Target == "store" {
			var err error
			store, place, err = d.archiveStore()
			if errors.Is(err, backup.ErrNoLocation) {
				writeErrorKey(w, http.StatusConflict, "backup_no_location", "error.backup_no_location", nil, "Kein Ort für Backups verfügbar")
				return
			}
			if err != nil {
				writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
				return
			}
		} else {
			d.cleanupDownloads(d.now())
			id = d.newDownloadID()
		}

		var jobID string
		var err error
		if body.Target == "store" {
			jobID, err = d.Jobs.Start("export", []string{"write", "finish"}, func(ctx context.Context, phase func(int)) (string, error) {
				phase(0)
				info, err := store.Add(backup.Info{
					Trigger: "manual",
					Place:   place,
					Version: d.Version,
					Areas:   backup.ChosenAreas(body.Areas),
					Secrets: body.Secrets,
					Locked:  body.Password != "",
				}, 0, func(w io.Writer) error {
					_, err := backup.Create(ctx, d.Sources, opts, w)
					return err
				})
				phase(1)
				return info.ID, err
			})
		} else {
			jobID, err = d.Jobs.Start("export", []string{"write", "finish"}, func(ctx context.Context, phase func(int)) (string, error) {
				phase(0)
				if err := d.writeDownload(ctx, id, opts); err != nil {
					return "", err
				}
				phase(1)
				return id, nil
			})
		}
		if errors.Is(err, backup.ErrBusy) {
			writeErrorKey(w, http.StatusConflict, "backup_busy", "error.backup_busy", nil, "Ein Backup läuft bereits")
			return
		}
		if err != nil {
			writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
			return
		}
		writeJSONStatus(w, http.StatusAccepted, map[string]string{"job": jobID})
	}
}

// writeDownload schreibt ein Archiv in den Staging-Ordner, für den Download.
func (d *BackupDeps) writeDownload(ctx context.Context, id string, opts backup.CreateOptions) error {
	if err := os.MkdirAll(d.stagingDir(), 0o700); err != nil {
		return err
	}
	name := downloadNames(id)[0]
	if opts.Password != "" {
		name = downloadNames(id)[1]
	}
	path := filepath.Join(d.stagingDir(), name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = backup.Create(ctx, d.Sources, opts, f)
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

func (d *BackupDeps) handleArchive(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/backup/archives/")
		switch r.Method {
		case http.MethodGet:
			if _, ok := backupAccess(w, r, manager, auth.RoleBackupExport); !ok {
				return
			}
			if !backupIDPattern.MatchString(id) {
				writeErrorKey(w, http.StatusBadRequest, "backup_bad_request", "error.backup_bad_request", nil, "Die Anfrage zum Backup ist ungültig")
				return
			}
			d.serveArchive(w, id)
		case http.MethodDelete:
			if _, ok := backupWrite(w, r, manager, auth.RoleBackupExport); !ok {
				return
			}
			if !backupIDPattern.MatchString(id) {
				writeErrorKey(w, http.StatusBadRequest, "backup_bad_request", "error.backup_bad_request", nil, "Die Anfrage zum Backup ist ungültig")
				return
			}
			store, _, err := d.archiveStore()
			if errors.Is(err, backup.ErrNoLocation) {
				writeErrorKey(w, http.StatusNotFound, "backup_not_found", "error.backup_not_found", nil, "Backup-Archiv nicht gefunden")
				return
			}
			if err != nil {
				writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
				return
			}
			if err := store.Delete(id); errors.Is(err, fs.ErrNotExist) {
				writeErrorKey(w, http.StatusNotFound, "backup_not_found", "error.backup_not_found", nil, "Backup-Archiv nicht gefunden")
				return
			} else if err != nil {
				writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
				return
			}
			writeJSON(w, map[string]string{"id": id, "status": "deleted"})
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodDelete)
		}
	}
}

// serveArchive streamt ein Archiv als Anhang. Ein Download aus dem Staging-Ordner
// wird danach gelöscht.
func (d *BackupDeps) serveArchive(w http.ResponseWriter, id string) {
	if path, locked := d.downloadPath(id); path != "" {
		f, err := os.Open(path)
		if err != nil {
			writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
			return
		}
		defer os.Remove(path)
		defer f.Close()
		name := id + archiveSuffixPlain
		if locked {
			name = id + archiveSuffixLocked
		}
		writeArchiveStream(w, name, f)
		return
	}
	store, _, err := d.archiveStore()
	if errors.Is(err, backup.ErrNoLocation) {
		writeErrorKey(w, http.StatusNotFound, "backup_not_found", "error.backup_not_found", nil, "Backup-Archiv nicht gefunden")
		return
	}
	if err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
		return
	}
	f, info, err := store.Open(id)
	if errors.Is(err, fs.ErrNotExist) {
		writeErrorKey(w, http.StatusNotFound, "backup_not_found", "error.backup_not_found", nil, "Backup-Archiv nicht gefunden")
		return
	}
	if err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
		return
	}
	defer f.Close()
	name := info.ID + archiveSuffixPlain
	if info.Locked {
		name = info.ID + archiveSuffixLocked
	}
	writeArchiveStream(w, name, f)
}

func writeArchiveStream(w http.ResponseWriter, name string, body io.Reader) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

func (d *BackupDeps) handleJob(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if _, ok := backupAccess(w, r, manager, auth.RoleBackupExport, auth.RoleBackupImport); !ok {
			return
		}
		var status backup.JobStatus
		if d.Jobs != nil {
			status = d.Jobs.Status()
		}
		writeJSON(w, status)
	}
}
