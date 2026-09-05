// Package tailscale provides the dashboard's narrow, read-mostly boundary
// to the Tailscale CLI already installed on the target system (see
// knowhow/tailscale-setup.md for how it got there). Anything that changes
// tailnet membership (login, logout) runs through the root
// energy-node-dashboard-system-action helper, same as the Mosquitto
// bridge apply flow - this package only ever shells out to the unprivileged
// read commands (`tailscale status`, `tailscale version`) directly.
package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
)

const defaultTimeout = 10 * time.Second

// Client runs the unprivileged, read-only tailscale CLI commands. It
// reuses systemactions.OutputRunner rather than its own exec plumbing, the
// same interface the Mosquitto bridge status check already uses.
type Client struct {
	binary  string
	runner  systemactions.OutputRunner
	timeout time.Duration
}

func NewClient(binary string, runner systemactions.OutputRunner, timeout time.Duration) *Client {
	if binary == "" {
		binary = "/usr/sbin/tailscale"
	}
	if runner == nil {
		runner = systemactions.ExecOutputRunner{}
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{binary: binary, runner: runner, timeout: timeout}
}

// selfStatus is the subset of `tailscale status --json`'s "Self" object the
// wizard and status view need.
type selfStatus struct {
	TailscaleIPs []string  `json:"TailscaleIPs"`
	DNSName      string    `json:"DNSName"`
	Online       bool      `json:"Online"`
	KeyExpiry    time.Time `json:"KeyExpiry"`
}

type currentTailnet struct {
	Name string `json:"Name"`
}

// rawStatus mirrors the subset of `tailscale status --json`'s top-level
// shape this package relies on. Unknown fields are ignored by
// encoding/json, so this is intentionally not a full mirror of the CLI's
// output - see the package doc comment on internal/tailscale for the note
// that these field names should be spot-checked against the actually
// installed tailscale version during rollout.
type rawStatus struct {
	BackendState   string                     `json:"BackendState"`
	AuthURL        string                     `json:"AuthURL"`
	Self           selfStatus                 `json:"Self"`
	CurrentTailnet currentTailnet             `json:"CurrentTailnet"`
	Peer           map[string]json.RawMessage `json:"Peer"`
	Health         []string                   `json:"Health"`
}

// Status is the parsed, UI-facing shape of `tailscale status --json`.
type Status struct {
	Installed    bool      `json:"installed"`
	BackendState string    `json:"backend_state"`
	AuthURL      string    `json:"auth_url,omitempty"`
	TailscaleIPs []string  `json:"tailscale_ips"`
	DNSName      string    `json:"dns_name"`
	Online       bool      `json:"online"`
	KeyExpiry    time.Time `json:"key_expiry,omitempty"`
	Tailnet      string    `json:"tailnet"`
	PeerCount    int       `json:"peer_count"`
	Health       []string  `json:"health,omitempty"`
}

// Status runs `tailscale status --json`. A failure to execute the binary
// at all (not installed, tailscaled not reachable) is reported as
// Status{Installed: false}, not an error - the prerequisite/status view
// needs to render that state, not fail outright.
func (c *Client) Status(ctx context.Context) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	out, err := c.runner.Output(ctx, c.binary, "status", "--json")
	if err != nil {
		return Status{Installed: false}, nil
	}
	var raw rawStatus
	if jsonErr := json.Unmarshal([]byte(out), &raw); jsonErr != nil {
		return Status{}, errors.New("tailscale status lieferte ungültiges JSON")
	}
	status := Status{
		Installed:    true,
		BackendState: raw.BackendState,
		AuthURL:      raw.AuthURL,
		TailscaleIPs: raw.Self.TailscaleIPs,
		DNSName:      strings.TrimSuffix(raw.Self.DNSName, "."),
		Online:       raw.Self.Online,
		Tailnet:      raw.CurrentTailnet.Name,
		PeerCount:    len(raw.Peer),
		Health:       raw.Health,
	}
	if !raw.Self.KeyExpiry.IsZero() {
		status.KeyExpiry = raw.Self.KeyExpiry
	}
	return status, nil
}

// Version runs `tailscale version` and returns its first line. An error
// means the binary could not be executed at all (most likely: not
// installed).
func (c *Client) Version(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	out, err := c.runner.Output(ctx, c.binary, "version")
	if err != nil {
		return "", err
	}
	line := strings.SplitN(out, "\n", 2)[0]
	return strings.TrimSpace(line), nil
}

// Prereqs is the "ist Tailscale installiert, läuft tailscaled, ist der
// Dienst aktiviert" check the wizard's first step runs.
type Prereqs struct {
	Installed      bool   `json:"installed"`
	Version        string `json:"version,omitempty"`
	ServiceActive  string `json:"service_active"`
	ServiceEnabled string `json:"service_enabled"`
}

func (c *Client) Prereqs(ctx context.Context) Prereqs {
	version, err := c.Version(ctx)
	return Prereqs{
		Installed:      err == nil,
		Version:        version,
		ServiceActive:  systemactions.ServiceIsActive(ctx, c.runner, "tailscaled"),
		ServiceEnabled: systemactions.ServiceIsEnabled(ctx, c.runner, "tailscaled"),
	}
}
