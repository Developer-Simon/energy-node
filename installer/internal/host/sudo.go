package host

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// sudoersDropIn is the file that grants the connecting user passwordless
// sudo. It stays after the installer is closed: later runs, updates and the
// developer CLI rely on `sudo -n`.
const sudoersDropIn = "/etc/sudoers.d/90-energy-node-installer"

// Exit codes of the script below. sudo itself exits 1 when the password is
// rejected, so the script's own failures use different numbers.
const (
	sudoExitBadPassword = 1
)

// enableSudoScript runs as root. $SUDO_USER is the account that called sudo,
// so the grant goes to the connecting user and to nobody else. visudo -c
// checks the line before it can lock the account out of sudo.
const enableSudoScript = `set -e
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
printf '%s ALL=(ALL) NOPASSWD:ALL\n' "$SUDO_USER" > "$tmp"
visudo -cf "$tmp" >/dev/null || exit 21
install -m 0440 -o root -g root "$tmp" ` + sudoersDropIn + ` || exit 22`

// ensureSudoNopasswd is a seam for tests; production always runs
// defaultEnsureSudoNopasswd.
var ensureSudoNopasswd = defaultEnsureSudoNopasswd

func (h *Host) currentSudoPassword() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sudoPassword
}

// defaultEnsureSudoNopasswd makes sure `sudo -n` works for the connecting
// user. When it does not, the password turns that on once by writing a
// sudoers drop-in. Without a password the operator gets SUDO_REQUIRED and the
// way out (enter the sudo password), instead of a failure several steps later.
func defaultEnsureSudoNopasswd(ctx context.Context, client *transport.Client, password string) error {
	if err := client.Run(ctx, "sudo -n true", io.Discard, io.Discard); err == nil {
		return nil
	}
	if password == "" {
		return &hostapi.Error{Code: "SUDO_REQUIRED", Status: http.StatusConflict}
	}
	command := "sudo -S -p '' sh -c " + transport.ShellQuote(enableSudoScript)
	var stderr strings.Builder
	err := client.RunWithStdin(ctx, command, strings.NewReader(password+"\n"), io.Discard, &stderr)
	if err != nil {
		return sudoSetupError(err, stderr.String())
	}
	if err := client.Run(ctx, "sudo -n true", io.Discard, io.Discard); err != nil {
		return &hostapi.Error{Code: "SUDO_SETUP_FAILED", Detail: "sudo still asks for a password after the setup", Status: http.StatusConflict}
	}
	return nil
}

func sudoSetupError(err error, stderr string) error {
	if exit, ok := err.(interface{ ExitStatus() int }); ok && exit.ExitStatus() == sudoExitBadPassword {
		return &hostapi.Error{Code: "SUDO_PASSWORD_WRONG", Status: http.StatusUnauthorized}
	}
	return &hostapi.Error{Code: "SUDO_SETUP_FAILED", Detail: fmt.Sprintf("%v (stderr: %s)", err, strings.TrimSpace(stderr)), Status: http.StatusConflict}
}

// EnableSudo richtet passwortloses sudo mit dem Passwort ein, das der Betreiber
// im Dialog eingegeben hat. Nur ein gelungener Aufruf merkt es sich.
func (h *Host) EnableSudo(ctx context.Context, password string) error {
	client, err := h.connected()
	if err != nil {
		return err
	}
	if password == "" {
		return &hostapi.Error{Code: "SUDO_REQUIRED", Status: http.StatusConflict}
	}
	if err := ensureSudoNopasswd(ctx, client, password); err != nil {
		return err
	}
	h.mu.Lock()
	h.sudoPassword = password
	h.mu.Unlock()
	return nil
}
