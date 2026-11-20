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
}
