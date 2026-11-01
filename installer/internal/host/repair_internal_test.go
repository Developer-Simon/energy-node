package host

import (
	"reflect"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func TestRepairListPairsTheBrokerAndDashboardSteps(t *testing.T) {
	manifest := bundle.Manifest{Steps: []bundle.StepEntry{{ID: "20"}, {ID: "40"}, {ID: "60"}}}
	cases := []struct {
		name string
		req  hostapi.RunRequest
		want []string
	}{
		{"repairing 60 with a new password runs the pair", hostapi.RunRequest{Mode: hostapi.ModeRepair, Only: "60", MQTTPassword: "neu"}, []string{"20", "60"}},
		{"repairing 60 without a password runs only 60", hostapi.RunRequest{Mode: hostapi.ModeRepair, Only: "60"}, []string{"60"}},
		{"repairing 40 with a password runs only 40", hostapi.RunRequest{Mode: hostapi.ModeRepair, Only: "40", MQTTPassword: "neu"}, []string{"40"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			for _, entry := range repairList(manifest, c.req) {
				got = append(got, entry.ID)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("repairList ids = %v, want %v", got, c.want)
			}
		})
	}
}
