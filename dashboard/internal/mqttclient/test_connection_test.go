package mqttclient

import (
	"testing"
	"time"
)

func TestConnectionRefusedOnClosedPort(t *testing.T) {
	cfg := Config{
		Host:        "127.0.0.1",
		Port:        "1",
		ClientID:    "test-client",
		Username:    "",
		Password:    "",
		TLS:         false,
		TLSInsecure: false,
	}
	result := TestConnection(cfg, 2*time.Second)
	if result.OK != false {
		t.Errorf("expected OK=false, got OK=%v", result.OK)
	}
	if result.ErrorCode != "connection_refused" {
		t.Errorf("expected ErrorCode=connection_refused, got ErrorCode=%s", result.ErrorCode)
	}
	if result.Detail == "" {
		t.Errorf("expected Detail to be non-empty, got empty string")
	}
	if result.Detail != result.Message {
		t.Errorf("expected Detail==Message, got Detail=%q Message=%q", result.Detail, result.Message)
	}
}
