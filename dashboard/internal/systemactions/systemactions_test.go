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
