package autolearn

import (
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

// Shared parts of the reliability detectors (spec 15.4 "Takten" and "Aktion
// ohne Wirkung"). Both read the rule events and rule actions of the day files.

const (
	// reliabilityWindowMin is the half width in minutes of the window that
	// collects the failures of one time of day.
	reliabilityWindowMin = 30.0
	// reliabilityClusterShare is the share of failures a window must hold to
	// count as clustered.
	reliabilityClusterShare = 0.6
	// reliabilityPeakShare is the share of the largest density a peak must
	// reach to be a candidate centre. The spec gives no number, the value is an assumption.
	reliabilityPeakShare = 0.5
)

// reliabilityMinRuntime is the shortest runtime in seconds a device tolerates
// by role: heat pump 10 minutes, wallbox 3 minutes, lights none (0), all other
// switchable devices 2 minutes.
func reliabilityMinRuntime(info DeviceInfo) float64 {
	switch {
	case info.Light:
		return 0
	case info.Role == energy.RoleHeatPump:
		return 600
	case info.Role == energy.RoleWallbox:
		return 180
	default:
		return 120
	}
}

// ruleEvent is a switching event of a device that a rule caused.
type ruleEvent struct {
	at   int64
	rule string
	on   bool
}

// ruleEventsOf returns the events of the device with origin rule, sorted by time.
func ruleEventsOf(days []DayFile, device string) []ruleEvent {
	var out []ruleEvent
	for i := range days {
		for _, e := range days[i].Events {
			if e.Device != device || Origin(e.Origin) != OriginRule || e.Rule == "" {
				continue
			}
			out = append(out, ruleEvent{at: e.At, rule: e.Rule, on: e.On})
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].at < out[b].at })
	return out
}

// ruleByID indexes the rules of the document by ID.
func ruleByID(doc Doc) map[string]Rule {
	out := make(map[string]Rule, len(doc.Rules))
	for _, r := range doc.Rules {
		out[r.ID] = r
	}
	return out
}

// unreachable reports whether the device could not be reached in quarter q:
// its reach counter is below half of the seen counter (spec 15.4).
func unreachable(dd *DeviceDay, q int) bool {
	return dd != nil && int(dd.Reach[q])*2 < int(dd.Seen[q])
}

// quarterOf returns the quarter of the local day that holds t.
func quarterOf(t time.Time) int {
	return t.Hour()*4 + t.Minute()/15
}

// clusterAt counts the minutes of the day within reliabilityWindowMin of center on the clock.
func clusterAt(minutes []float64, center float64) int {
	n := 0
	for _, m := range minutes {
		if d := circDelta(m, center); d <= reliabilityWindowMin && d >= -reliabilityWindowMin {
			n++
		}
	}
	return n
}

// reliabilityCluster looks for a window of reliabilityWindowMin on each side of
// a peak of the density of the minutes. It reports the centre of the window
// that holds the most minutes and whether it holds at least
// reliabilityClusterShare of them.
func reliabilityCluster(minutes []float64) (center float64, ok bool) {
	if len(minutes) == 0 {
		return 0, false
	}
	best := -1
	for _, p := range CircKDE(minutes, nil).Peaks(reliabilityPeakShare) {
		if n := clusterAt(minutes, p); n > best {
			best, center = n, p
		}
	}
	return center, best >= 0 && float64(best) >= reliabilityClusterShare*float64(len(minutes))
}
