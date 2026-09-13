package steps_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/selection"
	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

func requireSFTPServerForSteps(t *testing.T) {
	t.Helper()
	if _, ok := transporttest.SFTPServerPath(); !ok {
		t.Skip("no sftp-server binary found; install openssh-sftp-server to run this test")
	}
}

func dialForStepsTest(t *testing.T, sshd *transporttest.SSHD) *transport.Client {
	t.Helper()
	store := transport.NewHostKeyStore(filepath.Join(t.TempDir(), "known_hosts"))
	callback, err := store.Callback(func(string, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := transport.Dial(ctx, transport.Config{
		Host:            sshd.Addr,
		User:            sshd.User(),
		PrivateKeyPEM:   sshd.ClientKeyPEM,
		HostKeyCallback: callback,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// deployBootstrapScripts packs scripts (id-suffix.sh -> body) into an
// archive under bootstrap/ and deploys it to a fresh remote directory,
// returning the extracted bundle dir and the still-separate state dir the
// scripts will read/write EN_STATE_DIR-relative files under.
func deployBootstrapScripts(t *testing.T, client *transport.Client, scripts map[string]string) (bundleDir, stateDir string) {
	t.Helper()
	src := t.TempDir()
	for name, body := range scripts {
		path := filepath.Join(src, "bootstrap", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	archive := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if out, err := exec.Command("tar", "-czf", archive, "-C", src, ".").CombinedOutput(); err != nil {
		t.Fatalf("tar: %v\n%s", err, out)
	}

	bundleDir = "/tmp/energy-node-installer-steps-test/" + t.Name() + "/bundle"
	stateDir = "/tmp/energy-node-installer-steps-test/" + t.Name() + "/state"
	if err := bundle.Deploy(context.Background(), client, archive, bundleDir); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Run(context.Background(), "rm -rf /tmp/energy-node-installer-steps-test/"+t.Name(), &bytes.Buffer{}, &bytes.Buffer{})
	})
	return bundleDir, stateDir
}

const okScript = "#!/bin/sh\nprintf '##STEP %s begin\\n'\nprintf 'working...\\n'\nprintf '##STEP %s ok\\n'\n"

func scriptBody(id, tmpl string) string { return fmt.Sprintf(tmpl, id, id) }

func TestRunExecutesStepsInOrderAndReportsMarkers(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{
		"10-apt.sh":       scriptBody("10", okScript),
		"40-tailscale.sh": scriptBody("40", okScript),
	})

	var markers []steps.Marker
	var logs []string
	err := steps.Run(context.Background(), steps.RunOptions{
		Client:          client,
		RemoteBundleDir: bundleDir,
		RemoteStateDir:  stateDir,
		BundleVersion:   "v0.1.0",
		Steps:           []bundle.StepEntry{{ID: "10", Optional: false}, {ID: "40", Optional: true}},
		OnMarker:        func(m steps.Marker) { markers = append(markers, m) },
		OnLog:           func(stepID, line string) { logs = append(logs, stepID+": "+line) },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []steps.Marker{
		{StepID: "10", Kind: steps.Begin}, {StepID: "10", Kind: steps.OK},
		{StepID: "40", Kind: steps.Begin}, {StepID: "40", Kind: steps.OK},
	}
	if len(markers) != len(want) {
		t.Fatalf("got %d markers, want %d: %+v", len(markers), len(want), markers)
	}
	for i, m := range want {
		if markers[i] != m {
			t.Fatalf("marker %d: got %+v, want %+v", i, markers[i], m)
		}
	}
	if len(logs) != 2 || logs[0] != "10: working..." || logs[1] != "40: working..." {
		t.Fatalf("unexpected human-text log: %v", logs)
	}
}

func TestRunStopsAtTheFirstFailure(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	const failScript = "#!/bin/sh\nprintf '##STEP %s begin\\n'\nprintf '##STEP %s fail SOME_CODE\\n'\n"
	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{
		"10-apt.sh":     scriptBody("10", okScript),
		"50-python.sh":  scriptBody("50", failScript),
		"60-install.sh": scriptBody("60", okScript),
	})

	var ran []string
	err := steps.Run(context.Background(), steps.RunOptions{
		Client:          client,
		RemoteBundleDir: bundleDir,
		RemoteStateDir:  stateDir,
		BundleVersion:   "v0.1.0",
		Steps: []bundle.StepEntry{
			{ID: "10", Optional: false}, {ID: "50", Optional: false}, {ID: "60", Optional: false},
		},
		OnMarker: func(m steps.Marker) {
			if m.Kind == steps.Begin {
				ran = append(ran, m.StepID)
			}
		},
	})

	var failure *steps.StepFailure
	if err == nil {
		t.Fatalf("expected a *steps.StepFailure")
	}
	if fail, ok := err.(*steps.StepFailure); ok {
		failure = fail
	} else {
		t.Fatalf("expected *steps.StepFailure, got %T: %v", err, err)
	}
	if failure.StepID != "50" || failure.Code != "SOME_CODE" {
		t.Fatalf("unexpected failure: %+v", failure)
	}
	if len(ran) != 2 || ran[0] != "10" || ran[1] != "50" {
		t.Fatalf("step 60 must not have started after 50 failed: %v", ran)
	}
}

func TestRunUploadsSelectionBeforeAnyStep(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	// This script does not merely print OK -- it reads $EN_SELECTION itself,
	// the same way a real bootstrap step's step_selected would, and fails
	// loudly if the file Run was supposed to stage isn't there yet.
	const checkSelectionScript = `#!/bin/sh
printf '##STEP 40 begin\n'
if [ ! -f "$EN_SELECTION" ]; then
  printf '##STEP 40 fail SELECTION_MISSING\n'
  exit 0
fi
if ! grep -q '"70":false' "$EN_SELECTION"; then
  printf '##STEP 40 fail SELECTION_WRONG_CONTENT\n'
  exit 0
fi
printf '##STEP 40 ok\n'
`
	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{
		"40-tailscale.sh": checkSelectionScript,
	})

	sel := &selection.Selection{Steps: map[string]bool{"70": false}}
	err := steps.Run(context.Background(), steps.RunOptions{
		Client:          client,
		RemoteBundleDir: bundleDir,
		RemoteStateDir:  stateDir,
		BundleVersion:   "v0.1.0",
		Steps:           []bundle.StepEntry{{ID: "40", Optional: true}},
		Selection:       sel,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The uploaded file must also be valid JSON matching what selection.Save
	// would have produced -- both are Selection's own encoding.
	var stdout bytes.Buffer
	if err := client.Run(context.Background(), "cat "+stateDir+"/selection.json", &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("reading uploaded selection.json: %v", err)
	}
	var roundTripped selection.Selection
	if err := json.Unmarshal(stdout.Bytes(), &roundTripped); err != nil {
		t.Fatalf("uploaded selection.json is not valid JSON: %v (%q)", err, stdout.String())
	}
	if roundTripped.Selected("70") {
		t.Fatalf("uploaded selection.json does not reflect the deselection of 70")
	}
}

func TestRunInlinesEnvironmentInsteadOfUsingSSHSetenv(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	const echoEnvScript = `#!/bin/sh
printf '##STEP 60 begin\n'
if [ "$EN_BUNDLE_VERSION" != "v9.9.9" ] || [ "$EN_TARGET_USER" != "pruef" ]; then
  printf '##STEP 60 fail ENV_MISSING\n'
  exit 0
fi
printf '##STEP 60 ok\n'
`
	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{
		"60-node-install.sh": echoEnvScript,
	})

	err := steps.Run(context.Background(), steps.RunOptions{
		Client:          client,
		RemoteBundleDir: bundleDir,
		RemoteStateDir:  stateDir,
		BundleVersion:   "v9.9.9",
		TargetUser:      "pruef",
		Steps:           []bundle.StepEntry{{ID: "60", Optional: false}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}
