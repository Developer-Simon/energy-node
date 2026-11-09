package httpapi

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/backup"
)

const (
	backupImportDirName        = "import"
	backupImportUploadName     = "import-upload"
	backupImportPasswordHeader = "X-Backup-Password"
	backupImportMaxAge         = time.Hour
	// backupUploadExtra gibt dem Upload Luft über MaxTotalSize, damit ein zu großes
	// Archiv als beschädigt erkannt wird und nicht schon am Lesen scheitert.
	backupUploadExtra = 1 << 20
)

// backupImportSession ist das Archiv, das zuletzt eingespielt wurde, entpackt im Staging.
type backupImportSession struct {
	manifest backup.Manifest
	staging  string
	created  time.Time
}

// backupImportResponse ist die Antwort von POST /api/v1/backup/import und dem Entsperren.
type backupImportResponse struct {
	Manifest        backupImportManifest `json:"manifest"`
	Areas           []backupImportArea   `json:"areas"`
	Environment     []backupEnvRow       `json:"environment"`
	DifferentDevice bool                 `json:"different_device"`
}

type backupImportManifest struct {
	NodeVersion string        `json:"node_version"`
	Hostname    string        `json:"hostname"`
	Trigger     string        `json:"trigger"`
	CreatedAt   time.Time     `json:"created_at"`
	Areas       []backup.Area `json:"areas"`
	Secrets     bool          `json:"secrets"`
}

type backupImportArea struct {
	Area      backup.Area          `json:"area"`
	Available bool                 `json:"available"`
	Count     int                  `json:"count"`
	Size      int64                `json:"size"`
	Changes   []backupImportChange `json:"changes"`
}

type backupImportChange struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Name  string `json:"name"`
}

// backupEnvRow ist eine Zeile des Schritts Umgebung.
type backupEnvRow struct {
	Key     string `json:"key"`
	Here    string `json:"here"`
	Archive string `json:"archive"`
	Off     string `json:"off"`
}

func (d *BackupDeps) importDir() string {
	return filepath.Join(d.stagingDir(), backupImportDirName)
}

func (d *BackupDeps) importUploadPath() string {
	return filepath.Join(d.stagingDir(), backupImportUploadName)
}

// dropImport entfernt die Importsitzung samt Staging und Upload. Der Aufrufer hält importMu.
func (d *BackupDeps) dropImport() {
	_ = os.RemoveAll(d.importDir())
	_ = os.Remove(d.importUploadPath())
	d.importSession = nil
	d.importPending = false
}

// cleanupStaging räumt Downloads und Importe, die älter als eine Stunde sind. Läuft
// gerade ein Import, bleibt die Sitzung unangetastet.
func (d *BackupDeps) cleanupStaging(now time.Time) {
	d.cleanupDownloads(now)
	if !d.importMu.TryLock() {
		return
	}
	defer d.importMu.Unlock()
	stale := d.importSession != nil && now.Sub(d.importSession.created) >= backupImportMaxAge
	if info, err := os.Stat(d.importUploadPath()); err == nil && now.Sub(info.ModTime()) >= backupImportMaxAge {
		stale = true
	}
	if stale {
		d.dropImport()
	}
}

// isJSONRequest meldet, ob der Körper ein JSON-Objekt mit einem gespeicherten Archiv ist.
func isJSONRequest(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
}

// checkBackupSpace prüft, ob unter dem Datenverzeichnis need Byte frei sind.
func (d *BackupDeps) checkBackupSpace(w http.ResponseWriter, need int64) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(d.DataDir, &st); err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
		return false
	}
	free := uint64(st.Bavail) * uint64(st.Bsize)
	if free < uint64(need) {
		writeErrorKey(w, http.StatusInsufficientStorage, "backup_no_space", "error.backup_no_space", nil, "Auf dem Gerät ist nicht genug Speicherplatz für das Backup")
		return false
	}
	return true
}

// createUpload legt die Upload-Datei neu an, Modus 0600.
func (d *BackupDeps) createUpload() (*os.File, error) {
	if err := os.MkdirAll(d.stagingDir(), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(d.importUploadPath(), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
}

// receiveUpload streamt den Körper der Anfrage als Upload auf den Datenträger.
func (d *BackupDeps) receiveUpload(w http.ResponseWriter, r *http.Request) bool {
	need := int64(backup.MaxTotalSize)
	if r.ContentLength > 0 {
		need = 2 * r.ContentLength
	}
	if !d.checkBackupSpace(w, need) {
		return false
	}
	f, err := d.createUpload()
	if err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
		return false
	}
	body := http.MaxBytesReader(w, r.Body, backup.MaxTotalSize+backupUploadExtra)
	_, copyErr := io.Copy(f, body)
	closeErr := f.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Remove(d.importUploadPath())
		var tooLarge *http.MaxBytesError
		if errors.As(copyErr, &tooLarge) {
			writeErrorKey(w, http.StatusUnprocessableEntity, "backup_corrupt", "error.backup_corrupt", nil, "Das Backup-Archiv ist beschädigt")
			return false
		}
		writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", copyErr)
		return false
	}
	return true
}

// copyStoredArchive legt eine Kopie des gespeicherten Archivs als Upload an, damit
// Entsperren und Einspielen denselben Weg gehen.
func (d *BackupDeps) copyStoredArchive(w http.ResponseWriter, id string) bool {
	store, _, err := d.archiveStore()
	if errors.Is(err, backup.ErrNoLocation) {
		writeErrorKey(w, http.StatusConflict, "backup_no_location", "error.backup_no_location", nil, "Kein Ort für Backups verfügbar")
		return false
	}
	if err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
		return false
	}
	if !d.checkBackupSpace(w, backup.MaxTotalSize) {
		return false
	}
	src, _, err := store.Open(id)
	if errors.Is(err, fs.ErrNotExist) {
		writeErrorKey(w, http.StatusNotFound, "backup_not_found", "error.backup_not_found", nil, "Backup-Archiv nicht gefunden")
		return false
	}
	if err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
		return false
	}
	defer src.Close()
	f, err := d.createUpload()
	if err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
		return false
	}
	_, copyErr := io.Copy(f, src)
	closeErr := f.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Remove(d.importUploadPath())
		writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", copyErr)
		return false
	}
	return true
}

// extractUpload entpackt den Upload in ein frisches Importverzeichnis.
func (d *BackupDeps) extractUpload(ctx context.Context, password string) (backup.Manifest, error) {
	f, err := os.Open(d.importUploadPath())
	if err != nil {
		return backup.Manifest{}, err
	}
	defer f.Close()
	if err := os.RemoveAll(d.importDir()); err != nil {
		return backup.Manifest{}, err
	}
	return backup.ExtractToStaging(ctx, f, password, d.importDir())
}

// finishImport entpackt den Upload und antwortet. Der Aufrufer hält importMu.
func (d *BackupDeps) finishImport(ctx context.Context, w http.ResponseWriter, password string) {
	manifest, err := d.extractUpload(ctx, password)
	switch {
	case errors.Is(err, backup.ErrPasswordRequired):
		_ = os.RemoveAll(d.importDir())
		d.importPending = true
		writeErrorKey(w, http.StatusConflict, "password_required", "error.password_required", nil, "Das Backup-Archiv ist verschlüsselt und braucht ein Passwort")
		return
	case errors.Is(err, backup.ErrWrongPassword):
		_ = os.RemoveAll(d.importDir())
		d.importPending = true
		writeErrorKey(w, http.StatusUnprocessableEntity, "wrong_password", "error.wrong_password", nil, "Das Passwort passt nicht zum Backup-Archiv")
		return
	case err != nil:
		d.dropImport()
		writeImportFailure(w, err)
		return
	}

	resp, err := d.importResponse(manifest)
	if err != nil {
		d.dropImport()
		writeImportFailure(w, err)
		return
	}
	_ = os.Remove(d.importUploadPath())
	d.importPending = false
	d.importSession = &backupImportSession{manifest: manifest, staging: d.importDir(), created: d.now()}
	writeJSON(w, resp)
}

// writeImportFailure ordnet einen Fehler des Imports dem Fehlercode zu.
func writeImportFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, backup.ErrCorrupt):
		writeErrorDetail(w, http.StatusUnprocessableEntity, "backup_corrupt", err)
	case errors.Is(err, backup.ErrIncompatible):
		writeErrorDetail(w, http.StatusUnprocessableEntity, "backup_incompatible", err)
	case errors.Is(err, backup.ErrTooNew):
		writeErrorDetail(w, http.StatusUnprocessableEntity, "backup_too_new", err)
	default:
		writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
	}
}

// importResponse baut die Vorschau des entpackten Archivs.
func (d *BackupDeps) importResponse(m backup.Manifest) (backupImportResponse, error) {
	areas, err := backup.Preview(d.importDir(), d.Sources)
	if err != nil {
		return backupImportResponse{}, err
	}
	host, _ := os.Hostname()
	resp := backupImportResponse{
		Manifest: backupImportManifest{
			NodeVersion: m.NodeVersion,
			Hostname:    m.Hostname,
			Trigger:     m.Trigger,
			CreatedAt:   m.CreatedAt,
			Areas:       m.Areas,
			Secrets:     m.Secrets,
		},
		Areas:       make([]backupImportArea, 0, len(areas)),
		Environment: []backupEnvRow{},
		// Die Umgebung nur zeigen, wenn das Archiv von einem anderen Gerät stammt.
		DifferentDevice: m.Hostname != host,
	}
	for _, a := range areas {
		item := backupImportArea{Area: a.Area, Available: a.Available, Count: a.Count, Size: a.Size, Changes: []backupImportChange{}}
		for _, c := range a.Changes {
			item.Changes = append(item.Changes, backupImportChange{Kind: c.Kind, Label: c.Label, Name: c.Name})
		}
		resp.Areas = append(resp.Areas, item)
	}
	if resp.DifferentDevice {
		rows, err := d.environmentRows()
		if err != nil {
			return backupImportResponse{}, err
		}
		resp.Environment = rows
	}
	return resp, nil
}

// environmentRows vergleicht die Konfiguration im Staging mit der dieses Geräts.
// Fehlt eine der beiden Konfigurationen, gibt es keine Umgebungszeilen.
func (d *BackupDeps) environmentRows() ([]backupEnvRow, error) {
	archiveConfig, err := os.ReadFile(filepath.Join(d.importDir(), "config", "config.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return []backupEnvRow{}, nil
	}
	if err != nil {
		return nil, err
	}
	if d.Sources.AppConfigPath == "" {
		return []backupEnvRow{}, nil
	}
	hereConfig, err := os.ReadFile(d.Sources.AppConfigPath)
	if errors.Is(err, fs.ErrNotExist) {
		return []backupEnvRow{}, nil
	}
	if err != nil {
		return nil, err
	}
	bridge, err := os.ReadFile(filepath.Join(d.importDir(), "data", "bridge.json"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	rows, err := backup.EnvironmentRows(archiveConfig, hereConfig, bridge)
	if err != nil {
		return nil, errors.Join(backup.ErrCorrupt, err)
	}
	out := make([]backupEnvRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, backupEnvRow{Key: r.Key, Here: r.Here, Archive: r.Archive, Off: r.Off})
	}
	return out, nil
}

// handleImport nimmt ein Archiv als Upload oder ein gespeichertes Archiv entgegen
// und spielt seine Vorschau ein.
func (d *BackupDeps) handleImport(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if _, ok := backupWrite(w, r, manager, auth.RoleBackupImport); !ok {
			return
		}
		d.cleanupStaging(d.now())
		d.importMu.Lock()
		defer d.importMu.Unlock()

		stored := ""
		if isJSONRequest(r) {
			var body struct {
				Archive string `json:"archive"`
			}
			if err := decodeBody(r, &body); err != nil {
				writeErrorDetail(w, http.StatusBadRequest, "backup_bad_request", err)
				return
			}
			if !backupIDPattern.MatchString(body.Archive) {
				writeErrorKey(w, http.StatusBadRequest, "backup_bad_request", "error.backup_bad_request", nil, "Die Anfrage zum Backup ist ungültig")
				return
			}
			stored = body.Archive
		}

		// Eine neue Anfrage ersetzt die bisherige Sitzung.
		d.dropImport()
		if stored != "" {
			if !d.copyStoredArchive(w, stored) {
				d.dropImport()
				return
			}
		} else if !d.receiveUpload(w, r) {
			d.dropImport()
			return
		}
		d.finishImport(r.Context(), w, r.Header.Get(backupImportPasswordHeader))
	}
}

// handleImportUnlock entsperrt den wartenden Upload mit dem Passwort aus dem Körper.
func (d *BackupDeps) handleImportUnlock(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if _, ok := backupWrite(w, r, manager, auth.RoleBackupImport); !ok {
			return
		}
		d.cleanupStaging(d.now())
		d.importMu.Lock()
		defer d.importMu.Unlock()

		var body struct {
			Password string `json:"password"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "backup_bad_request", err)
			return
		}
		if !d.importPending {
			writeErrorKey(w, http.StatusConflict, "backup_no_import", "error.backup_no_import", nil, "Es liegt kein Backup zum Einspielen bereit")
			return
		}
		d.finishImport(r.Context(), w, body.Password)
	}
}
