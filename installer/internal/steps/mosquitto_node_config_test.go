package steps_test

import (
	"context"
	"errors"
	"path"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

// Fails the way 20-mosquitto.sh does when it gets no usable arguments.
const mosquittoArgsScript = `#!/bin/sh
printf '##STEP 20 begin\n'
user=""; file=""
while [ $# -gt 0 ]; do
  case "$1" in
    --user) user="$2"; shift 2 ;;
    --password-file) file="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ -z "$user" ] || [ -z "$file" ] || [ ! -f "$file" ]; then
  printf '##STEP 20 fail MOSQUITTO_ARGS_MISSING\n'
  exit 0
fi
printf 'user=%s file=%s\n' "$user" "$file"
printf '##STEP 20 ok\n'
`

// A redeploy or repair carries no credentials; step 20 must then be handed
// the user and password file the node's own config already names, like the
// dashboard's updater does.
func TestRunTakesTheMosquittoArgumentsFromTheNodeWhenNoneAreGiven(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)
	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{"20-mosquitto.sh": mosquittoArgsScript})

	dir := path.Dir(stateDir)
	pwFile := dir + "/mqtt.pw"
	config := dir + "/config.json"
	if err := client.UploadBytes([]byte("geheim"), pwFile, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := client.UploadBytes([]byte(`{"mqtt": {"username": "energynode", "password_file": "`+pwFile+`"}}`), config, 0o644); err != nil {
		t.Fatal(err)
	}

	var logs []string
	err := steps.Run(context.Background(), steps.RunOptions{
		Client: client, RemoteBundleDir: bundleDir, RemoteStateDir: stateDir, BundleVersion: "v0.1.0",
		Steps:          []bundle.StepEntry{{ID: "20"}},
		NodeConfigPath: config,
		OnLog:          func(_, line string) { logs = append(logs, line) },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "user=energynode file=" + pwFile
	if len(logs) != 1 || logs[0] != want {
		t.Fatalf("logs = %q, want [%q]", logs, want)
	}
}

func TestRunReportsAMissingNodeConfigAsAnUnreadableMqttConfig(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)
	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{"20-mosquitto.sh": mosquittoArgsScript})

	err := steps.Run(context.Background(), steps.RunOptions{
		Client: client, RemoteBundleDir: bundleDir, RemoteStateDir: stateDir, BundleVersion: "v0.1.0",
		Steps:          []bundle.StepEntry{{ID: "20"}},
		NodeConfigPath: path.Dir(stateDir) + "/gibt-es-nicht.json",
	})
	var failure *steps.StepFailure
	if !errors.As(err, &failure) || failure.StepID != "20" || failure.Code != "MQTT_CONFIG_UNREADABLE" {
		t.Fatalf("err = %v, want step 20 MQTT_CONFIG_UNREADABLE", err)
	}
}
