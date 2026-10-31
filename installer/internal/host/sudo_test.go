package host

import (
	"errors"
	"os/exec"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func TestSudoPasswordForUsesTheSSHPasswordUnlessKeyLogin(t *testing.T) {
	password := hostapi.ConnectRequest{Kind: hostapi.AuthPassword, SudoPassword: "ignored"}
	if got := sudoPasswordFor(password, "ssh-pw"); got != "ssh-pw" {
		t.Errorf("password login: got %q, want the ssh password", got)
	}
	key := hostapi.ConnectRequest{Kind: hostapi.AuthKey, SudoPassword: "sudo-pw"}
	if got := sudoPasswordFor(key, "passphrase"); got != "sudo-pw" {
		t.Errorf("key login: got %q, want the sudo field, never the key passphrase", got)
	}
}

func TestSudoSetupErrorMapsAWrongPassword(t *testing.T) {
	err := sudoSetupError(&ssh.ExitError{Waitmsg: ssh.Waitmsg{}}, "")
	var apiErr *hostapi.Error
	// A zero Waitmsg exits with status 0, which is not a rejected password.
	if !errors.As(err, &apiErr) || apiErr.Code != "SUDO_SETUP_FAILED" {
		t.Fatalf("exit 0 error = %v, want SUDO_SETUP_FAILED", err)
	}
	if got := sudoSetupError(exitStatus(1), ""); !errors.As(got, &apiErr) || apiErr.Code != "SUDO_PASSWORD_WRONG" {
		t.Fatalf("exit 1 error = %v, want SUDO_PASSWORD_WRONG", got)
	}
	if got := sudoSetupError(exitStatus(21), "visudo: bad"); !errors.As(got, &apiErr) || apiErr.Code != "SUDO_SETUP_FAILED" || apiErr.Detail == "" {
		t.Fatalf("exit 21 error = %v, want SUDO_SETUP_FAILED with detail", got)
	}
}

type exitStatus int

func (e exitStatus) Error() string   { return "exit" }
func (e exitStatus) ExitStatus() int { return int(e) }

func TestEnableSudoScriptIsValidShell(t *testing.T) {
	if out, err := exec.Command("sh", "-n", "-c", enableSudoScript).CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v\n%s", err, out)
	}
}
