package energy

import "sort"

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

func sortedChainIDs(chains map[string]Chain) []string {
	ids := make([]string, 0, len(chains))
	for id := range chains {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
