package autolearn

import (
	"context"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

// relDay returns an empty day file for the day that starts at midnight of day.
func relDay(day time.Time) DayFile {
	return DayFile{V: dayFileVersion, Day: day.Format(dayLayout), Devices: map[string]*DeviceDay{},
		Events: []EventRec{}, Actions: []ActionRec{}}
}

// relReachable gives the device full counters for every quarter of the day.
func relReachable(f *DayFile, device string) *DeviceDay {
	dd := &DeviceDay{}
	for q := range dd.Seen {
		dd.Seen[q] = dayCounterMax
		dd.Reach[q] = dayCounterMax
	}
	f.Devices[device] = dd
	return dd
}

// relAt returns the unix second of hh:mm:ss on day.
func relAt(day time.Time, hh, mm, ss int) int64 {
	return time.Date(day.Year(), day.Month(), day.Day(), hh, mm, ss, 0, day.Location()).Unix()
}

// relRuleEvent adds a switching event with origin rule.
func relRuleEvent(f *DayFile, at int64, device, rule string, on bool) {
	f.Events = append(f.Events, EventRec{At: at, Device: device, Entity: "switch." + device,
		On: on, Origin: string(OriginRule), Rule: rule})
}

// relRuleAction adds a rule action with its result.
func relRuleAction(f *DayFile, at int64, device, rule string, there, ok bool) {
	want := true
	f.Actions = append(f.Actions, ActionRec{At: at, Rule: rule, Device: device,
		Entity: "switch." + device, Want: &want, There: there, OK: ok, Reaction: 2})
}

// relRule is an enabled rule with one condition, so a hold can be set.
func relRule(id, name string) Rule {
	return Rule{ID: id, Name: name, Enabled: true,
		Conditions: []Condition{{Type: "balance_threshold", Field: "grid_export", Comparison: "above", Threshold: 300}}}
}

// relInput builds the input of one detector run.
func relInput(loc *time.Location, days []DayFile, devices map[string]DeviceInfo, rules ...Rule) *Input {
	return &Input{Now: time.Date(2026, 11, 20, 12, 0, 0, 0, loc), Loc: loc, Days: days,
		Doc: Doc{Rules: rules}, Devices: devices}
}

// relDevice is a switchable device with the given role.
func relDevice(role energy.Role) DeviceInfo {
	return DeviceInfo{ID: "dev", Name: "Dev", Role: role, Switchable: true}
}

// relRun runs one detector on one device and returns the candidates.
func relRun(run func(context.Context, *Input, string) []Candidate, in *Input, device string) []Candidate {
	return run(context.Background(), in, device)
}
