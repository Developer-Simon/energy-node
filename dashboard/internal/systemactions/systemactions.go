package systemactions

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

type Action string

const (
	RestartDashboard  Action = "restart-dashboard"
	Reboot            Action = "reboot"
	Poweroff          Action = "poweroff"
	ApplyBridgeConfig Action = "apply-bridge-config"
	ApplyAppConfig    Action = "apply-app-config"
	RestartMosquitto  Action = "restart-mosquitto"
	TailscaleUp       Action = "tailscale-up"
	TailscaleLogout   Action = "tailscale-logout"
	TailscaleRestart  Action = "tailscale-restart"
	StopServices      Action = "stop-services"
	StartServices     Action = "start-services"
)

var ErrBusy = errors.New("a system action is already running")

var actions = map[Action]struct{}{
	RestartDashboard:  {},
	Reboot:            {},
	Poweroff:          {},
	ApplyBridgeConfig: {},
	ApplyAppConfig:    {},
	RestartMosquitto:  {},
	TailscaleUp:       {},
	TailscaleLogout:   {},
	TailscaleRestart:  {},
	StopServices:      {},
	StartServices:     {},
}

type Runner interface {
	Run(context.Context, string, ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

// Output runs the command and returns its stdout, which Storage needs for the
// UUID that format prints and for the error codes the helper prints.
func (ExecRunner) Output(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

type Executor struct {
	runner Runner
	helper string
	mu     sync.Mutex
	busy   bool
}

func NewExecutor(runner Runner, helper string) *Executor {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Executor{runner: runner, helper: helper}
}

func (e *Executor) Execute(ctx context.Context, action Action) error {
	if _, ok := actions[action]; !ok {
		return errors.New("unsupported system action")
	}
	if e.helper == "" {
		return errors.New("system action helper is not configured")
	}
	if err := e.acquire(); err != nil {
		return err
	}
	defer e.release()
	return e.runner.Run(ctx, "sudo", "-n", e.helper, string(action))
}

// acquire takes the busy lock shared by every helper call of this Executor.
func (e *Executor) acquire() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.busy {
		return ErrBusy
	}
	e.busy = true
	return nil
}

func (e *Executor) release() {
	e.mu.Lock()
	e.busy = false
	e.mu.Unlock()
}

// ErrInvalidStorageRequest is returned for a verb or argument the storage
// verb does not accept. The helper validates the same inputs again.
var ErrInvalidStorageRequest = errors.New("invalid storage request")

var (
	storageDiskPattern = regexp.MustCompile(`^[a-z0-9]+$`)
	storageUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// Storage runs the storage verb of the system action helper, which runs the
// drive helper, with verb format, mount or unmount. Unmount takes no argument,
// format takes a disk name, mount a filesystem UUID. Inputs are checked here
// before anything runs. It returns the helper's stdout, trimmed: the UUID for
// format, the error code for a refusal. It shares the busy lock with Execute.
func (e *Executor) Storage(ctx context.Context, verb, arg string) (string, error) {
	switch verb {
	case "format":
		if !storageDiskPattern.MatchString(arg) {
			return "", ErrInvalidStorageRequest
		}
	case "mount":
		if !storageUUIDPattern.MatchString(arg) {
			return "", ErrInvalidStorageRequest
		}
	case "unmount":
		if arg != "" {
			return "", ErrInvalidStorageRequest
		}
	default:
		return "", ErrInvalidStorageRequest
	}
	if e.helper == "" {
		return "", errors.New("system action helper is not configured")
	}
	runner, ok := e.runner.(OutputRunner)
	if !ok {
		return "", errors.New("system action runner cannot return output")
	}
	if err := e.acquire(); err != nil {
		return "", err
	}
	defer e.release()

	args := []string{"-n", e.helper, "storage", verb}
	if arg != "" {
		args = append(args, arg)
	}
	out, err := runner.Output(ctx, "sudo", args...)
	out = strings.TrimSpace(out)
	if err != nil {
		return out, fmt.Errorf("storage %s: %w", verb, err)
	}
	return out, nil
}

// StopServices stops the enabled services that the backup restore touches,
// through the helper's stop-services verb.
func (e *Executor) StopServices() error {
	return e.Execute(context.Background(), StopServices)
}

// StartServices starts the enabled services again after a restore, through
// the helper's start-services verb.
func (e *Executor) StartServices() error {
	return e.Execute(context.Background(), StartServices)
}

func IsSupported(action Action) bool {
	_, ok := actions[action]
	return ok
}
