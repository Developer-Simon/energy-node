package netmgr

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strconv"
	"testing"
)

// fakeRunner records the call and answers with a fixed output or error.
type fakeRunner struct {
	out   string
	err   error
	name  string
	args  []string
	calls int
}

func (r *fakeRunner) Output(_ context.Context, name string, args ...string) (string, error) {
	r.calls++
	r.name = name
	r.args = args
	return r.out, r.err
}

func exitWith(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit "+strconv.Itoa(code)).Run()
	if err == nil {
		t.Fatal("expected exit error")
	}
	return err
}

func TestClientStatusRunsHelper(t *testing.T) {
	runner := &fakeRunner{out: readFixture(t, "both")}
	c := NewClient(runner, "/usr/local/bin/energy-node-dashboard-system-action", "/data")
	st, err := c.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runner.name != "sudo" {
		t.Errorf("name = %q, want sudo", runner.name)
	}
	want := []string{"-n", "/usr/local/bin/energy-node-dashboard-system-action", "network-status"}
	if !reflect.DeepEqual(runner.args, want) {
		t.Errorf("args = %v, want %v", runner.args, want)
	}
	if !st.Managed || len(st.Links) != 2 {
		t.Errorf("Status = %+v", st)
	}
}

func TestClientStatusHelperFailureIsUnavailable(t *testing.T) {
	runner := &fakeRunner{err: errors.New("exit status 1")}
	c := NewClient(runner, "/helper", "/data")
	_, err := c.Status(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestClientScanRunsWifiScan(t *testing.T) {
	runner := &fakeRunner{}
	c := NewClient(runner, "/helper", "/data")
	if err := c.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"-n", "/helper", "wifi-scan"}
	if runner.name != "sudo" || !reflect.DeepEqual(runner.args, want) {
		t.Errorf("call = %s %v, want sudo %v", runner.name, runner.args, want)
	}
}

func TestClientScanExit69IsUnmanaged(t *testing.T) {
	runner := &fakeRunner{err: exitWith(t, 69)}
	c := NewClient(runner, "/helper", "/data")
	if err := c.Scan(context.Background()); !errors.Is(err, ErrUnmanaged) {
		t.Fatalf("err = %v, want ErrUnmanaged", err)
	}
}

func TestClientScanOtherFailureIsUnavailable(t *testing.T) {
	runner := &fakeRunner{err: exitWith(t, 1)}
	c := NewClient(runner, "/helper", "/data")
	if err := c.Scan(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestSaveSettingsRunsNetworkSettings(t *testing.T) {
	runner := &fakeRunner{}
	c := NewClient(runner, "/helper", "/data")
	if err := c.SaveSettings(context.Background(), Settings{Fallback: "standby", Hotspot: true}); err != nil {
		t.Fatal(err)
	}
	want := []string{"-n", "/helper", "network-settings", "standby", "on"}
	if runner.name != "sudo" || !reflect.DeepEqual(runner.args, want) {
		t.Errorf("call = %s %v, want sudo %v", runner.name, runner.args, want)
	}
}

func TestSaveSettingsRejectsUnknownMode(t *testing.T) {
	runner := &fakeRunner{}
	c := NewClient(runner, "/helper", "/data")
	err := c.SaveSettings(context.Background(), Settings{Fallback: "fast", Hotspot: false})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
	if runner.calls != 0 {
		t.Errorf("helper ran for an unknown mode: %d calls", runner.calls)
	}
}

func TestSaveSettingsExit64IsInvalid(t *testing.T) {
	runner := &fakeRunner{err: exitWith(t, 64)}
	c := NewClient(runner, "/helper", "/data")
	err := c.SaveSettings(context.Background(), Settings{Fallback: "parallel"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
}

func TestSaveSettingsOtherFailureIsUnavailable(t *testing.T) {
	runner := &fakeRunner{err: exitWith(t, 1)}
	c := NewClient(runner, "/helper", "/data")
	err := c.SaveSettings(context.Background(), Settings{Fallback: "parallel"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestHotspotSecretReadsSSIDAndPassword(t *testing.T) {
	runner := &fakeRunner{out: "energy-node-hotspot\ns3cret-pass\n"}
	c := NewClient(runner, "/helper", "/data")
	ssid, password, err := c.HotspotSecret(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-n", "/helper", "hotspot-secret"}
	if runner.name != "sudo" || !reflect.DeepEqual(runner.args, want) {
		t.Errorf("call = %s %v, want sudo %v", runner.name, runner.args, want)
	}
	if ssid != "energy-node-hotspot" || password != "s3cret-pass" {
		t.Errorf("secret = %q, %q", ssid, password)
	}
}

func TestHotspotSecretBrokenOutputIsUnavailable(t *testing.T) {
	for _, out := range []string{"", "only-the-ssid\n", "ssid\n\n", "ssid\npass\nextra\n"} {
		runner := &fakeRunner{out: out}
		c := NewClient(runner, "/helper", "/data")
		_, _, err := c.HotspotSecret(context.Background())
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("output %q: err = %v, want ErrUnavailable", out, err)
		}
	}
}

func TestHotspotSecretMissingFileIsUnavailable(t *testing.T) {
	runner := &fakeRunner{err: exitWith(t, 66)}
	c := NewClient(runner, "/helper", "/data")
	_, _, err := c.HotspotSecret(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}
