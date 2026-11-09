package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/backup"
	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
)

const (
	// backupPendingDirName ist der Ordner des vorgemerkten Einspielens, wie im Paket backup.
	backupPendingDirName = "backup-pending"
	// backupPreImportTrigger kennzeichnet die Sicherung vor einem Einspielen. Der Bindestrich
	// aus dem Plan ist nicht erlaubt, der Auslöser muss aus Kleinbuchstaben und _ bestehen.
	backupPreImportTrigger = "pre_import"
	backupPreImportKeep    = 3
)

// Fehlercodes, die ein Einspielauftrag als Fehlertext meldet.
var (
	errBackupNoLocation = errors.New("backup_no_location")
	errBackupNoImport   = errors.New("backup_no_import")
)

// backupApplyRequest ist der Körper von POST /api/v1/backup/import/apply.
type backupApplyRequest struct {
	Areas      map[backup.Area]bool `json:"areas"`
	UseArchive map[string]bool      `json:"use_archive"`
}

// anyTrue meldet, ob ein Schalter gesetzt ist.
func anyTrue[K comparable](m map[K]bool) bool {
	for _, on := range m {
		if on {
			return true
		}
	}
	return false
}

// handleImportApply startet das Einspielen der Importsitzung als Auftrag.
func (d *BackupDeps) handleImportApply(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		user, ok := backupWrite(w, r, manager, auth.RoleBackupImport)
		if !ok {
			return
		}
		d.cleanupStaging(d.now())
		d.importMu.Lock()
		defer d.importMu.Unlock()

		var body backupApplyRequest
		if err := decodeBody(r, &body); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "backup_bad_request", err)
			return
		}
		session := d.importSession
		if session == nil {
			writeErrorKey(w, http.StatusConflict, "backup_no_import", "error.backup_no_import", nil, "Es liegt kein Backup zum Einspielen bereit")
			return
		}
		if d.Exec == nil {
			writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", errors.New("Systemaktionen sind nicht eingerichtet"))
			return
		}
		if len(backup.ChosenAreas(body.Areas)) == 0 && !anyTrue(body.UseArchive) {
			writeErrorKey(w, http.StatusBadRequest, "backup_bad_request", "error.backup_bad_request", nil, "Die Anfrage zum Backup ist ungültig")
			return
		}

		// Zugangsdaten und Konten mit Hashes brauchen die Rolle backup_secrets.
		secrets := body.Areas[backup.AreaSecrets] || (body.Areas[backup.AreaUsers] && session.manifest.Secrets)
		if secrets && !auth.HasRole(user, auth.RoleBackupSecrets) {
			writeErrorKey(w, http.StatusForbidden, "backup_forbidden", "error.backup_forbidden", nil, "Für das Backup fehlt die Berechtigung")
			return
		}

		areas, err := backup.Preview(session.staging, d.Sources)
		if err != nil {
			writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
			return
		}
		available := make(map[backup.Area]bool, len(areas))
		for _, a := range areas {
			available[a.Area] = a.Available
		}
		for _, area := range backup.ChosenAreas(body.Areas) {
			if !available[area] {
				writeErrorKey(w, http.StatusUnprocessableEntity, "backup_area_unavailable", "error.backup_area_unavailable", nil, "Ein gewählter Bereich ist im Archiv nicht enthalten")
				return
			}
		}

		jobID, err := d.Jobs.Start("import", []string{"safety", "stage", "stop", "restart"}, func(ctx context.Context, phase func(int)) (string, error) {
			return d.runApply(ctx, phase, session, body, secrets)
		})
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

// runApply ist der Einspielauftrag. Er hält importMu über seine ganze Laufzeit, damit
// kein zweiter Import das Staging wegnimmt, während es verschoben wird.
func (d *BackupDeps) runApply(ctx context.Context, phase func(int), session *backupImportSession, body backupApplyRequest, secrets bool) (string, error) {
	d.importMu.Lock()
	defer d.importMu.Unlock()
	if d.importSession != session {
		return "", errBackupNoImport
	}

	phase(0)
	safetyID, err := d.preImportArchive(ctx, secrets)
	if err != nil {
		return "", d.abortApply(ctx, err)
	}

	phase(1)
	merged, err := d.mergedEnvironment(session, body.UseArchive)
	if err != nil {
		return "", d.abortApply(ctx, err)
	}
	pending := backup.Pending{Areas: body.Areas, ArchiveSecrets: session.manifest.Secrets, PreArchive: safetyID}
	if err := backup.WritePending(d.DataDir, session.staging, pending); err != nil {
		return "", d.abortApply(ctx, err)
	}

	// Stoppen kommt vor der Konfiguration in /etc: scheitert es, bleibt /etc unverändert.
	phase(2)
	if err := d.Exec.Execute(ctx, systemactions.StopServices); err != nil {
		return "", d.abortApply(ctx, err)
	}
	if merged != nil {
		if err := systemactions.StageAndApplyAppConfig(ctx, d.Exec, d.DataDir, merged); err != nil {
			return "", d.abortApply(ctx, fmt.Errorf("Die Konfiguration konnte nicht übernommen werden: %w", err))
		}
	}

	phase(3)
	if err := d.Exec.Execute(ctx, systemactions.RestartDashboard); err != nil {
		cause := fmt.Errorf("Der Neustart ist fehlgeschlagen: %w", err)
		if merged != nil {
			cause = fmt.Errorf("Der Neustart ist fehlgeschlagen, die Konfiguration in /etc/energy-node/config.json ist bereits geändert: %w", err)
		}
		return safetyID, d.abortApply(ctx, cause)
	}
	// Ab hier startet der Neustart das Einspielen.
	d.dropImport()
	return safetyID, nil
}

// preImportArchive legt die Sicherung des aktuellen Stands vor dem Einspielen an.
// Sie enthält alle Bereiche und Zugangsdaten nur, wenn das Einspielen sie ersetzt.
func (d *BackupDeps) preImportArchive(ctx context.Context, secrets bool) (string, error) {
	store, place, err := d.archiveStore()
	if errors.Is(err, backup.ErrNoLocation) {
		return "", errBackupNoLocation
	}
	if err != nil {
		return "", err
	}
	all := make(map[backup.Area]bool, len(backupAreaOrder))
	for _, area := range backupAreaOrder {
		all[area] = true
	}
	opts := backup.CreateOptions{Areas: all, Secrets: secrets, Trigger: backupPreImportTrigger, NodeVersion: d.Version}
	info, err := store.Add(backup.Info{
		Trigger: backupPreImportTrigger,
		Place:   place,
		Version: d.Version,
		Areas:   backup.ChosenAreas(all),
		Secrets: secrets,
	}, backupPreImportKeep, func(w io.Writer) error {
		_, err := backup.Create(ctx, d.Sources, opts, w)
		return err
	})
	if err != nil {
		return "", err
	}
	return info.ID, nil
}

// mergedEnvironment führt die gewählten Umgebungszeilen des Archivs in die Konfiguration
// dieses Geräts ein. Ohne gewählte Zeilen ist das Ergebnis nil.
func (d *BackupDeps) mergedEnvironment(session *backupImportSession, useArchive map[string]bool) ([]byte, error) {
	if !anyTrue(useArchive) {
		return nil, nil
	}
	if d.Sources.AppConfigPath == "" {
		return nil, errors.New("Konfigurationsdatei ist nicht eingerichtet")
	}
	archiveConfig, err := os.ReadFile(filepath.Join(session.staging, "config", "config.json"))
	if err != nil {
		return nil, err
	}
	hereConfig, err := os.ReadFile(d.Sources.AppConfigPath)
	if err != nil {
		return nil, err
	}
	return backup.MergeConfig(archiveConfig, hereConfig, useArchive)
}

// abortApply macht einen Einspielauftrag vor dem Neustart rückgängig: das vorgemerkte
// Einspielen fällt weg, die Dienste laufen wieder und die Sitzung ist verbraucht.
func (d *BackupDeps) abortApply(ctx context.Context, cause error) error {
	_ = os.RemoveAll(filepath.Join(d.DataDir, backupPendingDirName))
	d.dropImport()
	if err := d.Exec.Execute(ctx, systemactions.StartServices); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

// handleImportResult liefert das Ergebnis des letzten Einspielens, leer als {}.
func (d *BackupDeps) handleImportResult(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if _, ok := backupAccess(w, r, manager, auth.RoleBackupImport); !ok {
			return
		}
		result, err := backup.ReadResult(d.DataDir)
		if err != nil {
			writeErrorDetail(w, http.StatusInternalServerError, "backup_failed", err)
			return
		}
		if result == nil {
			writeJSON(w, struct{}{})
			return
		}
		writeJSON(w, result)
	}
}
