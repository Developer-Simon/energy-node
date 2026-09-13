package hostapi_test

import (
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func TestRedactorReplacesEverySecretOccurrence(t *testing.T) {
	r := hostapi.NewRedactor("hunter2", "s3cret")
	got := r.Line("mosquitto_passwd -b file user hunter2 && echo s3cret hunter2")
	if strings.Contains(got, "hunter2") || strings.Contains(got, "s3cret") {
		t.Fatalf("the redacted line still carries a secret: %q", got)
	}
	if !strings.Contains(got, "mosquitto_passwd") {
		t.Errorf("the redactor swallowed the surrounding text: %q", got)
	}
}

func TestRedactorIgnoresEmptyAndVeryShortSecrets(t *testing.T) {
	// Ein leeres oder einzeichiges Geheimnis wuerde jede Zeile zu Sternen
	// machen. Das waere kein Schutz, sondern ein unlesbares Log.
	r := hostapi.NewRedactor("", "a")
	line := "a plain log line"
	if got := r.Line(line); got != line {
		t.Errorf("Line() = %q, want the line untouched", got)
	}
}

func TestRedactorWithoutSecretsIsTheIdentity(t *testing.T) {
	r := hostapi.NewRedactor()
	line := "##STEP 10 ok"
	if got := r.Line(line); got != line {
		t.Errorf("Line() = %q, want %q", got, line)
	}
}
