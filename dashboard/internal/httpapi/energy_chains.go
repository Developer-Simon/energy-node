package httpapi

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energydiscovery"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/uierror"
)

const (
	energyChainsPath       = "/api/v1/energy/chains"
	energyChainsItemPrefix = "/api/v1/energy/chains/"
	// chainSlugFallback names a chain whose label has no letters or digits.
	chainSlugFallback = "messkette"
)

// chainBody is the body of POST and PUT on a measurement chain. Counting
// empty means the first point counts.
type chainBody struct {
	Label     string   `json:"label"`
	Points    []string `json:"points"`
	Counting  string   `json:"counting"`
	Direction string   `json:"direction"`
}

// chainSlug turns a chain label into its ID: lower case, umlauts
// transliterated, every run of other characters one underscore. A taken ID
// gets the suffix _2, _3 and so on, without a prefix.
func chainSlug(label string, taken map[string]energy.Chain) string {
	base := subDeviceSlugSeparator.ReplaceAllString(subDeviceTranslit.Replace(strings.ToLower(label)), "_")
	base = strings.Trim(base, "_")
	if base == "" {
		base = chainSlugFallback
	}
	id := base
	for n := 2; ; n++ {
		if _, used := taken[id]; !used {
			return id
		}
		id = fmt.Sprintf("%s_%d", base, n)
	}
}

// checkChainPoints rejects measuring points that cannot be a power source:
// units other than power and the energy node's own entities. Points the
// registry does not know are skipped.
func checkChainPoints(reg *registry.Registry, points []string) error {
	for _, device := range reg.Snapshot() {
		for _, entity := range device.Entities {
			if !slices.Contains(points, entity.UniqueID) {
				continue
			}
			if device.ID == energydiscovery.DeviceIdentifier || !isSubDevicePowerUnit(entity.UnitOfMeasurement) {
				return uierror.New("error.energy_roles_rejected.chain_point_not_power",
					fmt.Sprintf("energy: %s does not measure power", entity.UniqueID),
					map[string]any{"entity": entity.UniqueID})
			}
		}
	}
	return nil
}

// effectiveAssignments is the role each entity of the registry has right now,
// stored or detected, which ApplyChainChange needs to move roles.
func effectiveAssignments(reg *registry.Registry, assignments map[string]energy.Assignment) map[string]energy.Assignment {
	resolver := energy.NewResolver(assignments)
	out := map[string]energy.Assignment{}
	for _, device := range reg.Snapshot() {
		for _, entity := range device.Entities {
			if resolved, ok := resolver.Resolve(entity); ok {
				out[entity.UniqueID] = resolved.Assignment
			}
		}
	}
	return out
}

// handleEnergyChains serves POST /api/v1/energy/chains and PUT or DELETE
// /api/v1/energy/chains/{id}. Every answer is the saved energy config.
func handleEnergyChains(store *settings.Store, resolver *energy.Resolver, manager *auth.Manager, reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			writeError(w, http.StatusNotImplemented, "energy_roles_unavailable", "Energie-Rollen sind ohne Datenverzeichnis nicht verfügbar")
			return
		}
		id, hasID := strings.CutPrefix(r.URL.Path, energyChainsItemPrefix)
		if !hasID {
			id = ""
		}
		switch {
		case r.URL.Path == energyChainsPath && r.Method == http.MethodPost:
		case hasID && id != "" && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		default:
			methodNotAllowed(w)
			return
		}
		if !requireEnergyMutation(w, r, manager) {
			return
		}
		defer r.Body.Close()

		var body chainBody
		if r.Method != http.MethodDelete {
			if err := decodeBody(r, &body); err != nil {
				writeErrorDetail(w, http.StatusBadRequest, "energy_roles_rejected", err)
				return
			}
			body.Label = strings.TrimSpace(body.Label)
			if body.Counting == "" && len(body.Points) > 0 {
				body.Counting = body.Points[0]
			}
		}

		notFound := false
		value, err := store.PatchEnergy(func(current *settings.EnergyConfig) error {
			if current.Chains == nil {
				current.Chains = map[string]energy.Chain{}
			}
			var prev, next *energy.Chain
			chainID := id
			if r.Method == http.MethodPost {
				chainID = chainSlug(body.Label, current.Chains)
			} else {
				existing, ok := current.Chains[id]
				if !ok {
					notFound = true
					return uierror.New("error.chain_not_found", "energy: measurement chain not found", nil)
				}
				prev = &existing
			}
			if r.Method != http.MethodDelete {
				if err := checkChainPoints(reg, body.Points); err != nil {
					return err
				}
				next = &energy.Chain{Label: body.Label, Points: body.Points, Counting: body.Counting, Direction: body.Direction}
			}

			assignments, needed := energy.ApplyChainChange(current.Assignments, effectiveAssignments(reg, current.Assignments), prev, next)
			current.Assignments = assignments
			if next != nil {
				current.Chains[chainID] = *next
			} else {
				delete(current.Chains, chainID)
			}
			for _, point := range needed {
				if !slices.Contains(current.RoleNeeded, point) {
					current.RoleNeeded = append(current.RoleNeeded, point)
				}
			}
			return checkEnergyModel(reg, *current)
		})
		if notFound {
			writeErrorKey(w, http.StatusNotFound, "chain_not_found", "error.chain_not_found", nil, "Diese Messkette gibt es nicht mehr.")
			return
		}
		if err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "energy_roles_rejected", err)
			return
		}
		applyEnergyConfig(resolver, value)
		writeJSON(w, value)
	}
}
