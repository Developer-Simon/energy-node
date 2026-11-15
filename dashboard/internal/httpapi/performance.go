package httpapi

import (
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/performance"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// performanceMinMeasure is the measuring time after which the page gives its
// verdict. The page reads it from min_measure_s.
const performanceMinMeasure = 6 * time.Hour

// PerformanceProvider gives the measurement window ending at now.
type PerformanceProvider interface {
	Snapshot(now time.Time) performance.Snapshot
}

// ChangeStore holds the applied change of the performance page. The monitor
// implements it. A nil store means no change tracking.
type ChangeStore interface {
	CurrentChange() *performance.Change
	RecordChange(performance.Change) error
	MarkUndone() error
}

// PerformanceDeps wires the performance page. Rates gives the current poll
// periods, Floors the poll floor per service ID. Without it the route answers
// 501. The fields after Floors are the write paths of the rate apply
// (POST /api/v1/performance/apply); the apply answers 501 without them.
type PerformanceDeps struct {
	Provider PerformanceProvider
	Rates    func() performance.RateConfig
	Floors   map[string]float64
	Changes  ChangeStore

	Auth       *auth.Manager
	Settings   *settings.Store
	Configs    *config.Manager
	SystemPath string
	DataDir    string
	Executor   SystemActionExecutor
	NodeRates  interface{ SetRates(float64, float64) }
}

// performanceBody is the answer of GET /api/v1/performance.
type performanceBody struct {
	MeasuredS        int                  `json:"measured_s"`
	MinMeasureS      int                  `json:"min_measure_s"`
	Cores            int                  `json:"cores"`
	TargetPct        int                  `json:"target_pct"`
	DefaultTargetPct int                  `json:"default_target_pct"`
	Deduction        performanceDeduction `json:"deduction"`
	BasePct          float64              `json:"base_pct"`
	TodayPct         float64              `json:"today_pct"`
	OverSinceS       *float64             `json:"over_since_s"`
	Clients          performanceClients   `json:"clients"`
	Device           performanceDevice    `json:"device"`
	External         *performanceExternal `json:"external"`
	Rows             []performanceRow     `json:"rows"`
	Units            []performanceUnit    `json:"units"`
	Change           *performance.Change  `json:"change"`
}

type performanceDeduction struct {
	Pct     float64  `json:"pct"`
	Reasons []string `json:"reasons"`
}

type performanceClients struct {
	Max  int  `json:"max"`
	Seen bool `json:"seen"`
}

type performanceDevice struct {
	TempC      *float64 `json:"temp_c"`
	Throttled  bool     `json:"throttled"`
	RAMFreePct *float64 `json:"ram_free_pct"` // null while no RAM sample exists
	SwapPagesS float64  `json:"swap_pages_s"`
}

type performanceExternal struct {
	Name   string  `json:"name"`
	EveryS float64 `json:"every_s"`
}

type performanceRow struct {
	ID       string    `json:"id"`
	Current  float64   `json:"current"`
	Steps    []float64 `json:"steps"`
	Floor    float64   `json:"floor"`
	RoundS   float64   `json:"round_s"`
	CostPct  float64   `json:"cost_pct"`
	Right    string    `json:"right"`
	Editable bool      `json:"editable"`
	Follows  string    `json:"follows"`
	Source   bool      `json:"source"`
	Balance  bool      `json:"balance"`
}

type performanceUnit struct {
	ID        string   `json:"id"`
	CPUPct    float64  `json:"cpu_pct"`
	RAMBytes  int64    `json:"ram_bytes"`
	RoundS    *float64 `json:"round_s"`
	MsgPerMin float64  `json:"msg_per_min"`
	Fixed     bool     `json:"fixed"`
}

// handlePerformance serves GET /api/v1/performance. The budget comes from the
// settings (0 is the default for the core count). A row is editable when the
// session holds its right; without a session every row is editable, as the
// other settings gates do.
func handlePerformance(store *settings.Store, deps *PerformanceDeps, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if deps == nil || deps.Provider == nil || deps.Rates == nil {
			writeError(w, http.StatusNotImplemented, "performance_unavailable", "Die Leistungsmessung ist nicht verfügbar")
			return
		}
		snap := deps.Provider.Snapshot(now())
		var ramFree *float64
		if snap.RAMFreeKnown {
			value := snap.RAMFreePct
			ramFree = &value
		}
		defaultTarget := performance.DefaultTarget(snap.Cores)
		target := performanceTarget(store, snap, nil)

		rows, base := performanceRows(snap, deps)
		user, hasUser := auth.UserFromContext(r.Context())

		body := performanceBody{
			MeasuredS:        int(snap.MeasuredFor.Seconds()),
			MinMeasureS:      int(performanceMinMeasure.Seconds()),
			Cores:            snap.Cores,
			TargetPct:        target,
			DefaultTargetPct: defaultTarget,
			Deduction:        deductionBody(performance.DeductionFor(snap)),
			TodayPct:         snap.CPUP95,
			Clients:          performanceClients{Max: snap.ClientsMax, Seen: snap.ClientsSeen},
			Device: performanceDevice{
				TempC:      snap.TempC,
				Throttled:  snap.Throttled,
				RAMFreePct: ramFree,
				SwapPagesS: snap.SwapPagesPerS,
			},
			BasePct: base,
			Rows:    []performanceRow{},
			Units:   []performanceUnit{},
		}
		if deps.Changes != nil {
			body.Change = deps.Changes.CurrentChange()
		}

		for _, row := range rows {
			editable := true
			if hasUser {
				editable = auth.HasRole(user, row.Right)
			}
			body.Rows = append(body.Rows, performanceRow{
				ID:       string(row.ID),
				Current:  row.Current,
				Steps:    row.Steps,
				Floor:    row.Floor,
				RoundS:   row.RoundS,
				CostPct:  row.CostPct,
				Right:    row.Right,
				Editable: editable,
				Follows:  row.Follows,
				Source:   row.Source,
				Balance:  row.Balance,
			})
		}
		if snap.External != nil {
			body.External = &performanceExternal{Name: snap.External.Name, EveryS: snap.External.EveryS}
		}
		for _, u := range snap.Units {
			unit := performanceUnit{
				ID:        u.ID,
				CPUPct:    u.CPUP95,
				RoundS:    u.RoundS,
				MsgPerMin: u.MsgPerMin,
				Fixed:     u.Fixed,
			}
			if u.HasRAM {
				unit.RAMBytes = u.RAMBytes
			}
			body.Units = append(body.Units, unit)
		}
		writeJSON(w, body)
	}
}

// performanceTarget is the budget in percent the page works with: the request
// target when given, else the saved target, else the default for the core count.
func performanceTarget(store *settings.Store, snap performance.Snapshot, requested *int) int {
	if requested != nil {
		return *requested
	}
	target := performance.DefaultTarget(snap.Cores)
	if store != nil {
		if value, err := store.LoadSettings(); err == nil && value.PerformanceTargetPct > 0 {
			target = value.PerformanceTargetPct
		}
	}
	return target
}

// performanceRows builds the rows the page shows and the base load they leave
// over. GET and the apply share it, so both see the same rows. Only present
// rows are returned.
func performanceRows(snap performance.Snapshot, deps *PerformanceDeps) ([]performance.Row, float64) {
	present := map[performance.RowID]bool{performance.RowRegulator: true}
	all := performance.BuildRows(snap, deps.Rates(), deps.Floors, present)
	rows := make([]performance.Row, 0, len(all))
	var costs float64
	for _, row := range all {
		if !row.Present {
			continue
		}
		costs += row.CostPct
		rows = append(rows, row)
	}
	base := snap.CPUP95 - costs
	if base < 0 {
		base = 0
	}
	return rows, base
}

// performanceRightRole maps a row's write right to the role that grants it.
func performanceRightRole(right string) string {
	switch right {
	case "edit_setup":
		return auth.RoleEditSetup
	case "automations":
		return auth.RoleAutomations
	case "system_actions":
		return auth.RoleSystemActions
	}
	return right
}

// handlePerformanceApply serves POST /api/v1/performance/apply. Like the system
// config PUT it needs HTTPS and the CSRF token, and a login. The rights of the
// rows are checked per row inside the writer.
func handlePerformanceApply(deps *PerformanceDeps, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if deps == nil || deps.Provider == nil || deps.Rates == nil || deps.Settings == nil || deps.Configs == nil {
			writeError(w, http.StatusNotImplemented, "performance_unavailable", "Die Leistungsmessung ist nicht verfügbar")
			return
		}
		if deps.Auth == nil {
			writeError(w, http.StatusNotImplemented, "authentication_unavailable", "Anmeldung ist nicht konfiguriert")
			return
		}
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
			return
		}
		if !requireHTTPS(w, r) || !requireCSRF(w, r, deps.Auth) {
			return
		}
		var req ApplyRequest
		if err := decodeBody(r, &req); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
			return
		}

		snap := deps.Provider.Snapshot(now())
		rows, base := performanceRows(snap, deps)
		writer := &rateWriter{
			Settings:   deps.Settings,
			Configs:    deps.Configs,
			SystemPath: deps.SystemPath,
			DataDir:    deps.DataDir,
			Executor:   deps.Executor,
			NodeRates:  deps.NodeRates,
		}
		resp, err := writer.Apply(r.Context(), req, rows, func(right string) bool {
			return auth.HasRole(user, performanceRightRole(right))
		})
		if err != nil {
			var saveErr *saveError
			if errors.As(err, &saveErr) {
				writeErrorDetail(w, saveErr.Status, saveErr.Code, saveErr.Err)
				return
			}
			writeErrorDetail(w, http.StatusInternalServerError, "apply_failed", err)
			return
		}

		selection := make(map[performance.RowID]float64, len(req.Rates))
		for id, value := range req.Rates {
			if id == performance.RowLiveUpdate {
				value = math.Round(value)
			}
			selection[id] = value
		}
		resp.ExpectedPct = performance.Estimate(base, rows, selection)
		resp.Change = recordAppliedChange(deps, now(), snap, req.TargetPct, rows, selection, resp.ExpectedPct)
		writeJSON(w, resp)
	}
}

// recordAppliedChange keeps the rows whose period the apply changed, with their
// values before and after, and the budget the page checks against. It returns
// nil and records nothing when no row changed. A failing save does not fail the
// apply, the write has already happened.
func recordAppliedChange(deps *PerformanceDeps, at time.Time, snap performance.Snapshot, requested *int, rows []performance.Row, selection map[performance.RowID]float64, expected float64) *performance.Change {
	change := performance.Change{
		At:       at,
		Before:   map[performance.RowID]float64{},
		After:    map[performance.RowID]float64{},
		Expected: expected,
		State:    "running",
	}
	for _, row := range rows {
		value, ok := selection[row.ID]
		if !ok || value == row.Current {
			continue
		}
		change.Before[row.ID] = row.Current
		change.After[row.ID] = value
	}
	if len(change.After) == 0 {
		return nil
	}
	change.Available = float64(performanceTarget(deps.Settings, snap, requested)) - performance.DeductionFor(snap).Pct
	if deps.Changes != nil {
		_ = deps.Changes.RecordChange(change)
	}
	return &change
}

func deductionBody(d performance.Deduction) performanceDeduction {
	reasons := d.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	return performanceDeduction{Pct: d.Pct, Reasons: reasons}
}
