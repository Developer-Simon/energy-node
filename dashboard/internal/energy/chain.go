package energy

import (
	"slices"
	"sort"
)

const (
	ChainDirectionAuto      = "auto"
	ChainDirectionReversed  = "reversed"
	ChainDirectionFixedDown = "fixed_down"
	ChainDirectionFixedUp   = "fixed_up"
)

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
