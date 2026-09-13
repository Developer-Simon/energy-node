package faults_test

import (
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/faults"
)

func TestLookupFindsAKnownCode(t *testing.T) {
	entry, ok := faults.Lookup("PIP_EXTERNALLY_MANAGED")
	if !ok {
		t.Fatalf("expected PIP_EXTERNALLY_MANAGED to be known")
	}
	if entry.Code != "PIP_EXTERNALLY_MANAGED" {
		t.Fatalf("unexpected code echoed back: %q", entry.Code)
	}
	if entry.Message == "" || entry.Remediation == "" {
		t.Fatalf("entry must carry both a message and a remediation: %+v", entry)
	}
	if !strings.Contains(entry.Remediation, "--break-system-packages") {
		t.Fatalf("remediation should name the actual fix: %+v", entry)
	}
}

func TestLookupReportsAnUnknownCode(t *testing.T) {
	if _, ok := faults.Lookup("SOME_FUTURE_CODE"); ok {
		t.Fatalf("expected an unrecognised code to report ok=false")
	}
}

func TestUnknownStillCarriesTheCode(t *testing.T) {
	entry := faults.Unknown("SOME_FUTURE_CODE")
	if entry.Code != "SOME_FUTURE_CODE" {
		t.Fatalf("Unknown must echo the code it was given: %+v", entry)
	}
	if entry.Message == "" {
		t.Fatalf("Unknown must still produce a readable message")
	}
}

// TestEveryCodeHasBothFields guards against a copy-paste entry that carries
// a code but forgot its text -- a silent empty string would otherwise only
// surface the first time that particular step actually failed.
func TestEveryCodeHasBothFields(t *testing.T) {
	for _, code := range faults.Codes() {
		entry, ok := faults.Lookup(string(code))
		if !ok {
			t.Fatalf("Codes() returned %q but Lookup does not know it", code)
		}
		if entry.Message == "" {
			t.Errorf("%s: empty Message", code)
		}
		if entry.Remediation == "" {
			t.Errorf("%s: empty Remediation", code)
		}
	}
}

// TestCatalogCoversTheStableCodeInventory is a literal transcription of the
// fault codes named across Plan A, Plan A-II and Plan B-I's own Vertrag 4 --
// a code missing here would fail silently as Unknown the first time that
// step actually failed on a real node.
func TestCatalogCoversTheStableCodeInventory(t *testing.T) {
	want := []string{
		"APT_UPDATE_FAILED", "APT_INSTALL_FAILED",
		"MOSQUITTO_CONF_FOREIGN", "MOSQUITTO_PASSWD_FAILED", "MOSQUITTO_ARGS_MISSING",
		"UFW_FAILED",
		"TAILSCALE_TARBALL_MISSING", "TAILSCALE_FLAG_INVALID", "TAILSCALE_INSTALL_FAILED",
		"BUNDLE_INCOMPLETE", "WHEELS_MISSING", "ARCH_MISMATCH", "PYTHON_ABI_MISMATCH",
		"PIP_EXTERNALLY_MANAGED", "PIP_INSTALL_FAILED",
		"DASHBOARD_BINARY_MISSING", "CONFIG_TEMPLATE_MISSING", "MANIFESTS_MISSING",
		"SUDOERS_INVALID", "SECRET_FILE_MISSING", "DASHBOARD_START_FAILED",
		"CADDY_BINARY_MISSING", "CADDY_CONFIG_INVALID", "CADDY_START_FAILED",
		"SERVICE_SOURCE_MISSING", "SERVICE_UNIT_FAILED", "SERVICE_START_FAILED",
		"BUNDLE_MANIFEST_MISSING", "BUNDLE_SIGNATURE_INVALID", "BUNDLE_HASH_MISMATCH",
	}
	for _, code := range want {
		if _, ok := faults.Lookup(code); !ok {
			t.Errorf("missing catalog entry for %s", code)
		}
	}
	if len(faults.Codes()) != len(want) {
		t.Errorf("catalog has %d entries, expected exactly %d known codes (found an extra or a typo?)", len(faults.Codes()), len(want))
	}
}
