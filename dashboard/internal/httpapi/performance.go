package httpapi

import (
	"net/http"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
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

// PerformanceDeps wires the performance page. Rates gives the current poll
// periods, Floors the poll floor per service ID. Without it the route answers
// 501.
type PerformanceDeps struct {
	Provider PerformanceProvider
	Rates    func() performance.RateConfig
	Floors   map[string]float64
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
	Change           *performanceChange   `json:"change"`
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
	RAMFreePct float64  `json:"ram_free_pct"`
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

// performanceChange is filled by the change proposal of the page; until then
// the field is null.
type performanceChange struct{}

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
		defaultTarget := performance.DefaultTarget(snap.Cores)
		target := defaultTarget
		if store != nil {
			if value, err := store.LoadSettings(); err == nil && value.PerformanceTargetPct > 0 {
				target = value.PerformanceTargetPct
			}
		}

		present := map[performance.RowID]bool{performance.RowRegulator: true}
		rows := performance.BuildRows(snap, deps.Rates(), deps.Floors, present)
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
				RAMFreePct: snap.RAMFreePct,
				SwapPagesS: snap.SwapPagesPerS,
			},
			Rows:  []performanceRow{},
			Units: []performanceUnit{},
		}

		var costs float64
		for _, row := range rows {
			if !row.Present {
				continue
			}
			costs += row.CostPct
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
		body.BasePct = snap.CPUP95 - costs
		if body.BasePct < 0 {
			body.BasePct = 0
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

func deductionBody(d performance.Deduction) performanceDeduction {
	reasons := d.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	return performanceDeduction{Pct: d.Pct, Reasons: reasons}
}
