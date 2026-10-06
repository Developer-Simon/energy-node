package systemactions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
)

// StagedAppConfigName ist der Dateiname, unter dem das Dashboard das neue
// config.json in data_dir ablegt. Der root-eigene Helper-Verb apply-app-config
// (dashboard/energy-node-dashboard-system-action) liest exakt diesen Pfad,
// beide Seiten muessen uebereinstimmen.
const StagedAppConfigName = "energy-node.config.staged.json"

// ActionExecutor ist die Teilmenge von *Executor, die der Staging-Helfer braucht.
type ActionExecutor interface {
	Execute(context.Context, Action) error
}

// StageAndApplyAppConfig legt body in data_dir ab und laesst den privilegierten
// Helper es nach /etc/energy-node/config.json installieren. Ein direkter
// Schreibversuch scheitert auf Boxen, auf denen ensure_remote_config.sh
// /etc/energy-node als 0755 angelegt hat: die Dienstgruppe darf dort keine
// Datei anlegen. Der Helper sichert den alten Stand nach
// /etc/energy-node/.config.json.bak. Die Staging-Datei wird in jedem Fall entfernt.
func StageAndApplyAppConfig(ctx context.Context, exec ActionExecutor, dataDir string, body []byte) error {
	if dataDir == "" {
		return fmt.Errorf("data_dir ist leer - keine Ablage fuer die Staging-Datei")
	}
	staged := filepath.Join(dataDir, StagedAppConfigName)
	if err := config.AtomicWrite(staged, body, 0o640); err != nil {
		return fmt.Errorf("Staging-Datei %s nicht schreibbar: %w", staged, err)
	}
	defer os.Remove(staged)
	return exec.Execute(ctx, ApplyAppConfig)
}
