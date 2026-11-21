package autolearn

import "github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"

const (
	// unthrottledMinPVW is the PV power above which a minute can be unthrottled.
	unthrottledMinPVW = 50.0
	// unthrottledImportW is the grid import above which a regulated minute
	// counts as unthrottled when the regulator output is at its top.
	unthrottledImportW = 50.0
	// unthrottledExportMarginW is added to the regulator deadband for the
	// feed-in check.
	unthrottledExportMarginW = 20.0
	// unthrottledOutputShare is the share of the regulator maximum the output
	// must reach for the import check.
	unthrottledOutputShare = 0.98
	// unthrottledChargeW is the battery charge power above which a minute can
	// be unthrottled.
	unthrottledChargeW = 50.0
	// unthrottledFullSOC is the state of charge from which the battery counts
	// as full.
	unthrottledFullSOC = 95.0
)

// RegulatorView is the regulator state of one minute, as the learning steps
// see it. Present is false when no regulator scenario exists.
type RegulatorView struct {
	DeadbandW float64
	MaxW      float64
	OutputW   float64
	Present   bool
}

// Unthrottled reports whether a PV minute ran without a regulator holding the
// production back. A minute counts only with PV above 50 W, and then when one
// of these holds:
//   - no regulator exists (reg nil or not present),
//   - grid export is above the regulator deadband plus 20 W,
//   - grid import is above 50 W while the regulator output is at least 98 % of its maximum,
//   - battery charge is above 50 W while the state of charge is below 95 %.
//
// A missing battery_soc counts as not below 95 %, so the charge check needs the value.
func Unthrottled(f learnrec.Frame, reg *RegulatorView) bool {
	if f.Balance["pv"] <= unthrottledMinPVW {
		return false
	}
	if reg == nil || !reg.Present {
		return true
	}
	if f.Balance["grid_export"] > reg.DeadbandW+unthrottledExportMarginW {
		return true
	}
	if f.Balance["grid_import"] > unthrottledImportW && reg.MaxW > 0 &&
		reg.OutputW >= unthrottledOutputShare*reg.MaxW {
		return true
	}
	soc, hasSOC := f.Balance["battery_soc"]
	return f.Balance["battery_charge"] > unthrottledChargeW && hasSOC && soc < unthrottledFullSOC
}
