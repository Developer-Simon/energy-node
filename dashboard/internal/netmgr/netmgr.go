// Package netmgr reads the network state of the node (LAN, WLAN, hotspot,
// WLAN scan results) through the root-owned system action helper. The
// helper runs nmcli, ip, iw and reads sysfs; this package only parses the
// sections it prints.
package netmgr

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
)

// ErrUnavailable means the helper could not be run or failed unexpectedly.
var ErrUnavailable = errors.New("network helper unavailable")

// ErrUnmanaged means NetworkManager is not running, so a WLAN scan is impossible.
var ErrUnmanaged = errors.New("network not managed by NetworkManager")

// helperExitUnmanaged is the exit code of wifi-scan without a managed NetworkManager.
const helperExitUnmanaged = 69

type Link struct {
	ID            string   `json:"id"`     // "lan" | "wifi"
	Device        string   `json:"device"` // eth0, wlan0, "" ohne Gerät
	State         string   `json:"state"`  // up | down | off | error | hotspot | none | blocked
	Route         bool     `json:"route"`  // trägt die Default-Route mit kleinster Metrik
	Address       string   `json:"address,omitempty"`
	Gateway       string   `json:"gateway,omitempty"`
	DNS           []string `json:"dns,omitempty"`
	SSID          string   `json:"ssid,omitempty"`
	SignalDBm     *int     `json:"signal_dbm,omitempty"`
	Band          string   `json:"band,omitempty"` // "2.4" | "5" | "6"
	Channel       int      `json:"channel,omitempty"`
	SpeedMbps     int      `json:"speed_mbps,omitempty"`
	LastError     string   `json:"last_error,omitempty"` // Code aus der Switch-Datei: auth | not_found | no_address | no_gateway | failed
	LastErrorSSID string   `json:"last_error_ssid,omitempty"`
	LastErrorAt   string   `json:"last_error_at,omitempty"`
}

type Network struct {
	SSID        string `json:"ssid"`
	Band        string `json:"band"`
	Channel     int    `json:"channel"`
	Security    string `json:"security"`   // "" | "WPA2" | "WPA3" | "WPA2/WPA3"
	SignalDBm   int    `json:"signal_dbm"` // aus nmcli-Prozent: pct/2 - 100
	Level       int    `json:"level"`      // 1..4
	Saved       bool   `json:"saved"`
	ProfileUUID string `json:"profile_uuid,omitempty"`
	Current     bool   `json:"current"`
}

type Settings struct {
	Fallback string `json:"fallback"` // "parallel" | "standby"
	Hotspot  bool   `json:"hotspot"`
}

type HotspotState struct {
	Available bool `json:"available"` // WLAN-Gerät vorhanden und managed
	Active    bool `json:"active"`
	Clients   int  `json:"clients"`
}

type Status struct {
	Managed  bool            `json:"managed"`
	HasWifi  bool            `json:"has_wifi"`
	Country  string          `json:"country"`
	Links    []Link          `json:"links"` // immer [lan, wifi]
	Networks []Network       `json:"networks"`
	Settings Settings        `json:"settings"`
	Hotspot  HotspotState    `json:"hotspot"`
	Switch   *SwitchProgress `json:"switch,omitempty"` // aus ##switch, nil bei {}
}

type SwitchStep struct {
	Key    string `json:"key"`   // associate | address | gateway
	State  string `json:"state"` // wait | run | done | fail
	Detail string `json:"detail,omitempty"`
}

type SwitchProgress struct {
	SSID       string       `json:"ssid"`
	StartedAt  string       `json:"started_at"`
	Steps      []SwitchStep `json:"steps"`
	Internet   string       `json:"internet"`
	Result     string       `json:"result"`
	Error      string       `json:"error"`
	Address    string       `json:"address"`
	RolledBack bool         `json:"rolled_back"`
	FinishedAt string       `json:"finished_at"`
}

// Client runs the system action helper through sudo.
type Client struct {
	runner   systemactions.OutputRunner
	helper   string
	stageDir string
}

// NewClient returns a Client. A nil runner defaults to ExecOutputRunner.
// stageDir is the data folder of the dashboard, where the staging file of
// the apply step is written.
func NewClient(runner systemactions.OutputRunner, helper, stageDir string) *Client {
	if runner == nil {
		runner = systemactions.ExecOutputRunner{}
	}
	return &Client{runner: runner, helper: helper, stageDir: stageDir}
}

// Status runs network-status and parses its output.
func (c *Client) Status(ctx context.Context) (Status, error) {
	out, err := c.runner.Output(ctx, "sudo", "-n", c.helper, "network-status")
	if err != nil {
		return Status{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return ParseStatus(out)
}

// Scan asks NetworkManager for a fresh WLAN scan through wifi-scan.
func (c *Client) Scan(ctx context.Context) error {
	_, err := c.runner.Output(ctx, "sudo", "-n", c.helper, "wifi-scan")
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == helperExitUnmanaged {
		return ErrUnmanaged
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}
