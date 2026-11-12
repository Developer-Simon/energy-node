package httpapi

import (
	"context"
	"io/fs"
	"net/http"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/storage"
)

// StorageDeps bündelt die Speicher-Routen unter /api/v1/storage/. Nil in
// RouterDependencies heisst: die Routen fehlen.
type StorageDeps struct {
	// DataDir ist das Datenverzeichnis mit storage.json.
	DataDir string
	// Service führt Einrichten und Entfernen aus und liefert den Auftragsstand.
	Service *storage.Service
	// Lister listet die Laufwerke. Nil heisst: keine Laufwerke.
	Lister storage.Lister
	// Status liefert den Zustand des Laufwerkswaechters. Nil heisst: kein Waechter.
	Status func() storage.Status
	// Recheck lässt den Waechter sofort prüfen, statt auf den nächsten Takt zu
	// warten, und liefert den neuen Zustand. Nil heisst: nur lesen.
	Recheck func(ctx context.Context) storage.Status
	// DemoSpace ist die Demo: der Einhängepunkt ist ein Ordner auf der
	// Systemplatte. Größe und Belegung kommen dann vom gelisteten Stick mit
	// der uuid aus der Konfiguration.
	DemoSpace bool
}

// storageAccess prüft Anmeldung, storage_manage und HTTPS. Bei write kommt
// das CSRF-Token dazu.
func storageAccess(w http.ResponseWriter, r *http.Request, manager *auth.Manager, write bool) (auth.User, bool) {
	if manager == nil {
		writeError(w, http.StatusNotImplemented, "authentication_unavailable", "Anmeldung ist nicht konfiguriert")
		return auth.User{}, false
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
		return auth.User{}, false
	}
	if !auth.HasRole(user, auth.RoleStorageManage) {
		writeErrorKey(w, http.StatusForbidden, "storage_forbidden", "error.storage_forbidden", nil, "Für den Speicher fehlt die Berechtigung")
		return auth.User{}, false
	}
	if !requireHTTPS(w, r) {
		return auth.User{}, false
	}
	if write && !requireCSRF(w, r, manager) {
		return auth.User{}, false
	}
	return user, true
}

// storageStatusBody ist die Antwort von GET /api/v1/storage/status.
type storageStatusBody struct {
	Config storage.Config    `json:"config"`
	Status storageModeBody   `json:"status"`
	System storageSpace      `json:"system"`
	Drive  storageDriveSpace `json:"drive"`
	Job    storage.Job       `json:"job"`
}

type storageModeBody struct {
	Mode   string     `json:"mode"`
	Since  *time.Time `json:"since"`
	Reason string     `json:"reason"`
}

type storageSpace struct {
	Size int64 `json:"size"`
	Used int64 `json:"used"`
}

type storageDriveSpace struct {
	Size  int64         `json:"size"`
	Used  int64         `json:"used"`
	Parts []storagePart `json:"parts"`
}

// storagePart nennt, wie viel der Laufwerksinhalt je Bereich belegt.
type storagePart struct {
	Key  string `json:"key"`
	Size int64  `json:"size"`
}

// storageSetupBody ist der Körper von POST /api/v1/storage/setup.
type storageSetupBody struct {
	Kname      string           `json:"kname"`
	UUID       string           `json:"uuid"`
	Format     bool             `json:"format"`
	UseData    bool             `json:"use_data"`
	UseBackups bool             `json:"use_backups"`
	Fallback   storage.Fallback `json:"fallback"`
}

// storageReplaceBody ist der Körper von POST /api/v1/storage/replace. Er hat
// die Felder von storageSetupBody.
type storageReplaceBody = storageSetupBody

// storageFallbackBody ist der Körper von PUT /api/v1/storage/fallback.
type storageFallbackBody struct {
	Fallback storage.Fallback `json:"fallback"`
}

func (d *StorageDeps) handleStatus(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if _, ok := storageAccess(w, r, manager, false); !ok {
			return
		}
		d.writeStatus(w)
	}
}

// handleRecheck prüft das Laufwerk sofort und antwortet wie handleStatus.
func (d *StorageDeps) handleRecheck(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if _, ok := storageAccess(w, r, manager, true); !ok {
			return
		}
		if d.Recheck != nil {
			d.Recheck(r.Context())
		}
		d.writeStatus(w)
	}
}

// writeStatus schreibt die Antwort von GET /api/v1/storage/status.
func (d *StorageDeps) writeStatus(w http.ResponseWriter) {
	cfg, err := storage.LoadConfig(d.DataDir)
	if err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, "storage_failed", err)
		return
	}
	body := storageStatusBody{Config: cfg, Job: d.Service.Job()}
	body.Status = storageModeBody{Mode: string(storage.ModeNone)}
	if d.Status != nil {
		st := d.Status()
		body.Status.Mode = string(st.Mode)
		body.Status.Reason = st.Reason
		if !st.Since.IsZero() {
			since := st.Since
			body.Status.Since = &since
		}
	}
	body.System = d.spaceOf(d.DataDir)
	body.Drive = d.driveSpace(cfg)
	writeJSON(w, body)
}

// spaceOf liest Größe und Belegung des Dateisystems, in dem dir liegt. Ein
// Fehler ergibt Nullwerte, die Seite bleibt benutzbar.
func (d *StorageDeps) spaceOf(dir string) storageSpace {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return storageSpace{}
	}
	size := int64(st.Blocks) * int64(st.Bsize)
	free := int64(st.Bfree) * int64(st.Bsize)
	return storageSpace{Size: size, Used: size - free}
}

// driveSpace liefert die Belegung des Laufwerks, solange es eingehängt und
// eingerichtet ist. Sonst bleibt alles leer.
func (d *StorageDeps) driveSpace(cfg storage.Config) storageDriveSpace {
	out := storageDriveSpace{Parts: []storagePart{}}
	if cfg.UUID == "" || !storage.VolumeOK(d.Service.Paths.Mount, d.Service.NodeID) {
		return out
	}
	space := d.spaceOf(d.Service.Paths.Mount)
	if d.DemoSpace {
		space = d.listedSpace(cfg.UUID)
	}
	out.Size, out.Used = space.Size, space.Used
	out.Parts = append(out.Parts,
		storagePart{Key: "runtime", Size: dirSize(storage.DriveRuntimeDir(d.Service.Paths))},
		storagePart{Key: "backups", Size: dirSize(filepath.Join(d.Service.Paths.Mount, "backups"))},
	)
	return out
}

// listedSpace liefert Größe und Belegung des gelisteten Laufwerks mit der uuid.
// Fehlt es in der Liste, bleiben die Werte leer.
func (d *StorageDeps) listedSpace(uuid string) storageSpace {
	if d.Lister == nil {
		return storageSpace{}
	}
	drives, err := d.Lister.Drives(context.Background())
	if err != nil {
		return storageSpace{}
	}
	for _, drive := range drives {
		if drive.UUID == uuid {
			return storageSpace{Size: drive.Size, Used: drive.Used}
		}
	}
	return storageSpace{}
}

// dirSize summiert die Dateigrößen unter dir. Nicht lesbare Einträge zählen nicht.
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.Type().IsRegular() {
			if fi, ierr := e.Info(); ierr == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}

func (d *StorageDeps) handleDrives(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if _, ok := storageAccess(w, r, manager, false); !ok {
			return
		}
		drives := []storage.Drive{}
		if d.Lister != nil {
			found, err := d.Lister.Drives(r.Context())
			if err != nil {
				writeErrorDetail(w, http.StatusInternalServerError, "storage_drives_failed", err)
				return
			}
			drives = append(drives, found...)
		}
		writeJSON(w, drives)
	}
}

func (d *StorageDeps) handleSetup(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if _, ok := storageAccess(w, r, manager, true); !ok {
			return
		}
		var body storageSetupBody
		if err := decodeBody(r, &body); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "storage_bad_request", err)
			return
		}
		req := storage.SetupRequest{
			Kname:      body.Kname,
			UUID:       body.UUID,
			Format:     body.Format,
			UseData:    body.UseData,
			UseBackups: body.UseBackups,
			Fallback:   body.Fallback,
		}
		if err := d.Service.StartSetup(req); err != nil {
			writeDriveJobError(w, err)
			return
		}
		writeJSONStatus(w, http.StatusAccepted, d.Service.Job())
	}
}

func (d *StorageDeps) handleReplace(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if _, ok := storageAccess(w, r, manager, true); !ok {
			return
		}
		var body storageReplaceBody
		if err := decodeBody(r, &body); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "storage_bad_request", err)
			return
		}
		req := storage.ReplaceRequest{
			Kname:      body.Kname,
			UUID:       body.UUID,
			Format:     body.Format,
			UseData:    body.UseData,
			UseBackups: body.UseBackups,
			Fallback:   body.Fallback,
		}
		if err := d.Service.StartReplace(req); err != nil {
			writeDriveJobError(w, err)
			return
		}
		writeJSONStatus(w, http.StatusAccepted, d.Service.Job())
	}
}

func (d *StorageDeps) handleRemove(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if _, ok := storageAccess(w, r, manager, true); !ok {
			return
		}
		if err := d.Service.StartRemove(); err != nil {
			writeDriveJobError(w, err)
			return
		}
		writeJSONStatus(w, http.StatusAccepted, d.Service.Job())
	}
}

func (d *StorageDeps) handleFallback(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodPut)
			return
		}
		if _, ok := storageAccess(w, r, manager, true); !ok {
			return
		}
		var body storageFallbackBody
		if err := decodeBody(r, &body); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "storage_bad_request", err)
			return
		}
		if body.Fallback != storage.FallbackSystem && body.Fallback != storage.FallbackRAM {
			writeError(w, http.StatusBadRequest, "storage_fallback_invalid", "Der Ausweichort ist unbekannt")
			return
		}
		if d.Service.Busy() {
			writeError(w, http.StatusConflict, "storage_busy", "Ein Speicherauftrag läuft gerade")
			return
		}
		cfg, err := storage.LoadConfig(d.DataDir)
		if err != nil {
			writeErrorDetail(w, http.StatusInternalServerError, "storage_config_failed", err)
			return
		}
		cfg.Fallback = body.Fallback
		if err := storage.SaveConfig(d.DataDir, cfg); err != nil {
			writeErrorDetail(w, http.StatusInternalServerError, "storage_save_failed", err)
			return
		}
		// The running watcher reads its fallback at start, so the change is
		// applied with the next start of the dashboard.
		writeJSON(w, map[string]any{"config": cfg, "applies_after_restart": true})
	}
}

// writeDriveJobError antwortet auf einen abgewiesenen Auftrag mit seinem Code.
func writeDriveJobError(w http.ResponseWriter, err error) {
	code := storage.CodeOf(err)
	if code == "" {
		code = storage.CodeFailed
	}
	writeErrorDetail(w, driveJobStatus(code), code, err)
}

// driveJobStatus wählt den HTTP-Status zu einem Code des Speicherauftrags.
func driveJobStatus(code string) int {
	switch code {
	case storage.CodeBusy, storage.CodeAlreadySetUp, storage.CodeNotSetUp, storage.CodeNotOnDrive, storage.CodeDriveMissing:
		return http.StatusConflict
	case storage.CodeDrivesFailed, storage.CodeConfigFailed, storage.CodeFailed:
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}
