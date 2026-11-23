package autolearn

import (
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

// DeviceInfo describes a device as the detectors see it.
type DeviceInfo struct {
	ID         string
	Name       string
	Role       energy.Role
	Light      bool
	Switchable bool
	Entities   []string
}

// Input is everything one run of the detectors reads: the clock, the day
// profiles, the rule document, the devices, the reaction times, the stage
// limits and the day base.
type Input struct {
	Now       time.Time
	Loc       *time.Location
	Days      []DayFile
	Doc       Doc
	Devices   map[string]DeviceInfo
	Reactions *Reactions
	Limits    learnrec.Limits
	Base      Base
	// Season holds the weekly figures for the detectors, nil without a store.
	Season *SeasonStore
	// Outdoor names the outdoor temperature entity, zero when none is chosen.
	Outdoor OutdoorSource
	// PV returns the stored roof model, the sky raster and whether a model
	// exists. Nil means no model is wired, and the detectors that need one
	// return nothing.
	PV func() (PVModel, *Raster, bool)
	// Learned holds the learned values by key, such as site/base_w. Nil when
	// no store is wired.
	Learned map[string]LearnedEntry
	// PriorEmptyShare returns the share of the days of an ISO week of the year
	// before that were empty before sunrise, and whether a record of that week
	// exists. Nil, or a week without a record, means the empty finding is not made.
	PriorEmptyShare func(week int) (float64, bool)
	// Minutes reads the minute record. Nil when none is wired, and the replay
	// then works from the quarter hours of the day files.
	Minutes learnrec.Reader
	// Steps reads the regulator steps of a scenario from and to, both ends
	// included. Nil when no step log is wired, and the detectors that need the
	// steps then find nothing.
	Steps func(scenario string, from, to time.Time) []Step
}

// OutdoorSource names the outdoor temperature entity: its unique id, its state
// topic and its value template. The zero value means no entity.
type OutdoorSource struct {
	EntityID      string
	Topic         string
	ValueTemplate string
}

// Base holds the day types, the absence and the day bounds derived from the
// day profiles. DayType maps a date (YYYY-MM-DD) to its group. DayStart and
// NightRest hold per group the minute of the day.
type Base struct {
	DayType   map[string]int
	Groups    [][]time.Weekday
	Away      map[string]bool
	DayStart  map[int]float64
	NightRest map[int]float64
	// RecentOutdoorC is the mean outdoor temperature of the last three days,
	// nil when no day has a temperature.
	RecentOutdoorC *float64
}

// Benefit is the gain of a candidate in its unit: kWh per week, actions per
// week or cycles per day.
type Benefit struct {
	Unit  string  `json:"unit"`
	Value float64 `json:"value"`
}

// Candidate is a finding of a detector before the gates decide on it. NEff is
// the effective number of days, MinNEff the minimum of the detector in its
// unit, Lower the lower confidence bound and Limit the bound of the detector
// for high confidence. HalvesAgree tells whether the first and the second half
// of the days agree. Values and Spread hold the numbers for the quiet check.
type Candidate struct {
	Finding     Finding
	Detector    string
	NEff        float64
	MinNEff     float64
	Lower       float64
	Limit       float64
	HalvesAgree bool
	Benefit     Benefit
	Values      map[string]float64
	Spread      map[string]float64
	// Replay is the replay check of an energy suggestion, nil when the
	// suggestion is not replayed.
	Replay *ReplayVerdict
	// Value is set on a learned value. Such a candidate is no finding and
	// reaches the engine through Values, not Candidates.
	Value *Value
}
