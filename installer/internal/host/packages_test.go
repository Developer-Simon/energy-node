package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-installer/internal/bundlesource"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// dialPackagesTest connects to a throwaway sshd on this machine, so the
// staging code runs its real sftp and shell commands.
func dialPackagesTest(t *testing.T) *transport.Client {
	t.Helper()
	if _, ok := transporttest.SFTPServerPath(); !ok {
		t.Skip("no sftp-server binary found; install openssh-sftp-server to run this test")
	}
	sshd := transporttest.Start(t)
	store := transport.NewHostKeyStore(filepath.Join(t.TempDir(), "known_hosts"))
	callback, err := store.Callback(func(string, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := transport.Dial(ctx, transport.Config{
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

// localPin writes content into dir and describes it as a pinned file.
func localPin(t *testing.T, dir, name, content string) bundlesource.LocalPackage {
	t.Helper()
	p := filepath.Join(dir, "cache-"+name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	return bundlesource.LocalPackage{Name: name, File: name, SHA256: hex.EncodeToString(sum[:]), Path: p}
}

func TestPrepareStagesPackagesAfterVerifying(t *testing.T) {
	stubSeams(t, "armv6l")
	h := hostWithBundled(t)
	var order []string
	origStage, origVerify, origMark := stagePackages, verifyStaged, markVerified
	t.Cleanup(func() { stagePackages, verifyStaged, markVerified = origStage, origVerify, origMark })
	verifyStaged = func(context.Context, *transport.Client, string, bool, bool) (bool, error) {
		order = append(order, "verify")
		return false, nil
	}
	stagePackages = func(_ context.Context, _ *transport.Client, remoteDir string, _ []bundlesource.LocalPackage, _ func(int64, int64)) error {
		order = append(order, "stage:"+remoteDir)
		return nil
	}
	markVerified = func(context.Context, *transport.Client, string) error {
		order = append(order, "mark")
		return nil
	}

	if err := h.SelectPackage(context.Background(), hostapi.PackageSelection{Kind: "bundled"}); err != nil {
		t.Fatalf("SelectPackage: %v", err)
	}
	if err := h.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModePrepare, ForceFullTransfer: true}, &recordingSink{}); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	want := []string{"verify", "stage:/var/lib/energy-node-installer/packages", "stage:/var/lib/energy-node-installer/wheels", "mark"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestStagePackagesSkipsFilesWithTheSameHash(t *testing.T) {
	client := dialPackagesTest(t)
	local := t.TempDir()
	remote := filepath.Join(t.TempDir(), "packages")
	pkgs := []bundlesource.LocalPackage{localPin(t, local, "tailscale_1.0_arm.deb", "deb-bytes")}
	ctx := context.Background()

	if err := defaultStagePackages(ctx, client, remote, pkgs, nil); err != nil {
		t.Fatalf("first stage: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(remote, pkgs[0].File))
	if err != nil || string(got) != "deb-bytes" {
		t.Fatalf("staged file = %q, %v", got, err)
	}
	if info, _ := os.Stat(filepath.Join(remote, pkgs[0].File)); info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", info.Mode().Perm())
	}

	// The second run must not upload again: the progress callback only
	// fires while bytes are being sent.
	uploads := 0
	if err := defaultStagePackages(ctx, client, remote, pkgs, func(int64, int64) { uploads++ }); err != nil {
		t.Fatalf("second stage: %v", err)
	}
	if uploads != 0 {
		t.Errorf("second run uploaded %d progress steps, want none", uploads)
	}

	// A file with the right name but other bytes is replaced.
	if err := os.WriteFile(filepath.Join(remote, pkgs[0].File), []byte("damaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := defaultStagePackages(ctx, client, remote, pkgs, nil); err != nil {
		t.Fatalf("third stage: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(remote, pkgs[0].File)); string(got) != "deb-bytes" {
		t.Errorf("damaged file was not replaced, content = %q", got)
	}
}

func TestStagePackagesRemovesStaleFiles(t *testing.T) {
	client := dialPackagesTest(t)
	local := t.TempDir()
	remote := filepath.Join(t.TempDir(), "wheels")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"old-1.0-py3-none-any.whl", "leftover.tmp-abc123"} {
		if err := os.WriteFile(filepath.Join(remote, name), []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pkgs := []bundlesource.LocalPackage{localPin(t, local, "new-2.0-py3-none-any.whl", "wheel-bytes")}

	if err := defaultStagePackages(context.Background(), client, remote, pkgs, nil); err != nil {
		t.Fatalf("stage: %v", err)
	}
	for _, gone := range []string{"old-1.0-py3-none-any.whl", "leftover.tmp-abc123"} {
		if _, err := os.Stat(filepath.Join(remote, gone)); !os.IsNotExist(err) {
			t.Errorf("%s still on the node: %v", gone, err)
		}
	}
	if _, err := os.Stat(filepath.Join(remote, "new-2.0-py3-none-any.whl")); err != nil {
		t.Errorf("pinned wheel missing: %v", err)
	}
}
