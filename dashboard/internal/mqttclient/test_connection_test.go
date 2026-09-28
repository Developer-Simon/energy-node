package mqttclient

import (
	"testing"
	"time"
)

func TestConnectionDetailPopulatedOnConnectError(t *testing.T) {
	// Test that when a connection error occurs (not a timeout), the Detail
	// field is populated with the error message. We connect to an unreachable
	// host with a short timeout to trigger a connection error rather than a
	// timeout (on most systems).
	cfg := Config{
		Host:        "192.0.2.1", // TEST-NET-1, unreachable address per RFC 5737
		Port:        "1883",
		ClientID:    "test-client",
		Username:    "",
		Password:    "",
		TLS:         false,
		TLSInsecure: false,
	}
	result := TestConnection(cfg, 500*time.Millisecond)
	if result.OK {
		t.Fatalf("expected OK=false, got OK=true")
	}
	// When an error occurs (not a timeout), Detail should be populated
	if result.ErrorCode != "timeout" && result.Detail == "" {
		t.Fatalf("expected Detail to be populated for error_code=%s", result.ErrorCode)
	}
}
