package main

import (
	"context"

	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
)

// persistV1Migration schreibt das nach v2 migrierte config.json ueber den
// privilegierten System-Action-Helper zurueck (siehe
// systemactions.StageAndApplyAppConfig). Ein direkter Schreibversuch scheitert
// auf einer 0755-Box, der Helper sichert den alten Stand nach
// /etc/energy-node/.config.json.bak.
func persistV1Migration(ctx context.Context, exec *systemactions.Executor, dataDir string, migrated []byte) error {
	return systemactions.StageAndApplyAppConfig(ctx, exec, dataDir, migrated)
}
