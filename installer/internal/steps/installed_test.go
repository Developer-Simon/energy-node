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

func TestRecordInstalledAlsoCopiesTheChangelogWhenTheBundleHasOne(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{
		"10-apt.sh": scriptBody("10", okScript),
	})
	const changelog = `{"schema_version":1,"components":[]}`
	if err := client.UploadBytes([]byte(`{"version":"v1.2.3"}`), bundleDir+"/manifest.json", 0o644); err != nil {
		t.Fatalf("upload manifest: %v", err)
	}
	if err := client.UploadBytes([]byte(changelog), bundleDir+"/changelog.json", 0o644); err != nil {
		t.Fatalf("upload changelog: %v", err)
	}
	if err := client.Run(context.Background(), "mkdir -p '"+stateDir+"'", os.Stderr, os.Stderr); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	if err := steps.RecordInstalled(context.Background(), client, bundleDir, stateDir); err != nil {
		t.Fatalf("RecordInstalled: %v", err)
	}

	local := filepath.Join(t.TempDir(), "changelog.json")
	if err := client.DownloadFile(stateDir+"/changelog.json", local); err != nil {
		t.Fatalf("download: %v", err)
	}
	got, err := os.ReadFile(local)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != changelog {
		t.Fatalf("changelog.json = %q, want %q", got, changelog)
	}
	if err := client.DownloadFile(stateDir+"/changelog.json.tmp", filepath.Join(t.TempDir(), "x")); err == nil {
		t.Fatal("the changelog .tmp file must not be left behind")
	}
}

func TestReadInstalledManifestReturnsTheRecordedCopy(t *testing.T) {
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

	got, err := steps.ReadInstalledManifest(context.Background(), client, stateDir)
	if err != nil {
		t.Fatalf("ReadInstalledManifest: %v", err)
	}
	if string(got) != manifest {
		t.Fatalf("got %q, want %q", got, manifest)
	}
}

func TestReadInstalledManifestIsNilWhenNothingWasRecordedYet(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	_, stateDir := deployBootstrapScripts(t, client, map[string]string{
		"10-apt.sh": scriptBody("10", okScript),
	})
	if err := client.Run(context.Background(), "mkdir -p '"+stateDir+"'", os.Stderr, os.Stderr); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	got, err := steps.ReadInstalledManifest(context.Background(), client, stateDir)
	if err != nil {
		t.Fatalf("a missing copy is not an error: %v", err)
	}
	if got != nil {
		t.Fatalf("got %q, want nil", got)
	}
}

func TestRecordInstalledKeepsThePreviousManifestOnAVersionChange(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{
		"10-apt.sh": scriptBody("10", okScript),
	})
	const older = `{"version":"v1.2.2","components":{"dashboard":"v2.0.0"}}`
	const newer = `{"version":"v1.2.3","components":{"dashboard":"v2.1.0"}}`
	if err := client.Run(context.Background(), "mkdir -p '"+stateDir+"'", os.Stderr, os.Stderr); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := client.UploadBytes([]byte(older), stateDir+"/installed-manifest.json", 0o644); err != nil {
		t.Fatalf("upload old manifest: %v", err)
	}
	if err := client.UploadBytes([]byte(newer), bundleDir+"/manifest.json", 0o644); err != nil {
		t.Fatalf("upload manifest: %v", err)
	}

	if err := steps.RecordInstalled(context.Background(), client, bundleDir, stateDir); err != nil {
		t.Fatalf("RecordInstalled: %v", err)
	}

	local := filepath.Join(t.TempDir(), "previous-manifest.json")
	if err := client.DownloadFile(stateDir+"/previous-manifest.json", local); err != nil {
		t.Fatalf("download previous: %v", err)
	}
	if got, _ := os.ReadFile(local); string(got) != older {
		t.Fatalf("previous-manifest.json = %q, want %q", got, older)
	}
}

func TestRecordInstalledKeepsThePredecessorWhenTheVersionIsUnchanged(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{
		"10-apt.sh": scriptBody("10", okScript),
	})
	const same = `{"version":"v1.2.3"}`
	const predecessor = `{"version":"v1.2.1"}`
	if err := client.Run(context.Background(), "mkdir -p '"+stateDir+"'", os.Stderr, os.Stderr); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	for path, body := range map[string]string{
		stateDir + "/installed-manifest.json": same,
		stateDir + "/previous-manifest.json":  predecessor,
		bundleDir + "/manifest.json":          same,
	} {
		if err := client.UploadBytes([]byte(body), path, 0o644); err != nil {
			t.Fatalf("upload %s: %v", path, err)
		}
	}

	if err := steps.RecordInstalled(context.Background(), client, bundleDir, stateDir); err != nil {
		t.Fatalf("RecordInstalled: %v", err)
	}

	local := filepath.Join(t.TempDir(), "previous-manifest.json")
	if err := client.DownloadFile(stateDir+"/previous-manifest.json", local); err != nil {
		t.Fatalf("download previous: %v", err)
	}
	if got, _ := os.ReadFile(local); string(got) != predecessor {
		t.Fatalf("previous-manifest.json = %q, want the untouched predecessor %q", got, predecessor)
	}
}

func TestRecordInstalledKeepsEveryReceivedChangelogVersion(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{
		"10-apt.sh": scriptBody("10", okScript),
	})
	const (
		newVersion    = `{"schema_version":1,"bundle_version":"v1.2.3"}`
		changedOld    = `{"schema_version":1,"bundle_version":"v1.2.2","note":"corrected"}`
		staleOld      = `{"schema_version":1,"bundle_version":"v1.2.2","note":"before"}`
		keptOlder     = `{"schema_version":1,"bundle_version":"v1.2.1"}`
		bundleManiest = `{"version":"v1.2.3"}`
	)
	if err := client.UploadBytes([]byte(bundleManiest), bundleDir+"/manifest.json", 0o644); err != nil {
		t.Fatalf("upload manifest: %v", err)
	}
	for _, dir := range []string{bundleDir + "/changelog", stateDir + "/changelog"} {
		if err := client.Run(context.Background(), "mkdir -p '"+dir+"'", os.Stderr, os.Stderr); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	for path, body := range map[string]string{
		bundleDir + "/changelog/v1.2.3.json": newVersion,
		bundleDir + "/changelog/v1.2.2.json": changedOld,
		stateDir + "/changelog/v1.2.2.json":  staleOld,
		stateDir + "/changelog/v1.2.1.json":  keptOlder,
	} {
		if err := client.UploadBytes([]byte(body), path, 0o644); err != nil {
			t.Fatalf("upload %s: %v", path, err)
		}
	}

	if err := steps.RecordInstalled(context.Background(), client, bundleDir, stateDir); err != nil {
		t.Fatalf("RecordInstalled: %v", err)
	}

	want := map[string]string{
		"v1.2.3.json": newVersion, // new file added
		"v1.2.2.json": changedOld, // changed file replaced
		"v1.2.1.json": keptOlder,  // node-only file kept
	}
	for name, body := range want {
		local := filepath.Join(t.TempDir(), name)
		if err := client.DownloadFile(stateDir+"/changelog/"+name, local); err != nil {
			t.Fatalf("download %s: %v", name, err)
		}
		if got, _ := os.ReadFile(local); string(got) != body {
			t.Fatalf("changelog/%s = %q, want %q", name, got, body)
		}
	}
	if err := client.DownloadFile(stateDir+"/changelog/v1.2.2.json.tmp", filepath.Join(t.TempDir(), "x")); err == nil {
		t.Fatal("the per-version .tmp file must not be left behind")
	}
}

func TestRecordInstalledSucceedsWithoutAPerVersionChangelogDir(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{
		"10-apt.sh": scriptBody("10", okScript),
	})
	if err := client.UploadBytes([]byte(`{"version":"v1.2.3"}`), bundleDir+"/manifest.json", 0o644); err != nil {
		t.Fatalf("upload manifest: %v", err)
	}
	if err := client.Run(context.Background(), "mkdir -p '"+stateDir+"'", os.Stderr, os.Stderr); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := steps.RecordInstalled(context.Background(), client, bundleDir, stateDir); err != nil {
		t.Fatalf("an older bundle without changelog/ must still record: %v", err)
	}
}
