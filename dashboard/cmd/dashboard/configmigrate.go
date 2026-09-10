package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
)

// stagedAppConfigName ist der Dateiname, unter dem das Dashboard das nach
// v2 migrierte config.json in data_dir ablegt. Der root-eigene Helper-Verb
// apply-app-config (dashboard/energy-node-dashboard-system-action) liest
// exakt diesen Pfad - beide Seiten muessen uebereinstimmen.
const stagedAppConfigName = "energy-node.config.staged.json"

// persistV1Migration schreibt das nach v2 migrierte config.json ueber den
// privilegierten System-Action-Helper zurueck. Ein direkter Schreibversuch
// scheitert: ensure_remote_config.sh legt /etc/energy-node als 0755 an, die
// Dienstgruppe darf im Verzeichnis keine Datei anlegen. Der Helper-Verb
// apply-app-config sichert den alten Stand nach
// /etc/energy-node/.config.json.bak und installiert die Staging-Datei.
func persistV1Migration(ctx context.Context, exec *systemactions.Executor, dataDir string, migrated []byte) error {
	if dataDir == "" {
		return fmt.Errorf("data_dir ist leer - keine Ablage fuer die Staging-Datei")
	}
	staged := filepath.Join(dataDir, stagedAppConfigName)
	if err := config.AtomicWrite(staged, migrated, 0o640); err != nil {
		return fmt.Errorf("Staging-Datei %s nicht schreibbar: %w", staged, err)
	}
	defer os.Remove(staged)
	return exec.Execute(ctx, systemactions.ApplyAppConfig)
}
