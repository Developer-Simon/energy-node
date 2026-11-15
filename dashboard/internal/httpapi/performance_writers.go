package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/performance"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
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

// applyMaxS ist die längste Abfrageperiode, die eine Übernahme annimmt.
const applyMaxS = 3600

// applyTargetMinPct und applyTargetMaxPct begrenzen das Ziel in Prozent.
const (
	applyTargetMinPct = 20
	applyTargetMaxPct = 95
)

// applyServiceRows listet die Zeilen, deren Periode in
// services.<id>.poll_interval_s steht.
var applyServiceRows = []performance.RowID{
	performance.RowShelly,
	performance.RowTrucki,
	performance.RowBatterySoc,
	performance.RowApsystems,
	performance.RowTuya,
}

// ApplyRequest ist der Körper der Übernahme: das Ziel in Prozent und die
// gewünschten Abfrageperioden je Zeile.
type ApplyRequest struct {
	TargetPct *int                          `json:"target_pct"`
	Rates     map[performance.RowID]float64 `json:"rates"`
}

// ApplyResponse ist die Antwort der Übernahme. Change bleibt in dieser Stufe
// null, der Änderungsvorschlag kommt mit der HTTP-Schicht.
type ApplyResponse struct {
	ExpectedPct float64            `json:"expected_pct"`
	Change      *performanceChange `json:"change"`
}

// rateWriter schreibt eine Übernahme über die bestehenden Schreibpfade. Jede
// Abhängigkeit ist ein Feld, damit Tests sie ersetzen können.
type rateWriter struct {
	Settings   *settings.Store
	Configs    *config.Manager
	SystemPath string
	DataDir    string
	Reloader   ConfigReloader
	Executor   SystemActionExecutor
	NodeRates  interface{ SetRates(float64, float64) }
}

// Apply prüft alle Zeilen, bevor etwas geschrieben wird, und schreibt dann in
// der Reihenfolge settings.json, Automationsregeln, Systemkonfiguration. Schlägt
// ein Schritt fehl, werden die vorherigen Schritte mit ihrem alten Inhalt
// zurückgeschrieben.
func (w *rateWriter) Apply(ctx context.Context, req ApplyRequest, rows []performance.Row, has func(right string) bool) (ApplyResponse, error) {
	byID := make(map[performance.RowID]performance.Row, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}

	// Das Ziel braucht das Recht edit_setup und liegt zwischen 20 und 95.
	if req.TargetPct != nil {
		if !has("edit_setup") {
			return ApplyResponse{}, &saveError{Status: http.StatusForbidden, Code: "forbidden", Err: fmt.Errorf("Ziel: Recht %q fehlt", "edit_setup")}
		}
		if *req.TargetPct < applyTargetMinPct || *req.TargetPct > applyTargetMaxPct {
			return ApplyResponse{}, &saveError{Status: http.StatusBadRequest, Code: "invalid_target", Err: fmt.Errorf("Ziel %d liegt nicht zwischen %d und %d", *req.TargetPct, applyTargetMinPct, applyTargetMaxPct)}
		}
	}

	changes := map[performance.RowID]float64{}
	for id, value := range req.Rates {
		row, ok := byID[id]
		if !ok {
			return ApplyResponse{}, &saveError{Status: http.StatusBadRequest, Code: "unknown_row", Err: fmt.Errorf("Zeile %q gibt es nicht", id)}
		}
		if row.Right != "" && !has(row.Right) {
			return ApplyResponse{}, &saveError{Status: http.StatusForbidden, Code: "forbidden", Err: fmt.Errorf("Zeile %q: Recht %q fehlt", id, row.Right)}
		}
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return ApplyResponse{}, &saveError{Status: http.StatusBadRequest, Code: "below_floor", Err: fmt.Errorf("Zeile %q: Wert ist keine endliche Zahl", id)}
		}
		// live_update wird als ganze Zahl gespeichert, also vorab runden.
		if id == performance.RowLiveUpdate {
			value = math.Round(value)
		}
		// Jede Periode ist mindestens 1 Sekunde, unabhängig von den weichen Untergrenzen.
		if value < 1 {
			return ApplyResponse{}, &saveError{Status: http.StatusBadRequest, Code: "below_floor", Err: fmt.Errorf("Zeile %q: %v ist unter 1 Sekunde", id, value)}
		}
		// Die Untergrenzen der Quellen (half, balance) sind weich.
		soft := row.Follows == "half" || row.Follows == "balance"
		if !soft && value < row.Floor {
			return ApplyResponse{}, &saveError{Status: http.StatusBadRequest, Code: "below_floor", Err: fmt.Errorf("Zeile %q: %v unter der Untergrenze %v", id, value, row.Floor)}
		}
		if value > applyMaxS {
			return ApplyResponse{}, &saveError{Status: http.StatusBadRequest, Code: "too_slow", Err: fmt.Errorf("Zeile %q: %v über %d", id, value, applyMaxS)}
		}
		if value == row.Current {
			continue
		}
		changes[id] = value
	}

	var undo []func()
	rollback := func(err error) (ApplyResponse, error) {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		return ApplyResponse{}, applyFailure(err)
	}

	// Schritt 1: settings.json (Live-Aktualisierung und Ziel).
	liveNew, liveChanged := changes[performance.RowLiveUpdate]
	if liveChanged || req.TargetPct != nil {
		old, err := w.Settings.LoadSettings()
		if err != nil {
			return rollback(err)
		}
		next := old
		if liveChanged {
			next.LiveUpdateIntervalSeconds = int(liveNew)
		}
		if req.TargetPct != nil {
			next.PerformanceTargetPct = *req.TargetPct
		}
		if err := w.Settings.SaveSettings(next); err != nil {
			return rollback(err)
		}
		undo = append(undo, func() { _ = w.Settings.SaveSettings(old) })
	}

	// Schritt 2: Automationsregeln (Takt von Regeln und Regler).
	tickNew, tickChanged := changes[performance.RowTick]
	regNew, regChanged := changes[performance.RowRegulator]
	if tickChanged || regChanged {
		oldRaw, err := w.Configs.Read("automation_rules")
		if err != nil {
			return rollback(err)
		}
		doc := map[string]any{}
		if err := json.Unmarshal(oldRaw, &doc); err != nil {
			return rollback(err)
		}
		section, _ := doc["settings"].(map[string]any)
		if section == nil {
			section = map[string]any{}
			doc["settings"] = section
		}
		if tickChanged {
			section["tick_interval_s"] = tickNew
		}
		if regChanged {
			section["regulator_interval_s"] = regNew
		}
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return rollback(err)
		}
		if _, err := saveConfiguration(w.Configs, "automation_rules", data); err != nil {
			return rollback(err)
		}
		undo = append(undo, func() { _, _ = saveConfiguration(w.Configs, "automation_rules", oldRaw) })
	}

	// Schritt 3: config.json (Knotenabfrage und Dienste).
	nodeNew, nodeChanged := changes[performance.RowNode]
	serviceNew := map[performance.RowID]float64{}
	for _, id := range applyServiceRows {
		if value, ok := changes[id]; ok {
			serviceNew[id] = value
		}
	}
	if nodeChanged || len(serviceNew) > 0 {
		oldRaw, err := os.ReadFile(w.SystemPath)
		if err != nil {
			return rollback(err)
		}
		doc := map[string]any{}
		if err := json.Unmarshal(oldRaw, &doc); err != nil {
			return rollback(err)
		}
		dashboard, _ := doc["dashboard"].(map[string]any)
		if dashboard == nil {
			dashboard = map[string]any{}
			doc["dashboard"] = dashboard
		}
		if nodeChanged {
			dashboard["node_poll_interval_s"] = nodeNew
		}
		services, _ := doc["services"].(map[string]any)
		if services == nil && len(serviceNew) > 0 {
			services = map[string]any{}
			doc["services"] = services
		}
		for id, value := range serviceNew {
			service, _ := services[string(id)].(map[string]any)
			if service == nil {
				service = map[string]any{}
				services[string(id)] = service
			}
			service["poll_interval_s"] = value
		}
		multiplier := 10.0
		if value, ok := dashboard["node_diagnostic_poll_multiplier"].(float64); ok {
			multiplier = value
		}
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return rollback(err)
		}
		deps := systemConfigDeps{Path: w.SystemPath, DataDir: w.DataDir, Reloader: w.Reloader, Executor: w.Executor}
		if _, err := saveSystemConfig(ctx, deps, append(data, '\n')); err != nil {
			return rollback(err)
		}
		undo = append(undo, func() { _, _ = saveSystemConfig(ctx, deps, oldRaw) })
		if nodeChanged && w.NodeRates != nil {
			w.NodeRates.SetRates(nodeNew, multiplier)
		}
	}

	return ApplyResponse{}, nil
}

// applyFailure gibt einen Schreibfehler unverändert weiter, wenn er schon ein
// *saveError ist, sonst verpackt als apply_failed.
func applyFailure(err error) error {
	var saveErr *saveError
	if errors.As(err, &saveErr) {
		return err
	}
	return &saveError{Status: http.StatusInternalServerError, Code: "apply_failed", Err: err}
}
