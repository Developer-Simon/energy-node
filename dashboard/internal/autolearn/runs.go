package autolearn

import (
	"sort"
	"time"
)

// Run is one run of a switched entity, from on to off. EndOrigin is the origin
// of the off event, Wh the energy the device drew during the run.
type Run struct {
	Device, Entity string
	Start, End     time.Time
	EndOrigin      Origin
	Wh             float64
}

// RunsOf returns the finished runs of the day files in local time. A run that
// is still on at the end of the days is dropped.
func RunsOf(days []DayFile) []Run {
	return runsIn(days, time.Local)
}

// runsIn is RunsOf with the time zone of the day files given. An on event while
// the entity already runs keeps the first start, an off event without an on
// event is ignored.
func runsIn(days []DayFile, loc *time.Location) []Run {
	if loc == nil {
		loc = time.Local
	}
	type event struct {
		at     time.Time
		device string
		entity string
		on     bool
		origin Origin
	}
	var events []event
	byDay := make(map[string]*DayFile, len(days))
	for i := range days {
		f := &days[i]
		byDay[f.Day] = f
		for _, e := range f.Events {
			events = append(events, event{at: time.Unix(e.At, 0).In(loc), device: e.Device,
				entity: e.Entity, on: e.On, origin: Origin(e.Origin)})
		}
	}
	sort.SliceStable(events, func(a, b int) bool { return events[a].at.Before(events[b].at) })

	open := map[string]event{}
	var runs []Run
	for _, e := range events {
		start, running := open[e.entity]
		switch {
		case e.on && !running:
			open[e.entity] = e
		case !e.on && running:
			delete(open, e.entity)
			runs = append(runs, Run{Device: start.device, Entity: e.entity, Start: start.at, End: e.at,
				EndOrigin: e.origin, Wh: runEnergy(byDay, loc, start.device, start.at, e.at)})
		}
	}
	return runs
}

// runEnergy sums the Wh of the device over [from, to). Each quarter of an hour
// counts with the share of the run that lies in it.
func runEnergy(byDay map[string]*DayFile, loc *time.Location, device string, from, to time.Time) float64 {
	var wh float64
	for t := from; t.Before(to); {
		local := t.In(loc)
		into := time.Duration(local.Minute()%15)*time.Minute +
			time.Duration(local.Second())*time.Second + time.Duration(local.Nanosecond())
		next := t.Add(15*time.Minute - into)
		if next.After(to) {
			next = to
		}
		if f := byDay[local.Format(dayLayout)]; f != nil {
			if dd := f.Devices[device]; dd != nil {
				q := local.Hour()*4 + local.Minute()/15
				wh += float64(dd.Wh[q]) * next.Sub(t).Minutes() / 15
			}
		}
		t = next
	}
	return wh
}

// withEntity sets the entity key of a prefill, an empty entity leaves the key
// out so that the assistant does not get an empty selection.
func withEntity(prefill map[string]any, key, entity string) map[string]any {
	if entity != "" {
		prefill[key] = entity
	}
	return prefill
}

// dominantEntity returns the entity that occurs most often, the smallest name
// wins a tie. It returns an empty string for no entities.
func dominantEntity(entities []string) string {
	counts := map[string]int{}
	for _, e := range entities {
		if e != "" {
			counts[e]++
		}
	}
	best := ""
	for e, n := range counts {
		if best == "" || n > counts[best] || (n == counts[best] && e < best) {
			best = e
		}
	}
	return best
}

// switchEntityOf is the entity the device was switched by most often in the
// day files, the one the assistant selects. Empty without a switch event.
func switchEntityOf(in *Input, device string) string {
	var entities []string
	for i := range in.Days {
		for _, ev := range in.Days[i].Events {
			if ev.Device == device {
				entities = append(entities, ev.Entity)
			}
		}
	}
	return dominantEntity(entities)
}
