package httpapi

import (
	"errors"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
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

	// Automation gives the active rules and scenarios for the breakdown and the
	// regulator row. Nil means no entries, the regulator row stays as it was.
	Automation func() []performance.AutomationEntry

	// Learning gives the compute load of the learning engine and the learning
	// state for the block learning. Nil answers the block as null.
	Learning *PerformanceLearning
}

// PerformanceLearning wires the learning block of the performance page. Both
// fields are functions, so httpapi does not depend on the engine itself.
type PerformanceLearning struct {
	Load  func() autolearn.Stats
	State func() learnrec.State
}

// performanceBreakdownLimit is the number of items shown per automation group.
// The rest is summed into more and more_pct.
const performanceBreakdownLimit = 6

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
	Breakdown        performanceBreakdown `json:"breakdown"`
	Learning         *performanceLearning `json:"learning"`
}

// performanceLearningTop is the number of detectors listed in the learning block.
const performanceLearningTop = 3

// performanceLearning is the learning block of GET /api/v1/performance. Suggest
// is left out when there is no suggestion to move up a level.
type performanceLearning struct {
	Setting  string                     `json:"setting"`
	Level    string                     `json:"level"`
	Share    float64                    `json:"share"`
	Target   float64                    `json:"target"`
	PerRunMs int64                      `json:"per_run_ms"`
	Top      []performanceLearningEntry `json:"top"`
	Suggest  string                     `json:"suggest,omitempty"`
}

type performanceLearningEntry struct {
	Detector string `json:"detector"`
	Ms       int64  `json:"ms"`
}

// performanceBreakdown splits the load into the parts of the dashboard and the
// automation entries. A part is null when it has nothing to show.
type performanceBreakdown struct {
	Dashboard  *performanceDashboardBreakdown  `json:"dashboard"`
	Automation *performanceAutomationBreakdown `json:"automation"`
}

type performanceDashboardBreakdown struct {
	Parts []performancePart `json:"parts"`
}

type performancePart struct {
	ID      string  `json:"id"`
	Pct     float64 `json:"pct"`
	Clients *int    `json:"clients,omitempty"` // only the push part, the highest client count
}

type performanceAutomationBreakdown struct {
	Groups []performanceGroup `json:"groups"`
}

type performanceGroup struct {
	Row     string            `json:"row"`
	Pct     float64           `json:"pct"`
	More    int               `json:"more"`
	MorePct float64           `json:"more_pct"`
	Items   []performanceItem `json:"items"`
}

type performanceItem struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Kind string  `json:"kind"`
	Pct  float64 `json:"pct"`
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
		var entries []performance.AutomationEntry
		if deps.Automation != nil {
			entries = deps.Automation()
		}

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
			BasePct:   base,
			Rows:      []performanceRow{},
			Units:     []performanceUnit{},
			Breakdown: performanceBreakdownBody(snap, rows, entries),
		}
		if deps.Changes != nil {
			body.Change = deps.Changes.CurrentChange()
		}
		body.Learning = performanceLearningBody(store, deps, snap, now())

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

// PerformanceTarget is the saved budget in percent, or the default for the core
// count of snap. The over-budget check uses the same value as the page.
func PerformanceTarget(store *settings.Store, snap performance.Snapshot) int {
	return performanceTarget(store, snap, nil)
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

// performanceLearningBody builds the learning block from the engine load, the
// learning setting and the learning state. It is nil without the learning deps.
// The share is the 24 hour share of the engine, the target follows the resolved
// level, and per_run_ms is the mean compute time of one round in the window.
func performanceLearningBody(store *settings.Store, deps *PerformanceDeps, snap performance.Snapshot, now time.Time) *performanceLearning {
	if deps == nil || deps.Learning == nil || deps.Learning.Load == nil {
		return nil
	}
	setting := settings.LearningLevelAuto
	if store != nil {
		if value, err := store.LoadSettings(); err == nil && value.LearningLevel != "" {
			setting = value.LearningLevel
		}
	}
	var state learnrec.State
	if deps.Learning.State != nil {
		state = deps.Learning.State()
	}
	level := learnrec.ResolveLevel(setting, snap.Cores, state.AutoLevel)
	stats := deps.Learning.Load()

	body := &performanceLearning{
		Setting: setting,
		Level:   string(level),
		Share:   stats.Share,
		Target:  learnrec.TargetShare(level),
		Top:     []performanceLearningEntry{},
	}
	var total time.Duration
	entries := make([]performanceLearningEntry, 0, len(stats.Took))
	for name, d := range stats.Took {
		total += d
		entries = append(entries, performanceLearningEntry{Detector: name, Ms: d.Milliseconds()})
	}
	if stats.Rounds > 0 {
		body.PerRunMs = total.Milliseconds() / int64(stats.Rounds)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Ms != entries[j].Ms {
			return entries[i].Ms > entries[j].Ms
		}
		return entries[i].Detector < entries[j].Detector
	})
	if len(entries) > performanceLearningTop {
		entries = entries[:performanceLearningTop]
	}
	body.Top = append(body.Top, entries...)
	if next, ok := learnrec.SuggestUp(state, level, now); ok {
		body.Suggest = string(next)
	}
	return body
}

// performanceBreakdownBody builds the breakdown of GET. The dashboard parts
// need a split in the window, the automation groups need entries for the rows.
func performanceBreakdownBody(snap performance.Snapshot, rows []performance.Row, entries []performance.AutomationEntry) performanceBreakdown {
	var out performanceBreakdown
	if parts := performance.DashboardBreakdown(snap); parts != nil {
		dashboard := &performanceDashboardBreakdown{Parts: make([]performancePart, 0, len(parts))}
		for _, p := range parts {
			part := performancePart{ID: p.ID, Pct: p.Pct}
			if p.ID == string(performance.PartPush) {
				clients := snap.ClientsMax
				part.Clients = &clients
			}
			dashboard.Parts = append(dashboard.Parts, part)
		}
		out.Dashboard = dashboard
	}
	groups := performance.AutomationBreakdown(rows, entries, performanceBreakdownLimit)
	if len(groups) > 0 {
		automation := &performanceAutomationBreakdown{Groups: make([]performanceGroup, 0, len(groups))}
		for _, g := range groups {
			group := performanceGroup{
				Row:     string(g.Row),
				Pct:     g.Pct,
				More:    g.More,
				MorePct: g.MorePct,
				Items:   make([]performanceItem, 0, len(g.Items)),
			}
			for _, it := range g.Items {
				group.Items = append(group.Items, performanceItem{ID: it.ID, Name: it.Name, Kind: it.Kind, Pct: it.Pct})
			}
			automation.Groups = append(automation.Groups, group)
		}
		out.Automation = automation
	}
	return out
}

// performanceRows builds the rows the page shows and the base load they leave
// over. GET and the apply share it, so both see the same rows. Only present
// rows are returned. The regulator row is present when an automation entry
// has a regulator, or always when the deps give no automation entries.
func performanceRows(snap performance.Snapshot, deps *PerformanceDeps) ([]performance.Row, float64) {
	present := map[performance.RowID]bool{performance.RowRegulator: true}
	if deps.Automation != nil {
		present[performance.RowRegulator] = performance.HasRegulator(deps.Automation())
	}
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
			writePerformanceWriteError(w, err)
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

// writePerformanceWriteError answers a failed rate write with the status and code
// the writer chose, or apply_failed for any other error.
func writePerformanceWriteError(w http.ResponseWriter, err error) {
	var saveErr *saveError
	if errors.As(err, &saveErr) {
		writeErrorDetail(w, saveErr.Status, saveErr.Code, saveErr.Err)
		return
	}
	writeErrorDetail(w, http.StatusInternalServerError, "apply_failed", err)
}

// rollbackConflictBody is the 409 answer of a rollback that found rows changed
// since the apply: the error body beside the current value of each such row.
type rollbackConflictBody struct {
	errorBody
	Conflicts map[performance.RowID]float64 `json:"conflicts"`
}

// rollbackBody is the answer of POST /api/v1/performance/rollback.
type rollbackBody struct {
	Change        *performance.Change `json:"change"`
	LeftUnchanged []string            `json:"left_unchanged"`
}

// handlePerformanceRollback serves POST /api/v1/performance/rollback. It puts the
// rows of the held change back to their values before the apply. Rows that
// moved since the apply are conflicts: without only_unchanged the rollback
// refuses and writes nothing, with it those rows stay as they are. The rights
// are checked per row inside the writer, as for the apply. A rollback creates
// no change of its own.
func handlePerformanceRollback(deps *PerformanceDeps, now func() time.Time) http.HandlerFunc {
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
		var req performance.RollbackRequest
		if err := decodeBody(r, &req); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
			return
		}

		if deps.Changes == nil {
			writeError(w, http.StatusConflict, "no_change_to_undo", "Es gibt keine Änderung zum Zurücknehmen")
			return
		}
		change := deps.Changes.CurrentChange()
		if change == nil || change.State == "undone" {
			writeError(w, http.StatusConflict, "no_change_to_undo", "Es gibt keine Änderung zum Zurücknehmen")
			return
		}

		snap := deps.Provider.Snapshot(now())
		rows, _ := performanceRows(snap, deps)
		current := make(map[performance.RowID]float64, len(rows))
		for _, row := range rows {
			current[row.ID] = row.Current
		}
		conflicts := performance.Conflicts(*change, current)
		if len(conflicts) > 0 && !req.OnlyUnchanged {
			writeJSONStatus(w, http.StatusConflict, rollbackConflictBody{
				errorBody: errorBody{Code: "rollback_conflict", Message: "Einige Zeilen wurden seit der Übernahme geändert", MessageKey: "error.rollback_conflict"},
				Conflicts: conflicts,
			})
			return
		}

		// Rows that are not present have no current value and are skipped. A
		// conflicting row keeps its current value.
		rates := make(map[performance.RowID]float64, len(change.After))
		for id := range change.After {
			if _, present := current[id]; !present {
				continue
			}
			if _, conflict := conflicts[id]; conflict {
				continue
			}
			rates[id] = change.Before[id]
		}
		if len(rates) > 0 {
			writer := &rateWriter{
				Settings:   deps.Settings,
				Configs:    deps.Configs,
				SystemPath: deps.SystemPath,
				DataDir:    deps.DataDir,
				Executor:   deps.Executor,
				NodeRates:  deps.NodeRates,
			}
			_, err := writer.Apply(r.Context(), ApplyRequest{Rates: rates}, rows, func(right string) bool {
				return auth.HasRole(user, performanceRightRole(right))
			})
			if err != nil {
				writePerformanceWriteError(w, err)
				return
			}
		}

		// A failing save does not fail the rollback, the rates are already written.
		_ = deps.Changes.MarkUndone()
		left := make([]string, 0, len(conflicts))
		for id := range conflicts {
			left = append(left, string(id))
		}
		sort.Strings(left)
		writeJSON(w, rollbackBody{Change: deps.Changes.CurrentChange(), LeftUnchanged: left})
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
