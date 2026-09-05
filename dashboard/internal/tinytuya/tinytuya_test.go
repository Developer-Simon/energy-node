package tinytuya

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeHelper(t *testing.T, output string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "helper.sh")
	content := "#!/bin/sh\nprintf '%s' '" + output + "'\n"
	if err := os.WriteFile(path, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestClientRunsHelperForDevicesAndStatus(t *testing.T) {
	helper := writeHelper(t, `{"ok":true,"devices":[{"device_id":"device-1","name":"Test","local_key":"key","ip":"192.0.2.10","version":3.3}],"status":{"dps":[]}}`)
	client := NewClient("/bin/sh", helper, time.Second)
	devices, err := client.Devices(context.Background(), CloudRequest{Region: "eu", AccessID: "id", AccessSecret: "secret"})
	if err != nil || len(devices) != 1 || devices[0].DeviceID != "device-1" {
		t.Fatalf("devices = %#v, err = %v", devices, err)
	}
	status, err := client.Status(context.Background(), StatusRequest{DeviceID: "device-1", LocalKey: "key", IP: "192.0.2.10", Version: 3.3})
	if err != nil || status.DPS == nil {
		t.Fatalf("status = %#v, err = %v", status, err)
	}
}

func TestClientRedactsSecretBearingHelperErrors(t *testing.T) {
	helper := writeHelper(t, `{"ok":false,"error":"access secret is invalid"}`)
	client := NewClient("/bin/sh", helper, time.Second)
	_, err := client.Devices(context.Background(), CloudRequest{Region: "eu", AccessID: "id", AccessSecret: "secret"})
	if err == nil || strings.Contains(strings.ToLower(err.Error()), "secret") {
		t.Fatalf("error = %v", err)
	}
}

func TestClientTimesOutHelper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	client := NewClient("/bin/sh", path, 10*time.Millisecond)
	_, err := client.Devices(context.Background(), CloudRequest{Region: "eu", AccessID: "id", AccessSecret: "secret"})
	if err == nil || !strings.Contains(err.Error(), "Zeitlimit") {
		t.Fatalf("error = %v", err)
	}
}
