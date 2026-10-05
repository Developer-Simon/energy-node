package steps_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

// fakeAptPending antwortet wie scripts/bootstrap/lib/apt_pending.py: mit
// --refresh scheitert es, wenn REFRESH_FAILS gesetzt ist (Exit 2, apt-Fehler
// auf stderr), sonst nennt es, ob frisch abgerufen wurde.
const fakeAptPending = `import json, os, sys
refresh = "--refresh" in sys.argv[1:]
if refresh and os.path.exists("/tmp/energy-node-installer-steps-test/refresh-fails"):
    print("E: Failed to fetch http://deb.debian.org", file=sys.stderr)
    sys.exit(2)
print(json.dumps({"count": 2 if refresh else 1, "checked_at": "2026-10-04T06:12:00+00:00",
    "packages": [{"name": "libssl3", "from": "3.0.11", "to": "3.0.13"}]}))
`

func TestQuerySystemUpdatesCountsAndRefreshesOnRequest(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)
	bundleDir, _ := deployBootstrapScripts(t, client, map[string]string{"lib/apt_pending.py": fakeAptPending})

	cached, err := steps.QuerySystemUpdates(context.Background(), client, bundleDir, false)
	if err != nil {
		t.Fatalf("cached: %v", err)
	}
	if cached == nil || cached.Count != 1 || cached.Packages[0].To != "3.0.13" {
		t.Fatalf("cached = %+v", cached)
	}
	fresh, err := steps.QuerySystemUpdates(context.Background(), client, bundleDir, true)
	if err != nil || fresh == nil || fresh.Count != 2 {
		t.Fatalf("fresh = %+v, %v", fresh, err)
	}
}

func TestQuerySystemUpdatesReportsAFailedRefreshWithAptsMessage(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)
	bundleDir, _ := deployBootstrapScripts(t, client, map[string]string{"lib/apt_pending.py": fakeAptPending})
	if err := client.UploadBytes([]byte("x"), "/tmp/energy-node-installer-steps-test/refresh-fails", 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.RemoveRemote("/tmp/energy-node-installer-steps-test/refresh-fails") })

	_, err := steps.QuerySystemUpdates(context.Background(), client, bundleDir, true)
	var failed *steps.RefreshFailedError
	if !errors.As(err, &failed) || failed.Detail != "E: Failed to fetch http://deb.debian.org" {
		t.Fatalf("err = %v, want RefreshFailedError with apt's message", err)
	}
}

func TestQuerySystemUpdatesKeepsAnUnknownCountNil(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)
	bundleDir, _ := deployBootstrapScripts(t, client, map[string]string{"lib/apt_pending.py": "print('null')\n"})
	got, err := steps.QuerySystemUpdates(context.Background(), client, bundleDir, false)
	if err != nil || got != nil {
		t.Fatalf("got %+v, %v, want nil without error", got, err)
	}
}
