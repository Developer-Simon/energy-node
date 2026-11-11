package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
	"github.com/Developer-Simon/energy-node-dashboard/internal/backup"
)

const backupCLIUsage = "Verwendung: energy-node-dashboard backup create --config PFAD --trigger update\n"

// runBackupCLI handles: backup create --config PATH --trigger update
// Exit codes: 0 archive written, 3 skipped (settings say no: trigger update and BeforeUpdate false, or no location), 1 error, 2 usage error.
func runBackupCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "create" {
		fmt.Fprint(stderr, backupCLIUsage)
		return 2
	}
	flags := flag.NewFlagSet("backup create", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", appconfig.DefaultPath, "Pfad zur zentralen Konfigurationsdatei")
	trigger := flags.String("trigger", "", "Auslöser des Backups, erlaubt: update")
	if err := flags.Parse(args[1:]); err != nil {
		fmt.Fprint(stderr, backupCLIUsage)
		return 2
	}
	if flags.NArg() > 0 || *trigger != "update" {
		fmt.Fprint(stderr, backupCLIUsage)
		return 2
	}

	cfg, err := appconfig.Load(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "backup: %v\n", err)
		return 1
	}
	settings, err := backup.LoadSettings(cfg.Paths.DataDir)
	if err != nil {
		fmt.Fprintf(stderr, "backup: Einstellungen: %v\n", err)
		return 1
	}
	if !settings.BeforeUpdate {
		fmt.Fprintln(stdout, "skipped: Backup vor dem Update ist ausgeschaltet")
		return 3
	}
	location := backup.SystemLocation{
		DataDir:  cfg.Paths.DataDir,
		Settings: func() backup.Settings { return settings },
	}
	dir, place, err := location.ArchiveDir()
	if errors.Is(err, backup.ErrNoLocation) {
		fmt.Fprintln(stdout, "no location")
		return 3
	}
	if err != nil {
		fmt.Fprintf(stderr, "backup: Ablage: %v\n", err)
		return 1
	}
	store, err := backup.NewStore(dir)
	if err != nil {
		fmt.Fprintf(stderr, "backup: Ablage: %v\n", err)
		return 1
	}
	creator := newArchiveCreator(buildSources(cfg, *configPath), buildVersion, *trigger)
	info, err := store.Add(backup.Info{
		Trigger: *trigger,
		Place:   place,
		Version: buildVersion,
		Areas:   backup.ChosenAreas(settings.Content),
		Secrets: settings.Secrets,
	}, settings.Keep, func(w io.Writer) error {
		return creator(context.Background(), settings, w)
	})
	if err != nil {
		fmt.Fprintf(stderr, "backup: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, info.ID)
	return 0
}

// buildSources nennt die Dateien, die ein Backup aus der Konfiguration liest.
func buildSources(cfg appconfig.Config, configPath string) backup.Sources {
	return backup.Sources{
		DataDir:       cfg.Paths.DataDir,
		DevicesDir:    cfg.Paths.DevicesDir,
		AppConfigPath: configPath,
		BatteryStates: batteryStateFiles(cfg.Paths.DevicesDir),
	}
}

// newArchiveCreator liefert den Schreiber für Archive mit dem gegebenen
// Auslöser. Passwort bleibt leer, automatische und Update-Archive sind
// unverschlüsselt.
func newArchiveCreator(sources backup.Sources, version, trigger string) func(ctx context.Context, s backup.Settings, w io.Writer) error {
	return func(ctx context.Context, s backup.Settings, w io.Writer) error {
		_, err := backup.Create(ctx, sources, backup.CreateOptions{
			Areas:       s.Content,
			Secrets:     s.Secrets,
			Trigger:     trigger,
			NodeVersion: version,
		}, w)
		return err
	}
}
