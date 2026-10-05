package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// SystemUpdates mirrors scripts/bootstrap/lib/apt_pending.py: the packages
// apt-get upgrade would update now. CheckedAt is the package lists' age, ""
// when unknown. The helper reports null when apt-get is missing or fails,
// which callers keep as nil.
type SystemUpdates struct {
	Count     int             `json:"count"`
	CheckedAt string          `json:"checked_at"`
	Packages  []SystemPackage `json:"packages"`
}

// SystemPackage is one pending update. From is "" for a package apt would
// install new.
type SystemPackage struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

// RefreshFailedError: apt-get update failed on the node (offline, broken
// sources). Detail is apt's stderr.
type RefreshFailedError struct {
	Detail string
}

func (e *RefreshFailedError) Error() string { return "apt-get update failed: " + e.Detail }

// refreshFailedExit is apt_pending.py's exit code for a failed --refresh.
const refreshFailedExit = 2

// QuerySystemUpdates runs the bundle's apt_pending.py on the node. Without
// refresh it only simulates on the existing package lists (no root, no
// network); with refresh it runs apt-get update through sudo first.
func QuerySystemUpdates(ctx context.Context, client *transport.Client, remoteBundleDir string, refresh bool) (*SystemUpdates, error) {
	command := "python3 " + transport.ShellQuote(path.Join(remoteBundleDir, "bootstrap", "lib", "apt_pending.py"))
	if refresh {
		command += " --refresh"
	}
	var stdout, stderr bytes.Buffer
	if err := client.Run(ctx, command, &stdout, &stderr); err != nil {
		var exit *ssh.ExitError
		if refresh && errors.As(err, &exit) && exit.ExitStatus() == refreshFailedExit {
			return nil, &RefreshFailedError{Detail: strings.TrimSpace(stderr.String())}
		}
		return nil, fmt.Errorf("running apt_pending.py: %w (stderr: %s)", err, stderr.String())
	}
	var updates *SystemUpdates
	if err := json.Unmarshal(stdout.Bytes(), &updates); err != nil {
		return nil, fmt.Errorf("parsing apt_pending.py output: %w (stdout: %q)", err, stdout.String())
	}
	return updates, nil
}
