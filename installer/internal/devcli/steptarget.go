package devcli

import (
	"fmt"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
)

// fixedOnlyTargets names the two --only values with no ServiceID of their
// own -- "dashboard" and "wheels" are core steps (never optional), not
// selectable device services, so they cannot be resolved via ServiceID the
// way every other --only value is.
var fixedOnlyTargets = map[string]string{
	"dashboard": "60",
	"wheels":    "50",
}

// ResolveStepTarget turns a developer-facing --only value into the single
// bundle.StepEntry a full run would have included at that point. RunDeploy
// passes the result as the sole entry of steps.RunOptions.Steps -- the same
// mechanism a full run uses, just filtered to one entry (Plan B-I, Task 12).
func ResolveStepTarget(manifest *bundle.Manifest, only string) (bundle.StepEntry, error) {
	if id, ok := fixedOnlyTargets[only]; ok {
		entry, ok := manifest.StepByID(id)
		if !ok {
			return bundle.StepEntry{}, fmt.Errorf("bundle manifest has no step %s for --only %s", id, only)
		}
		return entry, nil
	}
	for _, entry := range manifest.Steps {
		if entry.ServiceID == only {
			return entry, nil
		}
	}
	return bundle.StepEntry{}, fmt.Errorf("unknown --only target %q: not \"dashboard\", \"wheels\", or a service in this bundle's manifest", only)
}
