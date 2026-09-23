package devcli

import (
	"bytes"
	"context"
	"io"
	"path"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

// restartFixture uploads an installed manifest to a fixture sshd and swaps
// restartUnits for a recorder. The manifest read is real; systemctl is not.
func restartFixture(t *testing.T) (*transport.Client, string, *[]string) {
	t.Helper()
	if _, ok := transporttest.SFTPServerPath(); !ok {
		t.Skip("no sftp-server binary found; install openssh-server to run this test")
	}
	sshd := transporttest.Start(t)
	store := transport.NewHostKeyStore(filepath.Join(t.TempDir(), "known_hosts"))
	callback, err := store.Callback(func(string, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}
	client, err := transport.Dial(context.Background(), transport.Config{
		Host: sshd.Addr, User: sshd.User(), PrivateKeyPEM: sshd.ClientKeyPEM, HostKeyCallback: callback,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	bundleDir := t.TempDir()
	manifestJSON := `{"version":"v0.7.9","steps":[
		{"id":"50"},{"id":"60"},
		{"id":"81","optional":true,"service_id":"apsystems","unit":"apsystems-ez1.service"},
		{"id":"83","optional":true,"service_id":"shelly","unit":"shelly-rpc.service"}]}`
	if err := client.UploadBytes([]byte(manifestJSON), path.Join(bundleDir, "manifest.json"), 0o644); err != nil {
		t.Fatalf("uploading manifest.json: %v", err)
	}

	var calls []string
	orig := restartUnits
	t.Cleanup(func() { restartUnits = orig })
	restartUnits = func(_ context.Context, _ *transport.Client, verb string, units []string, _ io.Writer) error {
		for _, u := range units {
			calls = append(calls, verb+" "+u)
		}
		return nil
	}
	return client, bundleDir, &calls
}

func TestRunRestartWithoutOnlyTryRestartsTheDashboardAndEveryService(t *testing.T) {
	client, bundleDir, calls := restartFixture(t)

	err := RunRestart(context.Background(), RestartArgs{Client: client, RemoteBundleDir: bundleDir, Stdout: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("RunRestart: %v", err)
	}
	want := []string{
		"try-restart energy-node-dashboard.service",
		"try-restart apsystems-ez1.service",
		"try-restart shelly-rpc.service",
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %v, want %v", *calls, want)
	}
}

func TestRunRestartOnlyRestartsThatOneUnit(t *testing.T) {
	for _, tc := range []struct{ only, want string }{
		{"shelly", "restart shelly-rpc.service"},
		{"dashboard", "restart energy-node-dashboard.service"},
	} {
		t.Run(tc.only, func(t *testing.T) {
			client, bundleDir, calls := restartFixture(t)
			err := RunRestart(context.Background(), RestartArgs{Client: client, RemoteBundleDir: bundleDir, Only: tc.only, Stdout: &bytes.Buffer{}})
			if err != nil {
				t.Fatalf("RunRestart: %v", err)
			}
			if !reflect.DeepEqual(*calls, []string{tc.want}) {
				t.Fatalf("calls = %v, want [%s]", *calls, tc.want)
			}
		})
	}
}

func TestRunRestartRejectsATargetWithoutAUnit(t *testing.T) {
	for _, only := range []string{"wheels", "no-such-service"} {
		t.Run(only, func(t *testing.T) {
			client, bundleDir, calls := restartFixture(t)
			err := RunRestart(context.Background(), RestartArgs{Client: client, RemoteBundleDir: bundleDir, Only: only, Stdout: &bytes.Buffer{}})
			if err == nil {
				t.Fatalf("expected an error for --only %s", only)
			}
			if len(*calls) != 0 {
				t.Fatalf("nothing may restart on an error, calls = %v", *calls)
			}
		})
	}
}
