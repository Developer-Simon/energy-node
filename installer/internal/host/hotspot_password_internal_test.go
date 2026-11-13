package host

import (
	"testing"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// TestHostIgnoresHotspotPasswordOutsideInstall pins that only a first install
// hands the hotspot password to step 67. Redeploy, repair and prepare never
// pass it on, even when the request carries one.
func TestHostIgnoresHotspotPasswordOutsideInstall(t *testing.T) {
	const password = "correct horse 7"
	cases := []struct {
		name string
		req  hostapi.RunRequest
		want string
	}{
		{"install passes the password", hostapi.RunRequest{Mode: hostapi.ModeInstall, HotspotPassword: password}, password},
		{"install without a password stays empty", hostapi.RunRequest{Mode: hostapi.ModeInstall}, ""},
		{"repair ignores the password", hostapi.RunRequest{Mode: hostapi.ModeRepair, Only: "67", HotspotPassword: password}, ""},
		{"redeploy ignores the password", hostapi.RunRequest{Mode: hostapi.ModeRedeploy, HotspotPassword: password}, ""},
		{"prepare ignores the password", hostapi.RunRequest{Mode: hostapi.ModePrepare, HotspotPassword: password}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hotspotPasswordFor(c.req); got != c.want {
				t.Fatalf("hotspotPasswordFor(%+v) = %q, want %q", c.req, got, c.want)
			}
		})
	}
}
