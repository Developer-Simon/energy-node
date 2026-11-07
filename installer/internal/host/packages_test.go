package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/bundlesource"
	"github.com/Developer-Simon/energy-node-installer/internal/selection"
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

// packageSeams fakes the node side of the package question: which packages
// are installed, what the repository install does, and what it recorded.
type packageSeams struct {
	installed []string
	repoErr   error
	repoCalls []string
}

// stubMissingPackage prepares a host whose resolver reports the given
// packages as missing, with the vendor's update mode in update.
func stubMissingPackage(t *testing.T, missing []string, update string) (*Host, *staged, *packageSeams) {
	t.Helper()
	h := hostWithBundled(t)
	rec := stubSeams(t, "armv6l")
	seams := &packageSeams{}

	origResolve, origInstalled, origRepo := resolveBundle, packageInstalled, installFromRepo
	t.Cleanup(func() {
		resolveBundle, packageInstalled, installFromRepo = origResolve, origInstalled, origRepo
	})
	resolveBundle = func(context.Context, *bundlesource.Resolver, bundlesource.Request) (*bundlesource.Resolved, error) {
		m := &bundle.Manifest{
			Version: "v1.4.2", Arch: "armv6", PythonABI: "cp311",
			Packages: map[string]bundle.PackagePin{},
			Steps: []bundle.StepEntry{
				{ID: "40", Optional: true, Default: true},
				{ID: "70", Optional: true, Default: true},
			},
		}
		for _, name := range missing {
			m.Packages[name] = bundle.PackagePin{Update: update}
		}
		return &bundlesource.Resolved{Dir: t.TempDir(), Manifest: m, MissingPackages: missing, Cleanup: func() {}}, nil
	}
	packageInstalled = func(_ context.Context, _ *transport.Client, name string) bool {
		for _, have := range seams.installed {
			if have == name {
				return true
			}
		}
		return false
	}
	installFromRepo = func(_ context.Context, _ *transport.Client, _, name string) error {
		seams.repoCalls = append(seams.repoCalls, name)
		return seams.repoErr
	}
	return h, rec, seams
}

func prepareWithChoices(h *Host, choices map[string]string) error {
	return h.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModePrepare, PackageChoices: choices}, &recordingSink{})
}

func requireUnavailable(t *testing.T, err error, names, repoPossible string) {
	t.Helper()
	var apiErr *hostapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "PACKAGE_UNAVAILABLE" {
		t.Fatalf("err = %v, want PACKAGE_UNAVAILABLE", err)
	}
	if apiErr.Status != 409 {
		t.Errorf("status = %d, want 409", apiErr.Status)
	}
	if apiErr.Args["names"] != names || apiErr.Args["repo_possible"] != repoPossible {
		t.Errorf("args = %v, want names %q and repo_possible %q", apiErr.Args, names, repoPossible)
	}
}

func TestPrepareAsksForAMissingPackage(t *testing.T) {
	h, rec, _ := stubMissingPackage(t, []string{"caddy"}, "apt")
	err := prepareWithChoices(h, nil)
	requireUnavailable(t, err, "caddy", "true")
	if rec.markCalls != 0 {
		t.Errorf("markVerified called %d times, want none before the answer", rec.markCalls)
	}
}

func TestPrepareDoesNotAskWhenThePackageIsInstalled(t *testing.T) {
	h, rec, seams := stubMissingPackage(t, []string{"caddy"}, "apt")
	seams.installed = []string{"caddy"}
	if err := prepareWithChoices(h, nil); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if rec.markCalls != 1 {
		t.Errorf("markVerified calls = %d, want 1", rec.markCalls)
	}
}

func TestPrepareDoesNotAskWhenTheStepIsDeselected(t *testing.T) {
	h, rec, _ := stubMissingPackage(t, []string{"caddy"}, "apt")
	h.pending = &selection.Selection{Steps: map[string]bool{"70": false}}
	if err := prepareWithChoices(h, nil); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if rec.markCalls != 1 {
		t.Errorf("markVerified calls = %d, want 1", rec.markCalls)
	}
}

func TestSkipDeselectsTheStep(t *testing.T) {
	h, _, _ := stubMissingPackage(t, []string{"caddy"}, "apt")
	if err := prepareWithChoices(h, map[string]string{"caddy": "skip"}); err != nil {
		t.Fatalf("prepare with skip: %v", err)
	}
	if h.pending == nil || h.pending.Steps["70"] {
		t.Fatalf("pending = %+v, want step 70 deselected", h.pending)
	}
}

func TestRepoChoiceInstallsOnTheNode(t *testing.T) {
	h, rec, seams := stubMissingPackage(t, []string{"caddy"}, "apt")
	if err := prepareWithChoices(h, map[string]string{"caddy": "repo"}); err != nil {
		t.Fatalf("prepare with repo: %v", err)
	}
	if len(seams.repoCalls) != 1 || seams.repoCalls[0] != "caddy" {
		t.Errorf("repo installs = %v, want [caddy]", seams.repoCalls)
	}
	if rec.markCalls != 1 {
		t.Errorf("markVerified calls = %d, want 1", rec.markCalls)
	}
}

func TestRepoChoiceReportsAFailedInstall(t *testing.T) {
	h, _, seams := stubMissingPackage(t, []string{"caddy"}, "apt")
	seams.repoErr = errors.New("apt-get exited 100")
	var apiErr *hostapi.Error
	if err := prepareWithChoices(h, map[string]string{"caddy": "repo"}); !errors.As(err, &apiErr) || apiErr.Code != "PACKAGE_REPO_FAILED" {
		t.Fatalf("err = %v, want PACKAGE_REPO_FAILED", err)
	}
}

func TestRepoChoiceIsRejectedOnArmv6(t *testing.T) {
	h, _, seams := stubMissingPackage(t, []string{"caddy"}, "installer")
	err := prepareWithChoices(h, map[string]string{"caddy": "repo"})
	requireUnavailable(t, err, "caddy", "false")
	if len(seams.repoCalls) != 0 {
		t.Errorf("repo installs = %v, want none for update=installer", seams.repoCalls)
	}
}

func TestPackageChoiceIsRememberedForTheSession(t *testing.T) {
	h, rec, _ := stubMissingPackage(t, []string{"caddy"}, "apt")
	if err := prepareWithChoices(h, map[string]string{"caddy": "skip"}); err != nil {
		t.Fatalf("first prepare: %v", err)
	}
	if err := prepareWithChoices(h, nil); err != nil {
		t.Fatalf("second prepare without a choice must not ask again: %v", err)
	}
	if rec.markCalls != 2 {
		t.Errorf("markVerified calls = %d, want 2", rec.markCalls)
	}
	if h.packageChoices["caddy"] != "skip" {
		t.Errorf("remembered choices = %v, want caddy skip", h.packageChoices)
	}
}
