package steps_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

func TestRecordInstalledCopiesTheBundleManifestIntoTheStateDir(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{
		"10-apt.sh": scriptBody("10", okScript),
	})
	const manifest = `{"version":"v1.2.3","components":{"dashboard":"v2.1.0"}}`
	if err := client.UploadBytes([]byte(manifest), bundleDir+"/manifest.json", 0o644); err != nil {
		t.Fatalf("upload manifest: %v", err)
	}
	if err := client.Run(context.Background(), "mkdir -p '"+stateDir+"'", os.Stderr, os.Stderr); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	if err := steps.RecordInstalled(context.Background(), client, bundleDir, stateDir); err != nil {
		t.Fatalf("RecordInstalled: %v", err)
	}

	local := filepath.Join(t.TempDir(), "installed-manifest.json")
	if err := client.DownloadFile(stateDir+"/installed-manifest.json", local); err != nil {
		t.Fatalf("download: %v", err)
	}
	got, err := os.ReadFile(local)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != manifest {
		t.Fatalf("installed-manifest.json = %q, want the bundle manifest %q", got, manifest)
	}
	if err := client.DownloadFile(stateDir+"/installed-manifest.json.tmp", filepath.Join(t.TempDir(), "x")); err == nil {
		t.Fatal("the .tmp file must not be left behind")
	}
}

func TestRecordInstalledFailsWhenTheBundleHasNoManifest(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{
		"10-apt.sh": scriptBody("10", okScript),
	})
	if err := client.Run(context.Background(), "mkdir -p '"+stateDir+"'", os.Stderr, os.Stderr); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := steps.RecordInstalled(context.Background(), client, bundleDir, stateDir); err == nil {
		t.Fatal("RecordInstalled must fail when manifest.json is missing")
	}
}
