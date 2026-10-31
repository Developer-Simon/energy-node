package bundle

import (
	"strings"
	"testing"
)

func TestVerifyFailureMessageKeepsOnlyTheFactsOfAMismatch(t *testing.T) {
	cases := []struct {
		code   FaultCode
		stderr string
		want   string
	}{
		{FaultPythonABIMismatch, "Python-ABI cp313, Bundle erwartet cp311\n", "abi=cp313 expected=cp311"},
		{FaultArchMismatch, "Architektur x86_64, Bundle erwartet armv6l, armv7l\n", "arch=x86_64 expected=armv6l, armv7l"},
		{FaultHashMismatch, "fehlt: a/b.whl\nverfaelscht: c.txt\n", "missing=a/b.whl\ntampered=c.txt"},
		{FaultHashMismatch, "manifest.json: 'files' fehlt oder ist leer\n", "files=empty"},
		{FaultHashMismatch, "datei a.txt: Pruefsumme falsch\n", "verify_bundle.sh rejected the bundle on the node: datei a.txt: Pruefsumme falsch"},
		{FaultPythonABIMismatch, "etwas anderes\n", "verify_bundle.sh rejected the bundle on the node: etwas anderes"},
	}
	for _, c := range cases {
		if got := verifyFailureMessage(c.code, c.stderr); got != c.want {
			t.Errorf("%s %q: got %q, want %q", c.code, c.stderr, got, c.want)
		}
	}
}

func TestVerifyFailureMessageCapsTheListOfDamagedFiles(t *testing.T) {
	var stderr string
	for i := 0; i < 13; i++ {
		stderr += "verfaelscht: f" + string(rune('a'+i)) + "\n"
	}
	got := verifyFailureMessage(FaultHashMismatch, stderr)
	lines := strings.Split(got, "\n")
	if len(lines) != 11 || lines[0] != "tampered=fa" || lines[10] != "more=3" {
		t.Errorf("got %d lines, first %q, last %q", len(lines), lines[0], lines[len(lines)-1])
	}
}
