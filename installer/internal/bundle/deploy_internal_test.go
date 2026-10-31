package bundle

import (
	"errors"
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

func TestVerifyOutcomeLetsAWrongABIPassOnlyWithConsent(t *testing.T) {
	stdout := "FEHLER PYTHON_ABI_MISMATCH\n"
	stderr := "Python-ABI cp313, Bundle erwartet cp311\n"
	exit1 := errors.New("exit status 1")

	if tolerated, err := verifyOutcome(exit1, stdout, stderr, true); !tolerated || err != nil {
		t.Errorf("with consent: tolerated=%v err=%v, want tolerated and no error", tolerated, err)
	}
	var bundleErr *Error
	if tolerated, err := verifyOutcome(exit1, stdout, stderr, false); tolerated || !errors.As(err, &bundleErr) || bundleErr.Code != FaultPythonABIMismatch {
		t.Errorf("without consent: tolerated=%v err=%v, want PYTHON_ABI_MISMATCH", tolerated, err)
	}
	// Consent covers the ABI and nothing else.
	for _, code := range []string{"BUNDLE_HASH_MISMATCH", "BUNDLE_SIGNATURE_INVALID", "ARCH_MISMATCH"} {
		if tolerated, err := verifyOutcome(exit1, "FEHLER "+code+"\n", "", true); tolerated || err == nil {
			t.Errorf("%s with consent: tolerated=%v err=%v, want a rejection", code, tolerated, err)
		}
	}
	if tolerated, err := verifyOutcome(nil, "OK\n", "", true); tolerated || err != nil {
		t.Errorf("OK: tolerated=%v err=%v, want a plain success", tolerated, err)
	}
}
