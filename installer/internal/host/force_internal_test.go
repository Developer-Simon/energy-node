package host

import (
	"testing"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func TestForceStampsCoversInstallAndRepairOnly(t *testing.T) {
	cases := []struct {
		name string
		req  hostapi.RunRequest
		want bool
	}{
		{"first setup runs every step", hostapi.RunRequest{Mode: hostapi.ModeInstall}, true},
		{"a single install step is not forced", hostapi.RunRequest{Mode: hostapi.ModeInstall, Only: "20"}, false},
		{"a single step repair is forced", hostapi.RunRequest{Mode: hostapi.ModeRepair, Only: "20"}, true},
		{"a redeploy does not force", hostapi.RunRequest{Mode: hostapi.ModeRedeploy}, false},
		{"prepare does not force", hostapi.RunRequest{Mode: hostapi.ModePrepare}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := forceStamps(c.req); got != c.want {
				t.Fatalf("forceStamps(%+v) = %v, want %v", c.req, got, c.want)
			}
		})
	}
}
