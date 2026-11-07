package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	responses map[string]fakeResponse
}

type fakeResponse struct {
	out string
	err error
}

func (r fakeRunner) Output(_ context.Context, name string, args ...string) (string, error) {
	key := name
	for _, arg := range args {
		key += " " + arg
	}
	if resp, ok := r.responses[key]; ok {
		return resp.out, resp.err
	}
	return "", errors.New("unexpected command: " + key)
}

const statusJSON = `{
	"BackendState": "NeedsLogin",
	"AuthURL": "https://login.tailscale.com/a/xxxxxxxxx",
	"Self": {
		"TailscaleIPs": ["100.64.0.1"],
		"DNSName": "pi.tailnet-name.ts.net.",
		"Online": true,
		"KeyExpiry": "2026-12-01T00:00:00Z"
	},
	"CurrentTailnet": {"Name": "example.ts.net"},
	"Peer": {"nodekey:abc": {}, "nodekey:def": {}},
	"Health": ["some warning"]
}`

func TestStatusParsesTheFieldsTheUINeeds(t *testing.T) {
	client := NewClient("/usr/sbin/tailscale", fakeRunner{responses: map[string]fakeResponse{
		"/usr/sbin/tailscale status --json": {out: statusJSON},
	}}, time.Second)

	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Installed {
		t.Fatal("Installed = false, want true")
	}
	if status.BackendState != "NeedsLogin" {
		t.Fatalf("BackendState = %q, want NeedsLogin", status.BackendState)
	}
	if status.AuthURL != "https://login.tailscale.com/a/xxxxxxxxx" {
		t.Fatalf("AuthURL = %q, want a login URL", status.AuthURL)
	}
	if len(status.TailscaleIPs) != 1 || status.TailscaleIPs[0] != "100.64.0.1" {
		t.Fatalf("TailscaleIPs = %#v, want [100.64.0.1]", status.TailscaleIPs)
	}
	if status.DNSName != "pi.tailnet-name.ts.net" {
		t.Fatalf("DNSName = %q, want trailing dot trimmed", status.DNSName)
	}
	if status.Tailnet != "example.ts.net" {
		t.Fatalf("Tailnet = %q, want example.ts.net", status.Tailnet)
	}
	if status.PeerCount != 2 {
		t.Fatalf("PeerCount = %d, want 2", status.PeerCount)
	}
	if len(status.Health) != 1 || status.Health[0] != "some warning" {
		t.Fatalf("Health = %#v, want one warning", status.Health)
	}
	if status.KeyExpiry == nil || status.KeyExpiry.IsZero() {
		t.Fatal("KeyExpiry is zero, want the parsed timestamp")
	}
}

func TestStatusReportsNotInstalledWithoutAnError(t *testing.T) {
	client := NewClient("/usr/sbin/tailscale", fakeRunner{responses: map[string]fakeResponse{
		"/usr/sbin/tailscale status --json": {err: errors.New("exec: \"tailscale\": executable file not found")},
	}}, time.Second)

	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v, want nil (not-installed is a value, not an error)", err)
	}
	if status.Installed {
		t.Fatal("Installed = true, want false")
	}
}

func TestVersionReturnsFirstLine(t *testing.T) {
	client := NewClient("/usr/sbin/tailscale", fakeRunner{responses: map[string]fakeResponse{
		"/usr/sbin/tailscale version": {out: "1.62.0\n  tailscale commit: abcdef"},
	}}, time.Second)

	version, err := client.Version(context.Background())
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if version != "1.62.0" {
		t.Fatalf("Version() = %q, want 1.62.0", version)
	}
}

func TestPrereqsBundlesVersionAndServiceState(t *testing.T) {
	client := NewClient("/usr/sbin/tailscale", fakeRunner{responses: map[string]fakeResponse{
		"/usr/sbin/tailscale version":     {out: "1.62.0"},
		"systemctl is-active tailscaled":  {out: "active"},
		"systemctl is-enabled tailscaled": {out: "enabled"},
	}}, time.Second)

	prereqs := client.Prereqs(context.Background())
	if !prereqs.Installed || prereqs.Version != "1.62.0" {
		t.Fatalf("Prereqs = %#v, want Installed=true Version=1.62.0", prereqs)
	}
	if prereqs.ServiceActive != "active" || prereqs.ServiceEnabled != "enabled" {
		t.Fatalf("Prereqs = %#v, want ServiceActive=active ServiceEnabled=enabled", prereqs)
	}
}

func TestPrereqsReportsNotInstalled(t *testing.T) {
	client := NewClient("/usr/sbin/tailscale", fakeRunner{responses: map[string]fakeResponse{
		"systemctl is-active tailscaled":  {out: "unknown", err: errors.New("not found")},
		"systemctl is-enabled tailscaled": {out: "unknown", err: errors.New("not found")},
	}}, time.Second)

	prereqs := client.Prereqs(context.Background())
	if prereqs.Installed {
		t.Fatal("Installed = true, want false")
	}
}

func TestResolveBinaryKeepsAnExecutableConfiguredPath(t *testing.T) {
	exec := func(p string) bool { return p == "/opt/tailscale" || p == "/usr/bin/tailscale" }
	if got := resolveBinary("/opt/tailscale", exec); got != "/opt/tailscale" {
		t.Fatalf("resolveBinary = %q, want the configured path", got)
	}
}

func TestResolveBinaryFindsTheDebianPackagePath(t *testing.T) {
	// The Debian package installs the CLI to /usr/bin, the tarball install
	// of older nodes to /usr/sbin, which is still the configured default.
	exec := func(p string) bool { return p == "/usr/bin/tailscale" }
	if got := resolveBinary("/usr/sbin/tailscale", exec); got != "/usr/bin/tailscale" {
		t.Fatalf("resolveBinary = %q, want /usr/bin/tailscale", got)
	}
	if got := resolveBinary("", exec); got != "/usr/bin/tailscale" {
		t.Fatalf("resolveBinary(\"\") = %q, want /usr/bin/tailscale", got)
	}
}

func TestResolveBinaryKeepsTheConfiguredPathWhenNothingIsInstalled(t *testing.T) {
	exec := func(string) bool { return false }
	if got := resolveBinary("/usr/sbin/tailscale", exec); got != "/usr/sbin/tailscale" {
		t.Fatalf("resolveBinary = %q, want the configured path", got)
	}
	if got := resolveBinary("", exec); got != "/usr/sbin/tailscale" {
		t.Fatalf("resolveBinary(\"\") = %q, want /usr/sbin/tailscale", got)
	}
}

func TestStatusOmitsKeyExpiryWhenLoggedOut(t *testing.T) {
	client := NewClient("/usr/sbin/tailscale", fakeRunner{responses: map[string]fakeResponse{
		"/usr/sbin/tailscale status --json": {out: `{"BackendState":"NeedsLogin","Self":{"KeyExpiry":"0001-01-01T00:00:00Z"}}`},
	}}, time.Second)
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "key_expiry") {
		t.Fatalf("JSON = %s, want no key_expiry for a logged out node", raw)
	}
}
