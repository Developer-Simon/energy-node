package steps

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// randomSuffix returns a short random hex string for building unique
// staging paths -- see internal/bundle's identical helper (deploy.go) for
// why a fixed name is not safe: concurrent runs, or (proven while
// validating this plan) concurrent test binaries sharing one real
// filesystem via localhost sshd, must never share a staging path.
func randomSuffix() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("reading random bytes: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// dashboardStepID is the only step whose invocation takes password flags
// (Plan A-II, Task 11: "60-node-install.sh [--mqtt-password-file <pfad>]
// [--admin-password-file <pfad>]").
const dashboardStepID = "60"

// mosquittoStepID is the step that creates the broker user
// ("20-mosquitto.sh --user <name> --password-file <pfad>").
const mosquittoStepID = "20"

// mosquittoArgs renders the arguments step 20 expects. The password travels
// as a path, never as a value (Umgang mit Geheimnissen).
func mosquittoArgs(user, passwordPath string) string {
	return " --user " + transport.ShellQuote(user) + " --password-file " + transport.ShellQuote(passwordPath)
}

// DefaultNodeConfigPath is the dashboard's config on the node; it names the
// broker user and the 0600 file that holds its password.
const DefaultNodeConfigPath = "/etc/energy-node/config.json"

// mqttConfigUnreadable is the fault code for a run that has no credentials
// and cannot find them on the node either (same code as the updater's).
const mqttConfigUnreadable = "MQTT_CONFIG_UNREADABLE"

// readMqttConfigPy prints the broker user and its password file. The bundle's
// template lacks password_file on some versions, hence the default.
const readMqttConfigPy = `import json, sys
mqtt = json.load(open(sys.argv[1], encoding="utf-8")).get("mqtt", {})
print(mqtt.get("username", ""))
print(mqtt.get("password_file", "/etc/energy-node/mqtt.pw"))`

// nodeMosquittoArgs builds step 20's arguments from what is already on the
// node, as dashboard/energy-node-updater.sh does: the user and password
// file from the running config, or -- on a first install -- from the
// bundle's template. The password never leaves the node.
func nodeMosquittoArgs(ctx context.Context, opts RunOptions) (string, error) {
	configPath := opts.NodeConfigPath
	if configPath == "" {
		configPath = DefaultNodeConfigPath
	}
	candidates := []string{configPath, path.Join(opts.RemoteBundleDir, "config", "config.json")}
	var quoted []string
	for _, c := range candidates {
		quoted = append(quoted, transport.ShellQuote(c))
	}
	command := fmt.Sprintf(`for f in %s; do [ -f "$f" ] && python3 -c %s "$f" && exit 0; done; exit 1`,
		strings.Join(quoted, " "), transport.ShellQuote(readMqttConfigPy))
	var stdout, stderr bytes.Buffer
	if err := opts.Client.Run(ctx, command, &stdout, &stderr); err != nil {
		return "", fmt.Errorf("reading the MQTT config: %w (stderr: %s)", err, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 || lines[0] == "" || lines[1] == "" {
		return "", fmt.Errorf("the MQTT config names no user or password file")
	}
	user, passwordFile := lines[0], lines[1]
	if err := opts.Client.Run(ctx, "test -f "+transport.ShellQuote(passwordFile), io.Discard, io.Discard); err != nil {
		return "", fmt.Errorf("the MQTT password file %s does not exist", passwordFile)
	}
	return mosquittoArgs(user, passwordFile), nil
}

// stageSecretsForStep20 uploads the MQTT password to a 0600 temp file and
// returns step 20's arguments plus a cleanup that removes the file again.
func stageSecretsForStep20(client *transport.Client, user string, secrets *Secrets) (string, func(), error) {
	if user == "" || secrets == nil || secrets.MQTTPassword == "" {
		return "", func() {}, nil
	}
	remotePath := fmt.Sprintf("/tmp/energy-node-installer-mqtt20-%s.pw", randomSuffix())
	if err := client.UploadBytes([]byte(secrets.MQTTPassword), remotePath, 0o600); err != nil {
		return "", func() {}, fmt.Errorf("staging the MQTT password for step 20: %w", err)
	}
	return mosquittoArgs(user, remotePath), func() { _ = client.RemoveRemote(remotePath) }, nil
}

// Secrets holds the passwords a fresh install or a password change collects
// in the UI. Both are optional: 60-node-install.sh leaves an already-set
// password alone and reports a missing one as human text rather than
// failing (Plan A-II, Task 11) -- a hand-run bootstrap chain must still
// reach a working dashboard without the installer supplying credentials.
type Secrets struct {
	MQTTPassword  string
	AdminPassword string
}

// stageSecretsForStep60 uploads whichever passwords are set to 0600 temp
// files on the node -- each under a randomly suffixed name -- and returns
// the extra "--flag path" arguments 60-node-install.sh expects, plus a
// cleanup that removes them again. Secrets never survive on disk past the
// step that consumes them: a path is the argument, never the value itself,
// because /proc/<pid>/cmdline is world-readable (Umgang mit Geheimnissen in
// the spec).
func stageSecretsForStep60(client *transport.Client, secrets *Secrets) (extraArgs string, cleanup func(), err error) {
	var uploaded []string
	cleanup = func() {
		for _, p := range uploaded {
			_ = client.RemoveRemote(p)
		}
	}

	stage := func(value, namePrefix, flag string) error {
		if value == "" {
			return nil
		}
		remotePath := fmt.Sprintf("/tmp/energy-node-installer-%s-%s.pw", namePrefix, randomSuffix())
		if err := client.UploadBytes([]byte(value), remotePath, 0o600); err != nil {
			return err
		}
		uploaded = append(uploaded, remotePath)
		extraArgs += " " + flag + " " + transport.ShellQuote(remotePath)
		return nil
	}

	if err := stage(secrets.MQTTPassword, "mqtt", "--mqtt-password-file"); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("staging the MQTT password: %w", err)
	}
	if err := stage(secrets.AdminPassword, "admin", "--admin-password-file"); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("staging the admin password: %w", err)
	}
	return extraArgs, cleanup, nil
}
