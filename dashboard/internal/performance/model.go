package performance

import "math"

// RowID names one polling row of the performance page.
type RowID string

// The rows of the performance page.
const (
	RowLiveUpdate RowID = "live_update"
	RowTick       RowID = "tick"
	RowRegulator  RowID = "regulator"
	RowNode       RowID = "node"
	RowShelly     RowID = "shelly"
	RowTrucki     RowID = "trucki"
	RowBatterySoc RowID = "battery_soc"
	RowApsystems  RowID = "apsystems"
	RowTuya       RowID = "tuya"
)

// Row is one polling row with its cost at the current rate.
type Row struct {
	ID      RowID
	Current float64   // aktuelle Abfrageperiode in Sekunden
	Steps   []float64 // waehlbare Stufen
	Floor   float64   // unterste Stufe, Geraetegrenze oder Festwert
	RoundS  float64   // uebliche Dauer einer Runde
	CostPct float64   // Kosten bei Current inkl. Nachrichtenanteil
	Right   string    // Schreibrecht: "", "edit_setup", "automations", "system_actions"
	Follows string    // "", "half", "balance"
	Source  bool      // liefert Werte, die Dashboard und Regeln lesen
	Balance bool      // liefert die Netzbilanz
	Present bool      // false: Zeile gibt es nicht (Regler ohne Szenario)
}

// RowCost scales the cost of a row from its current period to the period v.
// The round duration is added to the period on both sides.
func RowCost(r Row, v float64) float64 {
	denom := v + r.RoundS
	if denom <= 0 {
		return 0
	}
	return r.CostPct * (r.Current + r.RoundS) / denom
}

// Estimate adds the base load and the cost of every present row. sel gives the
// chosen period per row, a row without an entry keeps its Current period.
func Estimate(basePct float64, rows []Row, sel map[RowID]float64) float64 {
	total := basePct
	for _, r := range rows {
		if !r.Present {
			continue
		}
		v, ok := sel[r.ID]
		if !ok {
			v = r.Current
		}
		total += RowCost(r, v)
	}
	return total
}

// DefaultTarget is the default CPU budget in percent for a device with the
// given number of cores.
func DefaultTarget(cores int) int {
	if cores == 1 {
		return 60
	}
	return 70
}

// Deduction is the reduction of the budget from warning signals. Reasons are
// catalogue keys in a fixed order.
type Deduction struct {
	Pct     float64
	Reasons []string
}

// maxDeduction caps the sum of all deductions.
const maxDeduction = 40

// DeductionFor reads the warning signals of the snapshot. CPU throttling takes
// 15, a temperature of at least 80 C takes 10 and at least 75 C takes 5, free
// RAM below 15 % takes 10 and more than 10 swapped pages per second take 10.
func DeductionFor(s Snapshot) Deduction {
	d := Deduction{Reasons: []string{}}
	add := func(pct float64, reason string) {
		d.Pct += pct
		d.Reasons = append(d.Reasons, reason)
	}
	if s.Throttled {
		add(15, "performance.deduction.throttled")
	}
	if s.TempC != nil {
		switch {
		case *s.TempC >= 80:
			add(10, "performance.deduction.hot")
		case *s.TempC >= 75:
			add(5, "performance.deduction.warm")
		}
	}
	if s.RAMFreePct < 15 {
		add(10, "performance.deduction.ram")
	}
	if s.SwapPagesPerS > 10 {
		add(10, "performance.deduction.swap")
	}
	d.Pct = math.Min(d.Pct, maxDeduction)
	return d
}

// RateConfig holds the current periods in seconds. Services maps the service
// ID to its poll period.
type RateConfig struct {
	LiveUpdateS, TickS, RegulatorS, NodeS float64
	Services                              map[string]float64
}

// serviceRows maps the service IDs to their row IDs.
var serviceRows = map[string]RowID{
	"shelly":        RowShelly,
	"trucki":        RowTrucki,
	"battery_soc":   RowBatterySoc,
	"apsystems_ez1": RowApsystems,
	"tuya_mqtt":     RowTuya,
}

// rowSpec is the static part of a row.
type rowSpec struct {
	id         RowID
	service    string // Dienst-ID, leer bei Zeilen ohne Dienst
	steps      []float64
	fixedFloor float64
	right      string
	follows    string
	source     bool
	balance    bool
}

// rowSpecs lists the rows in page order.
var rowSpecs = []rowSpec{
	{id: RowLiveUpdate, steps: []float64{1, 2, 3, 5, 10, 15, 30}, right: "edit_setup"},
	{id: RowTick, steps: []float64{5, 10, 15, 20, 30, 60}, fixedFloor: 5, right: "automations"},
	{id: RowRegulator, steps: []float64{5, 10, 15, 20, 30, 60}, fixedFloor: 5, right: "automations"},
	{id: RowNode, steps: []float64{5, 10, 15, 30, 60, 120}, fixedFloor: 5, right: "system_actions"},
	{id: RowShelly, service: "shelly", steps: []float64{2, 3, 5, 10, 15, 20, 30, 60}, right: "system_actions", source: true},
	{id: RowTrucki, service: "trucki", steps: []float64{2, 3, 5, 10, 15, 20, 30, 60}, right: "system_actions", follows: "balance", balance: true},
	{id: RowBatterySoc, service: "battery_soc", steps: []float64{1, 2, 5, 10, 20, 30}, right: "automations", follows: "balance", balance: true},
	{id: RowApsystems, service: "apsystems_ez1", steps: []float64{12, 15, 20, 30, 60, 120}, right: "system_actions", follows: "half"},
	{id: RowTuya, service: "tuya_mqtt", steps: []float64{10, 15, 20, 30, 60}, right: "system_actions", follows: "half"},
}

// Units that carry the cost split.
const (
	mosquittoUnit  = "mosquitto.service"
	automationUnit = "automation.service"
)

// BuildRows lists the rows with their floors, rounds and costs at the current
// periods.
//
// Floors come from floors, keyed by service ID, or by row ID for rows without a
// service. The fixed floors of node, tick and regulator (5 s) apply on top.
// present reports whether the regulator row exists.
//
// Cost split: a service carries its own P95 plus the CPU per message of
// mosquitto times its messages in the window. The dashboard's P95 is the
// live_update row. The automation unit's P95 is split between tick and
// regulator by their runs per second. Node has no unit of its own and costs 0.
// Everything else stays in the base load.
func BuildRows(snap Snapshot, cur RateConfig, floors map[string]float64, present map[RowID]bool) []Row {
	units := map[string]UnitStats{}
	for _, u := range snap.Units {
		units[u.ID] = u
	}

	minutes := snap.MeasuredFor.Minutes()
	msgs := map[string]float64{}
	var totalMsgs float64
	for _, u := range snap.Units {
		if u.Fixed {
			continue
		}
		msgs[u.ID] = u.MsgPerMin * minutes
		totalMsgs += msgs[u.ID]
	}
	perMsg := 0.0
	if totalMsgs > 0 {
		perMsg = unitCPU(units, mosquittoUnit) / totalMsgs
	}

	tickRuns := runsPerS(cur.TickS)
	regRuns := 0.0
	if present[RowRegulator] {
		regRuns = runsPerS(cur.RegulatorS)
	}
	automationCPU := unitCPU(units, automationUnit)

	rows := make([]Row, 0, len(rowSpecs))
	for _, spec := range rowSpecs {
		r := Row{
			ID:      spec.id,
			Steps:   spec.steps,
			Right:   spec.right,
			Follows: spec.follows,
			Source:  spec.source,
			Balance: spec.balance,
			Present: true,
		}

		floorKey := spec.service
		if floorKey == "" {
			floorKey = string(spec.id)
		}
		r.Floor = math.Max(floors[floorKey], spec.fixedFloor)

		switch spec.id {
		case RowLiveUpdate:
			r.Current = cur.LiveUpdateS
			r.CostPct = unitCPU(units, DashboardUnit)
		case RowTick:
			r.Current = cur.TickS
			r.CostPct = shareOf(automationCPU, tickRuns, tickRuns+regRuns)
		case RowRegulator:
			r.Current = cur.RegulatorS
			r.Present = present[RowRegulator]
			r.CostPct = shareOf(automationCPU, regRuns, tickRuns+regRuns)
		case RowNode:
			r.Current = cur.NodeS
		default:
			r.Current = cur.Services[spec.service]
			u := units[spec.service]
			if u.RoundS != nil {
				r.RoundS = *u.RoundS
			}
			r.CostPct = u.CPUP95 + perMsg*msgs[spec.service]
		}
		rows = append(rows, r)
	}
	return rows
}

// unitCPU returns the P95 of a unit, 0 when the unit is not in the snapshot.
func unitCPU(units map[string]UnitStats, id string) float64 {
	return units[id].CPUP95
}

// runsPerS returns the runs per second of a period, 0 for an unset period.
func runsPerS(period float64) float64 {
	if period <= 0 {
		return 0
	}
	return 1 / period
}

// shareOf returns part of total, 0 when there is nothing to share.
func shareOf(total, part, sum float64) float64 {
	if sum <= 0 {
		return 0
	}
	return total * part / sum
}
