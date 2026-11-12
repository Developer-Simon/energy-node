package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
	"github.com/Developer-Simon/energy-node-dashboard/internal/storage"
	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
)

// storageRuntimeDir ist der Ort der laufenden Daten, den Einrichten als
// paths.runtime_dir setzt, solange die Konfiguration noch keinen trägt.
func storageRuntimeDir(cfg appconfig.Config, dataDir string) string {
	if cfg.Paths.RuntimeDir != "" {
		return cfg.Paths.RuntimeDir
	}
	return filepath.Join(dataDir, "runtime")
}

// newStorageService baut den Dienst für Einrichten und Entfernen. Die
// Systemaktionen laufen über denselben Executor wie der Rest des Dashboards.
func newStorageService(cfg appconfig.Config, dataDir, configPath string, exec *systemactions.Executor, lister storage.Lister) *storage.Service {
	nodeID, err := storage.MachineID()
	if err != nil {
		log.Printf("energy-node-dashboard: storage: %v, drive jobs will refuse the volume", err)
	}
	return &storage.Service{
		Paths: storage.Paths{
			Runtime:        storageRuntimeDir(cfg, dataDir),
			Mount:          storageMountPoint,
			SystemFallback: filepath.Join(dataDir, "runtime-fallback"),
			RAMFallback:    storageRAMFallback,
		},
		DataDir: dataDir,
		NodeID:  nodeID,
		Legacy:  storage.Legacy{DevicesDir: cfg.Paths.DevicesDir},
		Deps: storage.Deps{
			Lister:        lister,
			Storage:       exec.Storage,
			StopServices:  exec.StopServices,
			StartServices: exec.StartServices,
			RestartDashboard: func(ctx context.Context) error {
				return exec.Execute(ctx, systemactions.RestartDashboard)
			},
			ReadRuntimeDir: func() string { return cfg.Paths.RuntimeDir },
			ApplyRuntimeDir: func(ctx context.Context, dir string) error {
				return applyRuntimeDir(ctx, exec, configPath, dataDir, dir)
			},
		},
	}
}

// applyRuntimeDir schreibt paths.runtime_dir in die Konfiguration. Es liest
// die aktuelle Datei, ändert nur diesen Schlüssel und lässt den Helfer sie
// einspielen. Ein leerer Wert entfernt den Schlüssel.
func applyRuntimeDir(ctx context.Context, exec systemactions.ActionExecutor, configPath, dataDir, dir string) error {
	doc := map[string]any{}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	paths, _ := doc["paths"].(map[string]any)
	if paths == nil {
		paths = map[string]any{}
	}
	if dir == "" {
		delete(paths, "runtime_dir")
	} else {
		paths["runtime_dir"] = dir
	}
	doc["paths"] = paths
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return systemactions.StageAndApplyAppConfig(ctx, exec, dataDir, append(body, '\n'))
}

// storageBackupDrive liefert den Backup-Ordner auf dem Laufwerk, solange das
// Laufwerk eingehängt ist, zu diesem Knoten gehört und Backups darauf liegen
// sollen. Die Auswahl wird bei jedem Aufruf neu gelesen.
func storageBackupDrive(dataDir string) func() (string, bool) {
	return func() (string, bool) {
		cfg, err := storage.LoadConfig(dataDir)
		if err != nil || cfg.UUID == "" || !cfg.UseBackups {
			return "", false
		}
		nodeID, err := storage.MachineID()
		if err != nil || !storage.VolumeOK(storageMountPoint, nodeID) {
			return "", false
		}
		return filepath.Join(storageMountPoint, "backups"), true
	}
}

// resumeStorage führt den Folgeschritt eines Laufwerksauftrags aus, sobald der
// Zustand grün ist. Ohne ausstehenden Auftrag tut es nichts.
func resumeStorage(ctx context.Context, svc *storage.Service) {
	ran, err := svc.Resume(ctx)
	if err != nil {
		log.Printf("energy-node-dashboard: storage follow-up: %v", err)
		return
	}
	if ran {
		log.Printf("energy-node-dashboard: storage follow-up done")
	}
}

// runStorageResume prüft alle 30 Sekunden, ob ein Auftrag auf seinen
// Folgeschritt wartet, bis das Laufwerk da ist.
func runStorageResume(ctx context.Context, svc *storage.Service) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		resumeStorage(ctx, svc)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
