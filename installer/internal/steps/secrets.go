package steps

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

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
