package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
)

// saveError trägt den HTTP-Status und den Fehlercode, mit denen ein
// Schreibpfad die Anfrage ablehnt.
type saveError struct {
	Status int
	Code   string
	Err    error
}

func (e *saveError) Error() string {
	return fmt.Sprintf("%s: %v", e.Code, e.Err)
}

func (e *saveError) Unwrap() error {
	return e.Err
}

type systemConfigResult struct {
	Restart  []string
	Reloaded map[string]string
}

type systemConfigDeps struct {
	Path     string
	DataDir  string
	Reloader ConfigReloader
	Executor SystemActionExecutor
}

// saveSystemConfig validates and writes a new config.json. The error is a *saveError.
func saveSystemConfig(ctx context.Context, deps systemConfigDeps, body []byte) (systemConfigResult, error) {
	if err := config.ValidateDocument(body, appconfig.Schema()); err != nil {
		return systemConfigResult{}, &saveError{Status: http.StatusBadRequest, Code: "schema_violation", Err: err}
	}
	if err := appconfig.ValidateSecretPaths(body); err != nil {
		return systemConfigResult{}, &saveError{Status: http.StatusBadRequest, Code: "path_not_allowed", Err: err}
	}
	previous, _ := os.ReadFile(deps.Path)
	if err := writeSystemConfigRevision(deps.DataDir, previous); err != nil {
		return systemConfigResult{}, &saveError{Status: http.StatusInternalServerError, Code: "revision_failed", Err: err}
	}
	if err := writeSystemConfig(ctx, deps.Path, deps.DataDir, body, deps.Executor); err != nil {
		return systemConfigResult{}, &saveError{Status: http.StatusInternalServerError, Code: "config_not_writable", Err: err}
	}
	restart := changedRestartFields(previous, body)
	reloaded := map[string]string{}
	if deps.Reloader != nil && len(restart) == 0 {
		for _, deviceID := range serviceDeviceIDs(body) {
			if err := deps.Reloader.ReloadService(deviceID); err != nil {
				reloaded[deviceID] = err.Error()
			} else {
				reloaded[deviceID] = "ok"
			}
		}
	}
	sort.Strings(restart)
	return systemConfigResult{Restart: restart, Reloaded: reloaded}, nil
}
