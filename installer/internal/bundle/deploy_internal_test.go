package bundle

import "testing"

func TestVerifyFailureMessageKeepsOnlyTheFactsOfAMismatch(t *testing.T) {
	cases := []struct {
		code   FaultCode
		stderr string
		want   string
	}{
		{FaultPythonABIMismatch, "Python-ABI cp313, Bundle erwartet cp311\n", "abi=cp313 expected=cp311"},
		{FaultArchMismatch, "Architektur x86_64, Bundle erwartet armv6l, armv7l\n", "arch=x86_64 expected=armv6l, armv7l"},
		{FaultHashMismatch, "datei a.txt: Pruefsumme falsch\n", "verify_bundle.sh rejected the bundle on the node: datei a.txt: Pruefsumme falsch"},
		{FaultPythonABIMismatch, "etwas anderes\n", "verify_bundle.sh rejected the bundle on the node: etwas anderes"},
	}
	for _, c := range cases {
		if got := verifyFailureMessage(c.code, c.stderr); got != c.want {
			t.Errorf("%s %q: got %q, want %q", c.code, c.stderr, got, c.want)
		}
	}
}
