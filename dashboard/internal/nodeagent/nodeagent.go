package nodeagent

import (
	"context"
	"sync"
	"time"
)

type Options struct {
	NodeID                   string
	NodeName                 string
	DiscoveryPrefix          string
	PollIntervalS            float64
	DiagnosticPollMultiplier float64
	TailscaleBin             string
}

type Agent struct {
	opts   Options
	reader *reader

	mu   sync.RWMutex
	fast FastState
	slow SlowDiagnostics
	seen bool

	live liveness
	now  func() time.Time
}

func New(opts Options) *Agent {
	if opts.NodeID == "" {
		opts.NodeID = "energy_node"
	}
	if opts.DiscoveryPrefix == "" {
		opts.DiscoveryPrefix = "homeassistant"
	}
	if opts.PollIntervalS <= 0 {
		opts.PollIntervalS = 60
	}
	if opts.DiagnosticPollMultiplier <= 0 {
		opts.DiagnosticPollMultiplier = 10
	}
	return &Agent{opts: opts, reader: newReader(), now: time.Now}
}

func (a *Agent) baseTopic() string  { return "outstation/" + a.opts.NodeID }
func (a *Agent) stateTopic() string { return a.baseTopic() + "/state" }
func (a *Agent) diagTopic() string  { return a.baseTopic() + "/diagnostics" }

// availabilityTopic ist die gemeinsame Node-Erreichbarkeit; alleiniger
// Publisher ist die LWT des Dashboard-MQTT-Clients (Entscheidung 2).
func (a *Agent) availabilityTopic() string { return a.baseTopic() + "/status/online" }

// PollInterval / DiagnosticInterval steuern die Ticker in cmd/dashboard.
func (a *Agent) PollInterval() time.Duration {
	return time.Duration(a.opts.PollIntervalS * float64(time.Second))
}
func (a *Agent) DiagnosticInterval() time.Duration {
	return time.Duration(a.opts.PollIntervalS * a.opts.DiagnosticPollMultiplier * float64(time.Second))
}

// Refresh liest die Fast-Metriken neu ein und merkt sie. full == true liest
// zusaetzlich die teuren Slow-Diagnostics.
func (a *Agent) Refresh(ctx context.Context, full bool) {
	fast := a.reader.FastState(ctx)
	a.mu.Lock()
	a.fast = fast
	a.seen = true
	if full {
		a.mu.Unlock()
		slow := a.reader.SlowDiagnostics(ctx, a.opts.TailscaleBin)
		a.mu.Lock()
		a.slow = slow
	}
	a.mu.Unlock()
}

// Telemetry ist der Ausschnitt fuer /api/v1/health und die Statusleiste.
type Telemetry struct {
	CPUTempC        *float64 `json:"cpu_temp_c"`
	RAMUsedPct      *float64 `json:"ram_used_pct"`
	UndervoltageNow bool     `json:"undervoltage_now"`
}

func (a *Agent) Telemetry() (Telemetry, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.seen {
		return Telemetry{}, false
	}
	return Telemetry{
		CPUTempC:        a.fast.CPUTempC,
		RAMUsedPct:      a.fast.RAMUsedPct,
		UndervoltageNow: a.fast.UndervoltageNow,
	}, true
}
