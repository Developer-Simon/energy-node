package systemactions

import (
	"context"
	"os/exec"
	"strings"
)

// OutputRunner is the read-only counterpart to Runner: it is used for
// unprivileged status queries (systemctl is-active) that need the command's
// stdout, not just success/failure, and therefore never go through the
// sudo/helper/allowlist path Executor.Execute uses.
type OutputRunner interface {
	Output(ctx context.Context, name string, args ...string) (string, error)
}

type ExecOutputRunner struct{}

func (ExecOutputRunner) Output(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return strings.TrimSpace(string(out)), err
}

// ServiceIsActive runs `systemctl is-active <service>` and returns the
// state word systemd prints (active, inactive, failed, activating, ...)
// even when systemctl's exit code is non-zero, which it is for every state
// except "active". A nil runner defaults to ExecOutputRunner{}.
func ServiceIsActive(ctx context.Context, runner OutputRunner, service string) string {
	if runner == nil {
		runner = ExecOutputRunner{}
	}
	out, err := runner.Output(ctx, "systemctl", "is-active", service)
	if out != "" {
		return out
	}
	if err != nil {
		return "unknown"
	}
	return "unknown"
}

// ServiceIsEnabled runs `systemctl is-enabled <service>` and returns the
// word systemd prints (enabled, disabled, static, masked, ...), same
// non-zero-exit-code handling as ServiceIsActive. A nil runner defaults to
// ExecOutputRunner{}.
func ServiceIsEnabled(ctx context.Context, runner OutputRunner, service string) string {
	if runner == nil {
		runner = ExecOutputRunner{}
	}
	out, err := runner.Output(ctx, "systemctl", "is-enabled", service)
	if out != "" {
		return out
	}
	if err != nil {
		return "unknown"
	}
	return "unknown"
}
