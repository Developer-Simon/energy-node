package mqttbridge

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func testConnection() settings.BridgeConnection {
	value := settings.DefaultBridgeConnection()
	value.Enabled = true
	value.Name = "aussenstandort-zu-hauptsystem"
	value.Address = "100.101.102.103"
	value.RemoteClientID = "pi-aussenstandort-bridge"
	value.RemoteUsername = "ha"
	value.Topics = []settings.BridgeTopic{
		{Pattern: "outstation/#", Direction: "both", QoS: 0},
		{Pattern: "homeassistant/#", Direction: "both", QoS: 0},
	}
	return value
}

func TestRenderMatchesGoldenFile(t *testing.T) {
	renderedAt := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	got, err := Render(Input{
		Connection: testConnection(),
		Password:   "secret-pw",
		Mask:       false,
		RenderedBy: "admin",
		RenderedAt: renderedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/golden.conf")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("rendered output does not match golden file:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderMasksPasswordInPreview(t *testing.T) {
	got, err := Render(Input{
		Connection: testConnection(),
		Password:   "secret-pw",
		Mask:       true,
		RenderedBy: "admin",
		RenderedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "secret-pw") {
		t.Fatal("masked render must never contain the real password")
	}
	if !strings.Contains(got, "remote_password "+PasswordPlaceholder) {
		t.Fatalf("masked render must show the placeholder, got:\n%s", got)
	}
}

func TestRenderOmitsPasswordLineWhenNotConfigured(t *testing.T) {
	got, err := Render(Input{
		Connection: testConnection(),
		Password:   "",
		Mask:       false,
		RenderedBy: "admin",
		RenderedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "remote_password") {
		t.Fatalf("render must omit remote_password entirely when no password is configured, got:\n%s", got)
	}
}

// TestRenderRejectsInjectionAttempts documents that Render itself is not the
// injection boundary - internal/settings.validateBridgeConnection (invoked
// here via settings.ValidateBridgeConnection as defense in depth) already
// rejects control characters and stray topic wildcards before a single byte
// is written to the .conf. This is the "even a compromised caller" half of
// the story; the "even a compromised dashboard process" half is the root
// helper's line allowlist, exercised on the Pi, not here.
func TestRenderRejectsInjectionAttempts(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(settings.BridgeConnection) settings.BridgeConnection
	}{
		{"address with embedded directive", func(v settings.BridgeConnection) settings.BridgeConnection {
			v.Address = "100.64.1.2\nlistener 1884"
			return v
		}},
		{"remote_username with embedded directive", func(v settings.BridgeConnection) settings.BridgeConnection {
			v.RemoteUsername = "ha\nlog_dest file /etc/shadow"
			return v
		}},
		{"remote_client_id with control characters", func(v settings.BridgeConnection) settings.BridgeConnection {
			v.RemoteClientID = "id\r\nuser root"
			return v
		}},
		{"topic pattern with embedded directive", func(v settings.BridgeConnection) settings.BridgeConnection {
			v.Topics = []settings.BridgeTopic{{Pattern: "a/#\nlisten 1884", Direction: "out", QoS: 0}}
			return v
		}},
		{"bare wildcard topic", func(v settings.BridgeConnection) settings.BridgeConnection {
			v.Topics = []settings.BridgeTopic{{Pattern: "#", Direction: "both", QoS: 0}}
			return v
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := Render(Input{
				Connection: testCase.mutate(testConnection()),
				Password:   "secret",
				RenderedBy: "admin",
				RenderedAt: time.Now(),
			})
			if err == nil {
				t.Fatal("expected Render to reject this connection, got nil error")
			}
		})
	}
}

func TestDirectiveChecksumIgnoresCommentsAndHeaderTimestamp(t *testing.T) {
	a, err := Render(Input{Connection: testConnection(), Password: "secret", RenderedBy: "admin", RenderedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Render(Input{Connection: testConnection(), Password: "secret", RenderedBy: "someone-else", RenderedAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("expected the two full renders to differ (different header)")
	}
	if DirectiveChecksum(a) != DirectiveChecksum(b) {
		t.Fatal("expected the directive checksum to ignore the header comment and be equal for identical connections")
	}
}
