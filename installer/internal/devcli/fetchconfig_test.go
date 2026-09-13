package devcli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/devcli"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

func requireSFTPServerForFetch(t *testing.T) {
	t.Helper()
	if _, ok := transporttest.SFTPServerPath(); !ok {
		t.Skip("no sftp-server binary found; install openssh-server to run this test")
	}
}

func dialForFetchTest(t *testing.T, sshd *transporttest.SSHD) *transport.Client {
	t.Helper()
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
	return client
}

func TestRunFetchConfigWritesANewLocalTemplateWithoutPrompting(t *testing.T) {
	requireSFTPServerForFetch(t)
	sshd := transporttest.Start(t)
	client := dialForFetchTest(t, sshd)

	remote := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(remote, []byte(`{"from":"node"}`), 0o644); err != nil {
		t.Fatalf("writing remote fixture: %v", err)
	}
	local := filepath.Join(t.TempDir(), "energy-node.config.json")

	err := devcli.RunFetchConfig(context.Background(), devcli.FetchConfigArgs{
		Client: client, RemoteConfigPath: remote, LocalTemplatePath: local,
		Confirm: func(string) (bool, error) { t.Fatalf("must not ask when no local template exists"); return false, nil },
		Stdout:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("RunFetchConfig: %v", err)
	}
	got, err := os.ReadFile(local)
	if err != nil || string(got) != `{"from":"node"}` {
		t.Fatalf("unexpected local content: %q, err=%v", got, err)
	}
}

func TestRunFetchConfigAsksBeforeOverwritingAnExistingTemplate(t *testing.T) {
	requireSFTPServerForFetch(t)
	sshd := transporttest.Start(t)
	client := dialForFetchTest(t, sshd)

	remote := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(remote, []byte(`{"from":"node"}`), 0o644)
	local := filepath.Join(t.TempDir(), "energy-node.config.json")
	os.WriteFile(local, []byte(`{"from":"repo"}`), 0o644)

	asked := false
	err := devcli.RunFetchConfig(context.Background(), devcli.FetchConfigArgs{
		Client: client, RemoteConfigPath: remote, LocalTemplatePath: local,
		Confirm: func(string) (bool, error) { asked = true; return true, nil },
		Stdout:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("RunFetchConfig: %v", err)
	}
	if !asked {
		t.Fatalf("expected to be asked before overwriting the existing template")
	}
	got, _ := os.ReadFile(local)
	if string(got) != `{"from":"node"}` {
		t.Fatalf("expected the template to be replaced, got %q", got)
	}
}

func TestRunFetchConfigDeclinedLeavesTheExistingTemplateUntouched(t *testing.T) {
	requireSFTPServerForFetch(t)
	sshd := transporttest.Start(t)
	client := dialForFetchTest(t, sshd)

	remote := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(remote, []byte(`{"from":"node"}`), 0o644)
	local := filepath.Join(t.TempDir(), "energy-node.config.json")
	os.WriteFile(local, []byte(`{"from":"repo"}`), 0o644)

	err := devcli.RunFetchConfig(context.Background(), devcli.FetchConfigArgs{
		Client: client, RemoteConfigPath: remote, LocalTemplatePath: local,
		Confirm: func(string) (bool, error) { return false, nil },
		Stdout:  &bytes.Buffer{},
	})
	if err == nil {
		t.Fatalf("expected an error when overwriting is declined")
	}
	got, _ := os.ReadFile(local)
	if string(got) != `{"from":"repo"}` {
		t.Fatalf("the existing template must stay untouched, got %q", got)
	}
}

func TestRunFetchConfigReportsAMissingRemoteFile(t *testing.T) {
	requireSFTPServerForFetch(t)
	sshd := transporttest.Start(t)
	client := dialForFetchTest(t, sshd)

	err := devcli.RunFetchConfig(context.Background(), devcli.FetchConfigArgs{
		Client:            client,
		RemoteConfigPath:  filepath.Join(t.TempDir(), "missing.json"),
		LocalTemplatePath: filepath.Join(t.TempDir(), "energy-node.config.json"),
		Stdout:            &bytes.Buffer{},
	})
	if err == nil {
		t.Fatalf("expected an error for a missing remote config.json")
	}
}
