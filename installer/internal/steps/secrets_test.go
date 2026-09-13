package steps_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

const checkSecretsScript = `#!/bin/sh
printf '##STEP 60 begin\n'
mqtt_file=""
admin_file=""
while [ $# -gt 0 ]; do
  case "$1" in
    --mqtt-password-file) mqtt_file="$2"; shift 2 ;;
    --admin-password-file) admin_file="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ -z "$mqtt_file" ] || [ -z "$admin_file" ]; then
  printf '##STEP 60 fail MISSING_FLAGS\n'
  exit 0
fi
if [ "$(cat "$mqtt_file")" != "mqtt-geheim" ] || [ "$(cat "$admin_file")" != "admin-geheim" ]; then
  printf '##STEP 60 fail WRONG_CONTENT\n'
  exit 0
fi
if [ "$(stat -c %a "$mqtt_file")" != "600" ]; then
  printf '##STEP 60 fail WRONG_MODE\n'
  exit 0
fi
echo "$mqtt_file" > /tmp/energy-node-installer-secrets-test-mqtt-path
printf '##STEP 60 ok\n'
`

func trimNewlineForSteps(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func TestRunStagesSecretsForStep60AndCleansUpAfterwards(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{
		"60-node-install.sh": checkSecretsScript,
	})
	t.Cleanup(func() {
		_ = client.RemoveRemote("/tmp/energy-node-installer-secrets-test-mqtt-path")
	})

	err := steps.Run(context.Background(), steps.RunOptions{
		Client:          client,
		RemoteBundleDir: bundleDir,
		RemoteStateDir:  stateDir,
		BundleVersion:   "v0.1.0",
		Steps:           []bundle.StepEntry{{ID: "60", Optional: false}},
		Secrets:         &steps.Secrets{MQTTPassword: "mqtt-geheim", AdminPassword: "admin-geheim"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var pathOut bytes.Buffer
	if err := client.Run(context.Background(), "cat /tmp/energy-node-installer-secrets-test-mqtt-path", &pathOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("reading recorded mqtt path: %v", err)
	}
	stagedPath := trimNewlineForSteps(pathOut.String())

	var stateOut bytes.Buffer
	_ = client.Run(context.Background(), "test -e "+stagedPath+" && echo present || echo gone", &stateOut, &bytes.Buffer{})
	if trimNewlineForSteps(stateOut.String()) != "gone" {
		t.Fatalf("staged secret file was not cleaned up: %q", stagedPath)
	}
}

func TestRunDoesNotPassSecretFlagsToOtherSteps(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	const rejectAnyArgScript = `#!/bin/sh
printf '##STEP 10 begin\n'
if [ $# -gt 0 ]; then
  printf '##STEP 10 fail UNEXPECTED_ARGS\n'
  exit 0
fi
printf '##STEP 10 ok\n'
`
	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{"10-apt.sh": rejectAnyArgScript})

	err := steps.Run(context.Background(), steps.RunOptions{
		Client:          client,
		RemoteBundleDir: bundleDir,
		RemoteStateDir:  stateDir,
		BundleVersion:   "v0.1.0",
		Steps:           []bundle.StepEntry{{ID: "10", Optional: false}},
		Secrets:         &steps.Secrets{MQTTPassword: "mqtt-geheim", AdminPassword: "admin-geheim"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}
