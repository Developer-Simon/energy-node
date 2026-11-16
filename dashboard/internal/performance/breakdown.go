package performance

import (
	"bytes"
	"encoding/json"
	"sort"
)

// DashboardSplit splits the measured dashboard time of the window into the
// parts and the garbage collector.
type DashboardSplit struct {
	GCShare float64          // Anteil der Speicherbereinigung an der Go-CPU im Fenster, 0..1
	Shares  map[Part]float64 // Anteil je Teil an der gemessenen Zeit, Summe 1
}

// PartPct is the share of one piece of the dashboard load in percent. ID is a
// Part or "gc".
type PartPct struct {
	ID  string
	Pct float64
}

// DashboardBreakdown splits the P95 of the dashboard unit into the parts and
// the garbage collector. The parts come in TimedParts order, gc last. It
// returns nil without a split.
func DashboardBreakdown(snap Snapshot) []PartPct {
	if snap.Dashboard == nil {
		return nil
	}
	var d float64
	for _, u := range snap.Units {
		if u.ID == DashboardUnit {
			d = u.CPUP95
		}
	}
	split := snap.Dashboard
	out := make([]PartPct, 0, len(TimedParts)+1)
	for _, p := range TimedParts {
		out = append(out, PartPct{ID: string(p), Pct: d * (1 - split.GCShare) * split.Shares[p]})
	}
	out = append(out, PartPct{ID: "gc", Pct: d * split.GCShare})
	return out
}

// AutomationEntry is one active rule or scenario. Kind is "rule" or
// "scenario". Regulator is true for a scenario with a regulator.
type AutomationEntry struct {
	ID, Name, Kind string
	Regulator      bool
}

// automationFile holds the parts of automation_rules.json that the breakdown reads.
type automationFile struct {
	Rules []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	} `json:"rules"`
	Scenarios []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
		While   struct {
			Regulator json.RawMessage `json:"regulator"`
		} `json:"while"`
	} `json:"scenarios"`
}

// ParseAutomation lists the active rules and scenarios of automation_rules.json,
// rules first. Disabled entries are left out. An entry without a name takes its ID.
// A scenario has a regulator when while.regulator is an object.
func ParseAutomation(raw []byte) ([]AutomationEntry, error) {
	var doc automationFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := make([]AutomationEntry, 0, len(doc.Rules)+len(doc.Scenarios))
	for _, r := range doc.Rules {
		if !r.Enabled {
			continue
		}
		out = append(out, AutomationEntry{ID: r.ID, Name: nameOr(r.Name, r.ID), Kind: "rule"})
	}
	for _, s := range doc.Scenarios {
		if !s.Enabled {
			continue
		}
		reg := bytes.HasPrefix(bytes.TrimSpace(s.While.Regulator), []byte("{"))
		out = append(out, AutomationEntry{ID: s.ID, Name: nameOr(s.Name, s.ID), Kind: "scenario", Regulator: reg})
	}
	return out, nil
}

// nameOr returns name, or fallback when name is empty.
func nameOr(name, fallback string) string {
	if name == "" {
		return fallback
	}
	return name
}

// HasRegulator reports whether one of the entries is a scenario with a regulator.
func HasRegulator(entries []AutomationEntry) bool {
	for _, e := range entries {
		if e.Regulator {
			return true
		}
	}
	return false
}

// AutomationItem is the share of one rule or scenario in a row's cost.
type AutomationItem struct {
	ID, Name, Kind string
	Pct            float64
}

// AutomationGroup is the cost of one row split over its entries. Items holds
// at most the limit, sorted by share, the rest counts in More and MorePct.
type AutomationGroup struct {
	Row     RowID
	Pct     float64
	Items   []AutomationItem
	More    int
	MorePct float64
}

// AutomationBreakdown splits the cost of the tick row evenly over all entries
// and the cost of the regulator row evenly over the scenarios with a regulator.
// A group without entries or a row that is not present is left out.
func AutomationBreakdown(rows []Row, entries []AutomationEntry, limit int) []AutomationGroup {
	var groups []AutomationGroup
	if g, ok := automationGroup(rows, RowTick, entries, limit); ok {
		groups = append(groups, g)
	}
	var regulated []AutomationEntry
	for _, e := range entries {
		if e.Kind == "scenario" && e.Regulator {
			regulated = append(regulated, e)
		}
	}
	if g, ok := automationGroup(rows, RowRegulator, regulated, limit); ok {
		groups = append(groups, g)
	}
	return groups
}

// automationGroup splits the cost of the present row id evenly over members.
func automationGroup(rows []Row, id RowID, members []AutomationEntry, limit int) (AutomationGroup, bool) {
	row, ok := presentRow(rows, id)
	if !ok || len(members) == 0 {
		return AutomationGroup{}, false
	}
	share := row.CostPct / float64(len(members))
	items := make([]AutomationItem, 0, len(members))
	for _, e := range members {
		items = append(items, AutomationItem{ID: e.ID, Name: e.Name, Kind: e.Kind, Pct: share})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Pct != items[j].Pct {
			return items[i].Pct > items[j].Pct
		}
		return items[i].Name < items[j].Name
	})
	g := AutomationGroup{Row: id, Pct: row.CostPct}
	if limit < 0 {
		limit = 0
	}
	if len(items) > limit {
		for _, it := range items[limit:] {
			g.MorePct += it.Pct
		}
		g.More = len(items) - limit
		items = items[:limit]
	}
	g.Items = items
	return g, true
}

// presentRow returns the row with the given ID when it is present.
func presentRow(rows []Row, id RowID) (Row, bool) {
	for _, r := range rows {
		if r.ID == id && r.Present {
			return r, true
		}
	}
	return Row{}, false
}
