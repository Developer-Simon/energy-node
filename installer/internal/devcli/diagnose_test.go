package devcli_test

import (
	"bytes"
	"context"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/devcli"
	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

const fakeDiagnoseForDevcli = `#!/bin/sh
cat <<JSON
{
  "bundle_version": "$EN_BUNDLE_VERSION",
  "steps": {},
  "units": {"energy-node-dashboard.service": "inactive"},
  "ports": {},
  "config": {"config.json": true, "manifests": []},
  "tailscale": {"angemeldet": true}
}
JSON
`

func TestRunDiagnosePrintsAChecklistWithARetryHint(t *testing.T) {
	requireSFTPServerForFetch(t)
	sshd := transporttest.Start(t)
	client := dialForFetchTest(t, sshd)

	bundleDir := t.TempDir()
	manifestJSON := `{"version":"v0.4.0","steps":[{"id":"70","optional":true}]}`
	if err := client.UploadBytes([]byte(manifestJSON), path.Join(bundleDir, "manifest.json"), 0o644); err != nil {
		t.Fatalf("uploading manifest.json: %v", err)
	}
	if err := client.UploadBytes([]byte(fakeDiagnoseForDevcli), path.Join(bundleDir, "bootstrap", "diagnose.sh"), 0o755); err != nil {
		t.Fatalf("uploading fake diagnose.sh: %v", err)
	}

	var out bytes.Buffer
	err := devcli.RunDiagnose(context.Background(), devcli.DiagnoseArgs{
		Client: client, RemoteBundleDir: bundleDir, RemoteStateDir: filepath.Join(t.TempDir(), "state"),
		Stdout: &out,
	})
	if err != nil {
		t.Fatalf("RunDiagnose: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "FAIL") || !strings.Contains(got, "energy-node-dashboard.service") {
		t.Fatalf("expected a failing energy-node-dashboard.service check, got:\n%s", got)
	}
	if !strings.Contains(got, "--only dashboard") {
		t.Fatalf("expected a retry hint naming --only dashboard, got:\n%s", got)
	}
	if !strings.Contains(got, "v0.4.0") {
		t.Fatalf("expected the report to echo the installed manifest's version, got:\n%s", got)
	}
}

func TestRunDiagnoseReportsAMissingInstallation(t *testing.T) {
	requireSFTPServerForFetch(t)
	sshd := transporttest.Start(t)
	client := dialForFetchTest(t, sshd)

	err := devcli.RunDiagnose(context.Background(), devcli.DiagnoseArgs{
		Client:          client,
		RemoteBundleDir: filepath.Join(t.TempDir(), "no-such-bundle"),
		RemoteStateDir:  filepath.Join(t.TempDir(), "state"),
		Stdout:          &bytes.Buffer{},
	})
	if err == nil {
		t.Fatalf("expected an error when no manifest.json exists on the node")
	}
}
