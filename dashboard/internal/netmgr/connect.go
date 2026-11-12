package netmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"unicode/utf8"
)

// ErrInvalidRequest means the helper or Validate rejected a WLAN request.
var ErrInvalidRequest = errors.New("invalid network request")

// ErrBusy means a WLAN switch is already running, so a new one cannot start.
var ErrBusy = errors.New("a wifi switch is already running")

// Exit codes of wifi-connect and wifi-forget in the system action helper.
const (
	helperExitBusy    = 75
	helperExitInvalid = 65
)

// stagedFileName is the request file the dashboard writes for wifi-connect.
// The helper reads it, checks it and deletes it on success.
const stagedFileName = "network-connect.staged.json"

var (
	passwordRE = regexp.MustCompile(`^(?:[\x20-\x7e]{8,63}|[0-9a-fA-F]{64})$`)
	uuidRE     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// ConnectRequest is one WLAN connect request as the user enters it.
type ConnectRequest struct {
	SSID        string `json:"ssid"`
	Password    string `json:"password"`
	Hidden      bool   `json:"hidden"`
	ProfileUUID string `json:"profile_uuid"`
}

// Validate applies the same rules as the wifi-connect check of the system
// action helper. An empty password means an open network and an empty
// profile UUID means a new profile.
func (r ConnectRequest) Validate() error {
	if n := len(r.SSID); n < 1 || n > 32 {
		return fmt.Errorf("%w: ssid must have 1 to 32 bytes", ErrInvalidRequest)
	}
	if !utf8.ValidString(r.SSID) {
		return fmt.Errorf("%w: ssid is not valid UTF-8", ErrInvalidRequest)
	}
	for _, ch := range r.SSID {
		if ch < 0x20 || ch == 0x7f {
			return fmt.Errorf("%w: ssid contains a control character", ErrInvalidRequest)
		}
	}
	if r.Password != "" && !passwordRE.MatchString(r.Password) {
		return fmt.Errorf("%w: password must be 8 to 63 printable ASCII characters or 64 hex digits", ErrInvalidRequest)
	}
	if r.ProfileUUID != "" && !uuidRE.MatchString(r.ProfileUUID) {
		return fmt.Errorf("%w: profile_uuid is not a UUID", ErrInvalidRequest)
	}
	return nil
}

// Connect validates the request, stages it as 0600 JSON in the stage folder
// and starts wifi-connect. The helper consumes the staged file. On any error
// the staged file is removed, so the password does not stay on disk.
func (c *Client) Connect(ctx context.Context, req ConnectRequest, security string) error {
	if err := req.Validate(); err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		SSID        string `json:"ssid"`
		Password    string `json:"password"`
		Hidden      bool   `json:"hidden"`
		ProfileUUID string `json:"profile_uuid"`
		Security    string `json:"security"`
	}{req.SSID, req.Password, req.Hidden, req.ProfileUUID, security})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if err := c.stage(body); err != nil {
		return fmt.Errorf("%w: stage wifi request: %v", ErrUnavailable, err)
	}
	_, err = c.runner.Output(ctx, "sudo", "-n", c.helper, "wifi-connect")
	if err != nil {
		_ = os.Remove(c.stagedPath())
	}
	return mapHelperExit(err)
}

// Forget deletes a saved WLAN profile through wifi-forget.
func (c *Client) Forget(ctx context.Context, uuid string) error {
	if !uuidRE.MatchString(uuid) {
		return fmt.Errorf("%w: profile_uuid is not a UUID", ErrInvalidRequest)
	}
	_, err := c.runner.Output(ctx, "sudo", "-n", c.helper, "wifi-forget", uuid)
	return mapHelperExit(err)
}

func (c *Client) stagedPath() string {
	return filepath.Join(c.stageDir, stagedFileName)
}

// stage writes body to a temp file with mode 0600 and renames it into place.
func (c *Client) stage(body []byte) error {
	tmp, err := os.CreateTemp(c.stageDir, stagedFileName+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fail(err)
	}
	if _, err := tmp.Write(body); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, c.stagedPath()); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// mapHelperExit turns the helper exit codes 75 and 65 into ErrBusy and
// ErrInvalidRequest. Other failures are ErrUnavailable.
func mapHelperExit(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case helperExitBusy:
			return ErrBusy
		case helperExitInvalid:
			return fmt.Errorf("%w: helper rejected the request", ErrInvalidRequest)
		}
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}
