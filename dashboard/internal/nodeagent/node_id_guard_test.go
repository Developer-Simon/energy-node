package nodeagent

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// TestNodeIDHasNoHyphenInMQTTContext haelt die Node-ID-Normalisierung
// (Spec V6/V7) dauerhaft: energy_node im MQTT-/Discovery-Kontext, energy-node
// nur noch in Pfaden.
func TestNodeIDHasNoHyphenInMQTTContext(t *testing.T) {
	out, err := exec.Command("git", "-C", "../../..", "grep", "-n", "energy-node",
		"--", "services/", "libs/", "dashboard/",
		":!:dashboard/internal/nodeagent/node_id_guard_test.go",
		// The one-time legacy-cleanup path (nodeagent.LegacyCleanupMessages)
		// deliberately and permanently asserts the pre-rename hyphenated topics
		// (outstation/energy-node/state etc.). The production code in
		// discovery.go dodges this guard via string concatenation, but
		// discovery_test.go uses the literal, so that one test file is excluded.
		":!:dashboard/internal/nodeagent/discovery_test.go").CombinedOutput()
	if err != nil && len(out) == 0 {
		t.Fatalf("git grep failed: %v", err)
	}
	pathLike := regexp.MustCompile(`/etc/energy-node/|energy-node-dashboard|energy-node\.service|energy-node\.config\.json|energy-node-bridge`)
	mqttCtx := regexp.MustCompile(`via_device|identifiers|unique_id|outstation/energy-node|homeassistant/[a-z_]+/energy-node/`)
	var offenders []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" || pathLike.MatchString(line) {
			continue
		}
		if mqttCtx.MatchString(line) {
			offenders = append(offenders, line)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("energy-node (hyphen) in MQTT/discovery context:\n%s", strings.Join(offenders, "\n"))
	}
}
