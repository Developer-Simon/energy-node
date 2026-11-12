package systemactions

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type recordingRunner struct {
	name string
	args []string
}

func (runner *recordingRunner) Run(_ context.Context, name string, args ...string) error {
	runner.name = name
	runner.args = append([]string(nil), args...)
	return nil
}

func TestExecutorUsesOnlyTheAllowlistedHelperInvocation(t *testing.T) {
	runner := &recordingRunner{}
	executor := NewExecutor(runner, "/usr/local/sbin/energy-node-dashboard-system-action")
	if err := executor.Execute(context.Background(), Reboot); err != nil {
		t.Fatal(err)
	}
	if runner.name != "sudo" {
		t.Fatalf("runner command = %q, want sudo", runner.name)
	}
	want := []string{"-n", "/usr/local/sbin/energy-node-dashboard-system-action", "reboot"}
	if len(runner.args) != len(want) {
		t.Fatalf("runner args = %#v, want %#v", runner.args, want)
	}
	for index := range want {
		if runner.args[index] != want[index] {
			t.Fatalf("runner args = %#v, want %#v", runner.args, want)
		}
	}
	if err := executor.Execute(context.Background(), Action("systemctl restart anything")); err == nil {
		t.Fatal("unsupported action was accepted")
	}
}

func TestExecutorSupportsApplyAppConfig(t *testing.T) {
	runner := &recordingRunner{}
	executor := NewExecutor(runner, "/usr/local/sbin/energy-node-dashboard-system-action")
	if err := executor.Execute(context.Background(), ApplyAppConfig); err != nil {
		t.Fatalf("Execute(ApplyAppConfig) = %v, want nil", err)
	}
	if runner.args[len(runner.args)-1] != "apply-app-config" {
		t.Fatalf("runner args = %#v, want last arg %q", runner.args, "apply-app-config")
	}
}

func TestExecutorSupportsTheNewBridgeActions(t *testing.T) {
	runner := &recordingRunner{}
	executor := NewExecutor(runner, "/usr/local/sbin/energy-node-dashboard-system-action")
	for _, action := range []Action{ApplyBridgeConfig, RestartMosquitto} {
		if err := executor.Execute(context.Background(), action); err != nil {
			t.Fatalf("Execute(%s) = %v, want nil", action, err)
		}
		if runner.args[len(runner.args)-1] != string(action) {
			t.Fatalf("runner args = %#v, want last arg %q", runner.args, action)
		}
	}
}

// blockingRunner lets a test hold Execute open long enough to prove the
// existing busy lock (systemactions.go:57-68) also blocks a
// restart-mosquitto call that arrives while apply-bridge-config is still
// running, and vice versa - the two new actions share one Executor, so
// nothing extra was needed for this, but it is worth a regression test
// since a per-action lock would have silently allowed both to run at once.
type blockingRunner struct {
	release chan struct{}
	started chan struct{}
}

func (r *blockingRunner) Run(ctx context.Context, name string, args ...string) error {
	close(r.started)
	select {
	case <-r.release:
	case <-ctx.Done():
	}
	return nil
}

func TestExecutorSupportsTheTailscaleActions(t *testing.T) {
	runner := &recordingRunner{}
	executor := NewExecutor(runner, "/usr/local/sbin/energy-node-dashboard-system-action")
	for _, action := range []Action{TailscaleUp, TailscaleLogout, TailscaleRestart} {
		if err := executor.Execute(context.Background(), action); err != nil {
			t.Fatalf("Execute(%s) = %v, want nil", action, err)
		}
		if runner.args[len(runner.args)-1] != string(action) {
			t.Fatalf("runner args = %#v, want last arg %q", runner.args, action)
		}
	}
}

func TestBusyLockIsSharedBetweenApplyBridgeConfigAndRestartMosquitto(t *testing.T) {
	runner := &blockingRunner{release: make(chan struct{}), started: make(chan struct{})}
	executor := NewExecutor(runner, "/usr/local/sbin/energy-node-dashboard-system-action")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = executor.Execute(context.Background(), ApplyBridgeConfig)
	}()
	<-runner.started

	if err := executor.Execute(context.Background(), RestartMosquitto); !errors.Is(err, ErrBusy) {
		t.Fatalf("Execute(RestartMosquitto) while apply-bridge-config runs = %v, want ErrBusy", err)
	}

	close(runner.release)
	wg.Wait()
}

type fakeOutputRunner struct {
	out string
	err error
}

func (r fakeOutputRunner) Output(context.Context, string, ...string) (string, error) {
	return r.out, r.err
}

func TestServiceIsActiveReturnsTheStateWordEvenOnNonZeroExit(t *testing.T) {
	// systemctl is-active exits non-zero for every state except "active",
	// but still prints the state word - callers need "failed"/"inactive",
	// not just "there was an error".
	got := ServiceIsActive(context.Background(), fakeOutputRunner{out: "failed", err: errors.New("exit status 3")}, "mosquitto")
	if got != "failed" {
		t.Fatalf("ServiceIsActive = %q, want %q", got, "failed")
	}
	got = ServiceIsActive(context.Background(), fakeOutputRunner{out: "active", err: nil}, "mosquitto")
	if got != "active" {
		t.Fatalf("ServiceIsActive = %q, want %q", got, "active")
	}
	got = ServiceIsActive(context.Background(), fakeOutputRunner{out: "", err: errors.New("not found")}, "mosquitto")
	if got != "unknown" {
		t.Fatalf("ServiceIsActive = %q, want %q", got, "unknown")
	}
}

func TestServiceIsEnabledReturnsTheStateWordEvenOnNonZeroExit(t *testing.T) {
	got := ServiceIsEnabled(context.Background(), fakeOutputRunner{out: "disabled", err: errors.New("exit status 1")}, "tailscaled")
	if got != "disabled" {
		t.Fatalf("ServiceIsEnabled = %q, want %q", got, "disabled")
	}
	got = ServiceIsEnabled(context.Background(), fakeOutputRunner{out: "enabled", err: nil}, "tailscaled")
	if got != "enabled" {
		t.Fatalf("ServiceIsEnabled = %q, want %q", got, "enabled")
	}
	got = ServiceIsEnabled(context.Background(), fakeOutputRunner{out: "", err: errors.New("not found")}, "tailscaled")
	if got != "unknown" {
		t.Fatalf("ServiceIsEnabled = %q, want %q", got, "unknown")
	}
}

func TestStopStartServicesAreSupportedActions(t *testing.T) {
	for _, action := range []Action{StopServices, StartServices} {
		if !IsSupported(action) {
			t.Fatalf("IsSupported(%s) = false, want true", action)
		}
	}
	runner := &recordingRunner{}
	executor := NewExecutor(runner, "/usr/local/sbin/energy-node-dashboard-system-action")
	if err := executor.StopServices(); err != nil {
		t.Fatalf("StopServices() = %v, want nil", err)
	}
	if runner.args[len(runner.args)-1] != "stop-services" {
		t.Fatalf("runner args = %#v, want last arg %q", runner.args, "stop-services")
	}
	if err := executor.StartServices(); err != nil {
		t.Fatalf("StartServices() = %v, want nil", err)
	}
	if runner.args[len(runner.args)-1] != "start-services" {
		t.Fatalf("runner args = %#v, want last arg %q", runner.args, "start-services")
	}
}

const testStorageHelper = "/usr/local/sbin/energy-node-dashboard-system-action"

// recordingOutputRunner records the last call and returns canned stdout.
type recordingOutputRunner struct {
	recordingRunner
	out   string
	err   error
	calls int
}

func (runner *recordingOutputRunner) Output(_ context.Context, name string, args ...string) (string, error) {
	runner.calls++
	runner.name = name
	runner.args = append([]string(nil), args...)
	return runner.out, runner.err
}

func assertCommand(t *testing.T, runner *recordingOutputRunner, name string, args ...string) {
	t.Helper()
	if runner.name != name {
		t.Fatalf("command = %q, want %q", runner.name, name)
	}
	if len(runner.args) != len(args) {
		t.Fatalf("args = %#v, want %#v", runner.args, args)
	}
	for index := range args {
		if runner.args[index] != args[index] {
			t.Fatalf("args = %#v, want %#v", runner.args, args)
		}
	}
}

func TestStorageRunsTheStorageVerbThroughSudoAndReturnsStdout(t *testing.T) {
	runner := &recordingOutputRunner{out: "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d\n"}
	executor := NewExecutor(runner, testStorageHelper)
	out, err := executor.Storage(context.Background(), "format", "sda")
	if err != nil {
		t.Fatalf("Storage(format, sda) = %v, want nil", err)
	}
	if out != "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d" {
		t.Fatalf("Storage stdout = %q, want the trimmed UUID", out)
	}
	assertCommand(t, runner, "sudo", "-n", testStorageHelper, "storage", "format", "sda")

	uuid := "3f9c2e1a-8b7d-4c6e-9f10-2a3b4c5d6e7f"
	if _, err := executor.Storage(context.Background(), "mount", uuid); err != nil {
		t.Fatalf("Storage(mount) = %v, want nil", err)
	}
	assertCommand(t, runner, "sudo", "-n", testStorageHelper, "storage", "mount", uuid)

	if _, err := executor.Storage(context.Background(), "unmount", ""); err != nil {
		t.Fatalf("Storage(unmount) = %v, want nil", err)
	}
	assertCommand(t, runner, "sudo", "-n", testStorageHelper, "storage", "unmount")
}

func TestStorageRejectsInvalidRequestsBeforeAnyCommand(t *testing.T) {
	cases := []struct{ verb, arg string }{
		{"format", "../sda"},
		{"format", "sda;reboot"},
		{"format", ""},
		{"format", "SDA"},
		{"mount", "not-a-uuid"},
		{"mount", ""},
		{"unmount", "sda"},
		{"rm", "sda"},
		{"", ""},
	}
	for _, tc := range cases {
		runner := &recordingOutputRunner{}
		executor := NewExecutor(runner, testStorageHelper)
		if _, err := executor.Storage(context.Background(), tc.verb, tc.arg); err == nil {
			t.Fatalf("Storage(%q, %q) was accepted", tc.verb, tc.arg)
		}
		if runner.calls != 0 {
			t.Fatalf("Storage(%q, %q) ran a command", tc.verb, tc.arg)
		}
	}
}

func TestStorageReturnsStdoutAndErrorOfAFailedHelperRun(t *testing.T) {
	runner := &recordingOutputRunner{out: "DEVICE_MOUNTED\n", err: errors.New("exit status 65")}
	executor := NewExecutor(runner, testStorageHelper)
	out, err := executor.Storage(context.Background(), "format", "sda")
	if err == nil {
		t.Fatal("Storage with failed helper run = nil error, want error")
	}
	if out != "DEVICE_MOUNTED" {
		t.Fatalf("Storage stdout = %q, want the helper's error code", out)
	}
}

// blockingOutputRunner holds Output open until release is closed.
type blockingOutputRunner struct {
	release chan struct{}
	started chan struct{}
}

func (r *blockingOutputRunner) Run(context.Context, string, ...string) error { return nil }

func (r *blockingOutputRunner) Output(context.Context, string, ...string) (string, error) {
	close(r.started)
	<-r.release
	return "", nil
}

func TestStorageSharesTheBusyLockWithExecute(t *testing.T) {
	runner := &blockingOutputRunner{release: make(chan struct{}), started: make(chan struct{})}
	executor := NewExecutor(runner, testStorageHelper)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = executor.Storage(context.Background(), "format", "sda")
	}()
	<-runner.started

	if err := executor.Execute(context.Background(), Reboot); !errors.Is(err, ErrBusy) {
		t.Fatalf("Execute(Reboot) while storage runs = %v, want ErrBusy", err)
	}
	if _, err := executor.Storage(context.Background(), "unmount", ""); !errors.Is(err, ErrBusy) {
		t.Fatalf("Storage(unmount) while storage runs = %v, want ErrBusy", err)
	}

	close(runner.release)
	wg.Wait()
}
