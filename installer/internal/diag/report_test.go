package diag_test

import (
	"context"
	"path"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/diag"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

func requireSFTPServerForDiag(t *testing.T) {
	t.Helper()
	if _, ok := transporttest.SFTPServerPath(); !ok {
		t.Skip("no sftp-server binary found; install openssh-server to run this test")
	}
}

func dialForDiagTest(t *testing.T, sshd *transporttest.SSHD) *transport.Client {
	t.Helper()
	store := transport.NewHostKeyStore(path.Join(t.TempDir(), "known_hosts"))
	callback, err := store.Callback(func(string, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}
	client, err := transport.Dial(context.Background(), transport.Config{
		Host:            sshd.Addr,
		User:            sshd.User(),
		PrivateKeyPEM:   sshd.ClientKeyPEM,
		HostKeyCallback: callback,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

const fakeDiagnoseScript = `#!/bin/sh
cat <<JSON
{
  "bundle_version": "$EN_BUNDLE_VERSION",
  "steps": {"10": "v0.2.0"},
  "units": {"mosquitto.service": "active", "caddy.service": "inactive"},
  "ports": {"1883": true, "8080": false},
  "config": {"config.json": true, "manifests": ["shelly", "tuya"]},
  "tailscale": {"angemeldet": true}
}
JSON
`

func TestRunParsesTheDiagnoseReport(t *testing.T) {
	requireSFTPServerForDiag(t)
	sshd := transporttest.Start(t)
	client := dialForDiagTest(t, sshd)

	bundleDir := t.TempDir()
	scriptPath := path.Join(bundleDir, "bootstrap", "diagnose.sh")
	if err := client.UploadBytes([]byte(fakeDiagnoseScript), scriptPath, 0o755); err != nil {
		t.Fatalf("uploading fake diagnose.sh: %v", err)
	}

	report, err := diag.Run(context.Background(), client, bundleDir, "/var/lib/energy-node-installer", "v0.2.0")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if report.BundleVersion != "v0.2.0" {
		t.Errorf("unexpected bundle version: %q", report.BundleVersion)
	}
	if report.Steps["10"] != "v0.2.0" {
		t.Errorf("unexpected step stamp: %+v", report.Steps)
	}
	if report.Units["mosquitto.service"] != "active" || report.Units["caddy.service"] != "inactive" {
		t.Errorf("unexpected units: %+v", report.Units)
	}
	if !report.Ports["1883"] || report.Ports["8080"] {
		t.Errorf("unexpected ports: %+v", report.Ports)
	}
	if !report.Config.ConfigJSON {
		t.Errorf("expected config.json to be reported present")
	}
	if len(report.Config.Manifests) != 2 {
		t.Errorf("unexpected manifests: %+v", report.Config.Manifests)
	}
	if !report.Tailscale.Angemeldet {
		t.Errorf("expected tailscale to be reported logged in")
	}
}

func TestRunReportsATransportFailure(t *testing.T) {
	requireSFTPServerForDiag(t)
	sshd := transporttest.Start(t)
	client := dialForDiagTest(t, sshd)

	// No diagnose.sh was ever uploaded to this bundle directory.
	_, err := diag.Run(context.Background(), client, t.TempDir(), "/var/lib/energy-node-installer", "v0.2.0")
	if err == nil {
		t.Fatalf("expected an error when diagnose.sh does not exist on the node")
	}
}
