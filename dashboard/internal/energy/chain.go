package energy

import (
	"math"
	"slices"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energydiscovery"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

const (
	ChainDirectionAuto      = "auto"
	ChainDirectionReversed  = "reversed"
	ChainDirectionFixedDown = "fixed_down"
	ChainDirectionFixedUp   = "fixed_up"
)

const (
	QualitySubstitute = "substitute"
	// ChainFreshAge is how old the last update of a counting point may be
	// before a fresh point further along the chain takes over.
	ChainFreshAge = 10 * time.Minute
	// chainMinInputW is the source power below which a segment has no
	// efficiency, the ratio of two small numbers says nothing.
	chainMinInputW = 10.0
	// chainIdleW is the power below which a chain counts as idle.
	chainIdleW = 1.0
)

// ChainPointState is one measuring point of a chain as the energy page shows
// it.
type ChainPointState struct {
	EntityID  string  `json:"entity_id"`
	DeviceID  string  `json:"device_id"`
	Label     string  `json:"label"`
	Value     float64 `json:"value"`
	HasValue  bool    `json:"has_value"`
	Freshness string  `json:"freshness"` // fresh, stale, missing
	Counting  bool    `json:"counting"`
}

// From and To follow the current flow, so LossW is source minus target of the
// absolute values. Efficiency stays nil below chainMinInputW at the source.
type ChainSegmentState struct {
	From       string   `json:"from"`
	To         string   `json:"to"`
	Available  bool     `json:"available"`
	LossW      float64  `json:"loss_w"`
	Efficiency *float64 `json:"efficiency,omitempty"`
}

type ChainState struct {
	ID              string              `json:"id"`
	Label           string              `json:"label"`
	Role            Role                `json:"role,omitempty"`
	Counting        string              `json:"counting"`
	Direction       string              `json:"direction"`
	Flow            string              `json:"flow"` // down, up, idle
	Points          []ChainPointState   `json:"points"`
	Segments        []ChainSegmentState `json:"segments"`
	SubstituteFrom  string              `json:"substitute_from,omitempty"` // Entitäts-ID
	NoCurrentValues bool                `json:"no_current_values,omitempty"`
}

// Chain is a measurement chain from energy.json ("chains"). Points run in the
// normal flow direction. Direction empty means auto.
type Chain struct {
	Label     string   `json:"label"`
	Points    []string `json:"points"`
	Counting  string   `json:"counting"`
	Direction string   `json:"direction,omitempty"`
}

// IsReference reports whether the entity is a measuring point of the chain
// that does not count: its power is already in the counting point.
func (c Chain) IsReference(entityID string) bool {
	if entityID == c.Counting {
		return false
	}
	for _, point := range c.Points {
		if point == entityID {
			return true
		}
	}
	return false
}

// ReferencePoints maps every reference point of every chain to the ID of its
// chain.
func (m Model) ReferencePoints() map[string]string {
	out := map[string]string{}
	for _, id := range sortedChainIDs(m.Chains) {
		chain := m.Chains[id]
		for _, point := range chain.Points {
			if chain.IsReference(point) {
				out[point] = id
			}
		}
	}
	return out
}

// ChainOf returns the chain that lists the entity as a measuring point.
func (m Model) ChainOf(entityID string) (id string, chain Chain, ok bool) {
	for _, id := range sortedChainIDs(m.Chains) {
		chain := m.Chains[id]
		for _, point := range chain.Points {
			if point == entityID {
				return id, chain, true
			}
		}
	}
	return "", Chain{}, false
}

// ApplyChainChange returns the assignments after prev became next. prev nil
// means created, next nil means deleted. effective holds the current role of
// each involved entity, stored or detected. roleNeeded lists the entities that
// left the chain without a role.
func ApplyChainChange(assignments, effective map[string]Assignment, prev, next *Chain) (out map[string]Assignment, roleNeeded []string) {
	out = make(map[string]Assignment, len(assignments))
	for id, a := range assignments {
		out[id] = a
	}
	if prev == nil && next == nil {
		return out, nil
	}

	role := chainRole(effective, prev, next)

	if next != nil {
		for _, point := range next.Points {
			a := entryFor(out, effective, point)
			a.Role = ""
			if point == next.Counting {
				a.Role = role
			}
			out[point] = a
		}
	}

	if prev != nil {
		for _, point := range prev.Points {
			if next != nil && slices.Contains(next.Points, point) {
				continue
			}
			a := entryFor(out, effective, point)
			if next == nil && point == prev.Counting {
				a.Role = role
			} else {
				a.Role = ""
				roleNeeded = append(roleNeeded, point)
			}
			out[point] = a
		}
	}
	return out, roleNeeded
}

// chainRole is the role the chain carries across the change: the counting
// role of the old chain, otherwise the first role along its points, otherwise
// the same search on the new chain.
func chainRole(effective map[string]Assignment, prev, next *Chain) Role {
	if prev != nil {
		if role := effective[prev.Counting].Role; role != "" {
			return role
		}
		if role := firstRole(effective, prev.Points); role != "" {
			return role
		}
		if next != nil {
			return firstRole(effective, next.Points)
		}
		return ""
	}
	if role := effective[next.Counting].Role; role != "" {
		return role
	}
	return firstRole(effective, next.Points)
}

func firstRole(effective map[string]Assignment, points []string) Role {
	for _, point := range points {
		if role := effective[point].Role; role != "" {
			return role
		}
	}
	return ""
}

// entryFor returns the stored assignment of the point. A point without one
// starts from its effective Scale and Invert, never from a chain role.
func entryFor(out, effective map[string]Assignment, point string) Assignment {
	if a, ok := out[point]; ok {
		return a
	}
	e := effective[point]
	return Assignment{Scale: e.Scale, Invert: e.Invert}
}

func sortedChainIDs(chains map[string]Chain) []string {
	ids := make([]string, 0, len(chains))
	for id := range chains {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// chainEval is what evaluateChains hands to Aggregate: the reference points
// that do not count, the substitute value per counting entity and the state
// of every chain.
type chainEval struct {
	references  map[string]bool
	substitutes map[string]chainSubstitute // Zählpunkt -> Ersatzwert
	states      []ChainState
}

type chainSubstitute struct {
	value float64 // W, Scale und Invert des Quellpunkts angewendet
	label string
}

type chainRead struct {
	device registry.DeviceView
	entity registry.EntityView
}

// evaluateChains reads every chain of the model. A counting point that is not
// fresh is replaced by the nearest fresh point of its chain, on a tie the
// earlier point wins.
func evaluateChains(devices []registry.DeviceView, resolver *Resolver, model Model, at time.Time) chainEval {
	out := chainEval{references: map[string]bool{}, substitutes: map[string]chainSubstitute{}}
	if len(model.Chains) == 0 {
		return out
	}

	index := map[string]chainRead{}
	for _, device := range devices {
		if device.ID == energydiscovery.DeviceIdentifier {
			continue
		}
		for _, entity := range device.Entities {
			if _, seen := index[entity.UniqueID]; !seen {
				index[entity.UniqueID] = chainRead{device: device, entity: entity}
			}
		}
	}

	for _, id := range sortedChainIDs(model.Chains) {
		chain := model.Chains[id]
		for _, point := range chain.Points {
			if chain.IsReference(point) {
				out.references[point] = true
			}
		}

		points := make([]ChainPointState, len(chain.Points))
		anyFresh := false
		for i, point := range chain.Points {
			points[i] = readChainPoint(index, resolver, point, at)
			points[i].Counting = point == chain.Counting
			if points[i].Freshness == "fresh" {
				anyFresh = true
			}
		}

		countIdx := slices.Index(chain.Points, chain.Counting)
		substituteFrom := ""
		var sub *chainSubstitute
		if countIdx >= 0 && points[countIdx].Freshness != "fresh" {
			if src := nearestFreshPoint(points, countIdx); src >= 0 {
				s := chainSubstitute{value: points[src].Value, label: points[src].Label}
				out.substitutes[chain.Counting] = s
				sub = &s
				substituteFrom = points[src].EntityID
			}
		}

		activeValue, hasActive := 0.0, false
		switch {
		case sub != nil:
			activeValue, hasActive = sub.value, true
		case countIdx >= 0 && points[countIdx].HasValue:
			activeValue, hasActive = points[countIdx].Value, true
		}

		direction := chain.Direction
		if direction == "" {
			direction = ChainDirectionAuto
		}
		flow := chainFlow(direction, activeValue, hasActive)

		var role Role
		if read, ok := index[chain.Counting]; ok {
			assignment, _ := resolver.Resolve(read.entity)
			role = assignment.Role
		}

		segments := []ChainSegmentState{}
		for i := 0; i+1 < len(points); i++ {
			from, to := i, i+1
			if flow == "up" {
				from, to = i+1, i
			}
			src, dst := points[from], points[to]
			seg := ChainSegmentState{From: src.EntityID, To: dst.EntityID}
			if src.Freshness == "fresh" && dst.Freshness == "fresh" {
				seg.Available = true
				seg.LossW = math.Abs(src.Value) - math.Abs(dst.Value)
				if math.Abs(src.Value) >= chainMinInputW {
					eff := math.Abs(dst.Value) / math.Abs(src.Value)
					seg.Efficiency = &eff
				}
			}
			segments = append(segments, seg)
		}

		state := ChainState{
			ID:              id,
			Label:           chain.Label,
			Role:            role,
			Counting:        chain.Counting,
			Direction:       direction,
			Flow:            flow,
			Points:          points,
			Segments:        segments,
			SubstituteFrom:  substituteFrom,
			NoCurrentValues: !anyFresh,
		}
		out.states = append(out.states, state)
	}
	return out
}

// readChainPoint reads one measuring point with its Scale and Invert applied.
// A point missing from the registry, without a value or with an unreadable one
// is missing.
func readChainPoint(index map[string]chainRead, resolver *Resolver, entityID string, at time.Time) ChainPointState {
	state := ChainPointState{EntityID: entityID, Label: entityID, Freshness: "missing"}
	read, ok := index[entityID]
	if !ok {
		return state
	}
	state.DeviceID = read.device.ID
	state.Label = resolvedLabel(resolver, read.device, read.entity)
	if !read.entity.HasValue {
		return state
	}
	value, _, err := powerValue(read.entity.Value, read.entity.UnitOfMeasurement)
	if err != nil {
		return state
	}
	assignment, _ := resolver.Resolve(read.entity)
	if assignment.Invert {
		value = -value
	}
	scale := assignment.Scale
	if scale == 0 {
		scale = 1
	}
	state.Value = value * scale
	state.HasValue = true

	switch {
	case read.entity.Stale || (read.entity.HasAvailability && !read.entity.Available):
		state.Freshness = "stale"
	case !read.entity.LastSeen.IsZero() && at.Sub(read.entity.LastSeen) > ChainFreshAge:
		state.Freshness = "stale"
	default:
		state.Freshness = "fresh"
	}
	return state
}

// nearestFreshPoint returns the index of the fresh point closest to from, or
// -1. On a tie the smaller index wins.
func nearestFreshPoint(points []ChainPointState, from int) int {
	best := -1
	for i, point := range points {
		if i == from || point.Freshness != "fresh" {
			continue
		}
		if best < 0 || distance(i, from) < distance(best, from) {
			best = i
		}
	}
	return best
}

func distance(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// chainFlow derives the flow from the active value. Below chainIdleW the chain
// is idle. Direction fixes the flow for fixed_* and flips the sign for
// reversed.
func chainFlow(direction string, value float64, active bool) string {
	if !active || math.Abs(value) < chainIdleW {
		return "idle"
	}
	positive := value > 0
	switch direction {
	case ChainDirectionFixedDown:
		return "down"
	case ChainDirectionFixedUp:
		return "up"
	case ChainDirectionReversed:
		if positive {
			return "up"
		}
		return "down"
	default:
		if positive {
			return "down"
		}
		return "up"
	}
}

// substituteValue applies the role's sign rule to a substitute value. Battery,
// grid and storage categories keep the sign, every other role is clamped to
// zero.
func substituteValue(role Role, value float64, model Model) float64 {
	if role == RoleBattery || role == RoleGrid {
		return value
	}
	if id, ok := role.CategoryID(); ok && model.Categories[id].Base == CategoryStorage {
		return value
	}
	if value < 0 {
		return 0
	}
	return value
}
