package steps

import "testing"

func TestMosquittoArgsQuoteUserAndPath(t *testing.T) {
	got := mosquittoArgs("energy node", "/tmp/energy-node-installer-mqtt-ab12.pw")
	want := " --user 'energy node' --password-file '/tmp/energy-node-installer-mqtt-ab12.pw'"
	if got != want {
		t.Errorf("mosquittoArgs = %q, want %q", got, want)
	}
}
