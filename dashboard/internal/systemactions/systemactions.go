package systemactions

import (
	"context"
	"errors"
	"os/exec"
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
	e.mu.Lock()
	if e.busy {
		e.mu.Unlock()
		return ErrBusy
	}
	e.busy = true
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.busy = false
		e.mu.Unlock()
	}()
	return e.runner.Run(ctx, "sudo", "-n", e.helper, string(action))
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
