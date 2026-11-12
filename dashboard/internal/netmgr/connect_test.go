package netmgr

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	hex64 := strings.Repeat("a", 64)
	tests := []struct {
		name    string
		req     ConnectRequest
		wantErr bool
	}{
		{"ok with password", ConnectRequest{SSID: "Haus", Password: "geheim123"}, false},
		{"ok open network", ConnectRequest{SSID: "Gast"}, false},
		{"ok hex psk", ConnectRequest{SSID: "Haus", Password: hex64}, false},
		{"ok 32 byte ssid", ConnectRequest{SSID: strings.Repeat("s", 32), Password: "geheim123"}, false},
		{"ok umlaut ssid within 32 bytes", ConnectRequest{SSID: "Küche"}, false},
		{"empty ssid", ConnectRequest{SSID: ""}, true},
		{"33 byte ssid", ConnectRequest{SSID: strings.Repeat("s", 33)}, true},
		{"umlaut ssid over 32 bytes", ConnectRequest{SSID: strings.Repeat("ü", 17)}, true},
		{"ssid with newline", ConnectRequest{SSID: "Haus\nNeu"}, true},
		{"ssid with DEL", ConnectRequest{SSID: "Haus\x7f"}, true},
		{"ssid invalid utf8", ConnectRequest{SSID: "Haus\xff"}, true},
		{"password 7 chars", ConnectRequest{SSID: "Haus", Password: "1234567"}, true},
		{"password 8 chars", ConnectRequest{SSID: "Haus", Password: "12345678"}, false},
		{"password 63 chars", ConnectRequest{SSID: "Haus", Password: strings.Repeat("p", 63)}, false},
		{"password 64 non hex", ConnectRequest{SSID: "Haus", Password: strings.Repeat("g", 64)}, true},
		{"password 65 chars", ConnectRequest{SSID: "Haus", Password: strings.Repeat("p", 65)}, true},
		{"password with umlaut", ConnectRequest{SSID: "Haus", Password: "Passwörter"}, true},
		{"empty uuid is a new profile", ConnectRequest{SSID: "Haus", Password: "geheim123"}, false},
		{"valid uuid", ConnectRequest{SSID: "Haus", ProfileUUID: "3f2b1c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"}, false},
		{"bad uuid", ConnectRequest{SSID: "Haus", ProfileUUID: "not-a-uuid"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr && !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("err = %v, want ErrInvalidRequest", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
}

// stagingRunner checks the staged file at the moment the helper runs,
// because the helper consumes it.
type stagingRunner struct {
	path    string
	mode    os.FileMode
	content []byte
	name    string
	args    []string
	err     error
	statErr error
	exists  bool
}

func (r *stagingRunner) Output(_ context.Context, name string, args ...string) (string, error) {
	r.name = name
	r.args = args
	info, err := os.Stat(r.path)
	r.statErr = err
	if err == nil {
		r.exists = true
		r.mode = info.Mode().Perm()
		r.content, _ = os.ReadFile(r.path)
	}
	return "", r.err
}

func TestConnectStagesFileWith0600(t *testing.T) {
	dir := t.TempDir()
	runner := &stagingRunner{path: filepath.Join(dir, "network-connect.staged.json")}
	c := NewClient(runner, "/helper", dir)
	req := ConnectRequest{SSID: "Küche", Password: "geheim123", Hidden: true, ProfileUUID: "3f2b1c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"}
	if err := c.Connect(context.Background(), req, "WPA3"); err != nil {
		t.Fatal(err)
	}
	if !runner.exists {
		t.Fatalf("staged file missing during helper call: %v", runner.statErr)
	}
	if runner.mode != 0o600 {
		t.Errorf("mode = %o, want 600", runner.mode)
	}
	var got map[string]any
	if err := json.Unmarshal(runner.content, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"ssid":         "Küche",
		"password":     "geheim123",
		"hidden":       true,
		"profile_uuid": "3f2b1c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d",
		"security":     "WPA3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("staged = %v, want %v", got, want)
	}
	if runner.name != "sudo" || !reflect.DeepEqual(runner.args, []string{"-n", "/helper", "wifi-connect"}) {
		t.Errorf("call = %s %v", runner.name, runner.args)
	}
	if _, err := os.Stat(runner.path); err != nil {
		t.Errorf("staged file should stay for the helper to consume: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestConnectInvalidRequestWritesNothing(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{}
	c := NewClient(runner, "/helper", dir)
	err := c.Connect(context.Background(), ConnectRequest{SSID: ""}, "")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
	if runner.calls != 0 {
		t.Errorf("helper called %d times, want 0", runner.calls)
	}
	if _, err := os.Stat(filepath.Join(dir, "network-connect.staged.json")); !os.IsNotExist(err) {
		t.Errorf("staged file exists after invalid request: %v", err)
	}
}

func TestConnectWhileSwitchRunning(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{err: exitWith(t, 75)}
	c := NewClient(runner, "/helper", dir)
	err := c.Connect(context.Background(), ConnectRequest{SSID: "Haus", Password: "geheim123"}, "WPA2")
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if _, err := os.Stat(filepath.Join(dir, stagedFileName)); !os.IsNotExist(err) {
		t.Errorf("staged file with the password is still on disk: %v", err)
	}
}

func TestConnectHelperRejectsRequest(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{err: exitWith(t, 65)}
	c := NewClient(runner, "/helper", dir)
	err := c.Connect(context.Background(), ConnectRequest{SSID: "Haus", Password: "geheim123"}, "WPA2")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
}

func TestConnectOtherFailureIsUnavailable(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{err: exitWith(t, 1)}
	c := NewClient(runner, "/helper", dir)
	err := c.Connect(context.Background(), ConnectRequest{SSID: "Haus"}, "")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestForgetRejectsNonUUID(t *testing.T) {
	runner := &fakeRunner{}
	c := NewClient(runner, "/helper", t.TempDir())
	err := c.Forget(context.Background(), "Haus; rm -rf /")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
	if runner.calls != 0 {
		t.Errorf("helper called %d times, want 0", runner.calls)
	}
}

func TestForgetRunsWifiForget(t *testing.T) {
	runner := &fakeRunner{}
	c := NewClient(runner, "/helper", t.TempDir())
	uuid := "3f2b1c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	if err := c.Forget(context.Background(), uuid); err != nil {
		t.Fatal(err)
	}
	want := []string{"-n", "/helper", "wifi-forget", uuid}
	if runner.name != "sudo" || !reflect.DeepEqual(runner.args, want) {
		t.Errorf("call = %s %v, want sudo %v", runner.name, runner.args, want)
	}
}

func TestForgetHelperRejectsProfile(t *testing.T) {
	runner := &fakeRunner{err: exitWith(t, 65)}
	c := NewClient(runner, "/helper", t.TempDir())
	err := c.Forget(context.Background(), "3f2b1c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
}
