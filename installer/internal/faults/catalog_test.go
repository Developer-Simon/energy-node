package faults_test

import (
	"strings"
	"testing"

	webui "github.com/Developer-Simon/energy-node-webui"
	"github.com/Developer-Simon/energy-node-webui/i18n"

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
		"APT_FAILED",
		"APT_INSTALL_FAILED",
		"APT_UPDATE_FAILED",
		"ARCH_MISMATCH",
		"BUNDLE_HASH_MISMATCH",
		"BUNDLE_INCOMPLETE",
		"BUNDLE_MANIFEST_MISSING",
		"BUNDLE_SIGNATURE_INVALID",
		"CADDY_BINARY_MISSING",
		"CADDY_CONFIG_INVALID",
		"CADDY_START_FAILED",
		"CADDY_VALIDATE_FAILED",
		"CONFIG_EXISTS",
		"CONFIG_JSON_MISSING",
		"CONFIG_TEMPLATE_MISSING",
		"CONFIG_WRITE_FAILED",
		"DASHBOARD_BINARY_MISSING",
		"DASHBOARD_START_FAILED",
		"MANIFESTS_MISSING",
		"MANIFEST_MISSING",
		"MANIFEST_PARSE_FAILED",
		"MOSQUITTO_ARGS_MISSING",
		"MOSQUITTO_CONFIG_INVALID",
		"MOSQUITTO_CONF_FOREIGN",
		"MOSQUITTO_PASSWD_FAILED",
		"MQTT_CONFIG_UNREADABLE",
		"PIP_EXTERNALLY_MANAGED",
		"PIP_INSTALL_FAILED",
		"PYTHON_ABI_MISMATCH",
		"SECRET_FILE_MISSING",
		"SELECTION_UNREADABLE",
		"SERVICE_SOURCE_MISSING",
		"SERVICE_START_FAILED",
		"SERVICE_UNIT_FAILED",
		"SUDOERS_INVALID",
		"SUDO_REQUIRED",
		"TAILSCALE_FLAG_INVALID",
		"TAILSCALE_INSTALL_FAILED",
		"TAILSCALE_TARBALL_MISSING",
		"TARGET_INVALID",
		"UFW_FAILED",
		"UFW_MISSING",
		"UNIT_START_FAILED",
		"UPDATER_PATH_START_FAILED",
		"WHEELS_MISSING",
		"WHEEL_MISSING",
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

func TestEveryCodeHasTextInEveryShippedCatalog(t *testing.T) {
	set, err := i18n.Load(webui.Catalogs())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, lang := range set.Languages() {
		for _, code := range faults.Codes() {
			for _, suffix := range []string{"message", "remediation"} {
				key := "fault." + string(code) + "." + suffix
				text, ok := set.Lookup(lang, key)
				if !ok || text == "" {
					t.Errorf("catalog %s is missing %s", lang, key)
				}
			}
		}
	}
}

func TestLookupFollowsTheActiveLanguage(t *testing.T) {
	t.Cleanup(func() { faults.SetLanguage("en") })

	faults.SetLanguage("en")
	english, ok := faults.Lookup("PIP_EXTERNALLY_MANAGED")
	if !ok {
		t.Fatalf("PIP_EXTERNALLY_MANAGED is not in the catalog")
	}

	faults.SetLanguage("de")
	german, ok := faults.Lookup("PIP_EXTERNALLY_MANAGED")
	if !ok {
		t.Fatalf("PIP_EXTERNALLY_MANAGED is not in the German catalog")
	}
	if german.Message == english.Message {
		t.Errorf("the German message equals the English one: %q", german.Message)
	}
	if german.Code != english.Code {
		t.Errorf("the code must not depend on the language: %q vs %q", german.Code, english.Code)
	}
}

func TestUnknownCodeStaysUntranslatedButUsable(t *testing.T) {
	entry := faults.Unknown("SOMETHING_NEW")
	if entry.Code != "SOMETHING_NEW" {
		t.Errorf("Code = %q, want the code to survive verbatim", entry.Code)
	}
	if entry.Message == "" {
		t.Errorf("an unknown code must still carry a message the CLI can print")
	}
}
