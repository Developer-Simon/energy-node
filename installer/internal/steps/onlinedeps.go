package steps

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// pythonDepsStepID is step 50, which installs the bundle's wheels offline.
const pythonDepsStepID = "50"

// Exit code of the install command when the bundle carries no pure Python
// wheel to start from.
const exitNoWheels = 70

// remoteRunner is what installDepsOnline needs from the connection; the real
// *transport.Client satisfies it.
type remoteRunner interface {
	Run(ctx context.Context, command string, stdout, stderr io.Writer) error
}

// runOnlineDeps replaces step 50 when the operator allowed loading the Python
// dependencies online because the node's Python version differs from the one
// the bundle's wheels were built for.
func runOnlineDeps(ctx context.Context, opts RunOptions, step bundle.StepEntry) (Marker, error) {
	return installDepsOnline(ctx, opts.Client, opts, step)
}

// installDepsOnline does what 50-python-deps.sh does, with one difference:
// only the pure Python wheels of the bundle are installed from the bundle,
// pip fetches everything else (compiled wheels for the node's Python, such as
// cryptography) from the node's package index, which on Raspberry Pi OS is
// piwheels. It then writes the stamp step 50 would have written, so that the
// bundle's own step 50 (which stops on the Python ABI) treats the work as done.
//
// It lives in the installer and not in the bundle so that it works for every
// package, including releases that predate it.
func installDepsOnline(ctx context.Context, r remoteRunner, opts RunOptions, step bundle.StepEntry) (Marker, error) {
	emit := func(kind Kind, detail string) Marker {
		m := Marker{StepID: step.ID, Kind: kind, Detail: detail}
		if opts.OnMarker != nil {
			opts.OnMarker(m)
		}
		return m
	}
	logLine := func(line string) {
		if opts.OnLog != nil {
			opts.OnLog(step.ID, line)
		}
	}
	emit(Begin, "")

	stamp := path.Join(opts.RemoteStateDir, "steps", step.ID)
	if r.Run(ctx, stampMatchesCommand(stamp, step.Fingerprint, opts.BundleVersion), io.Discard, io.Discard) == nil {
		return emit(Skip, "bereits erledigt"), nil
	}

	logLine("Python-Version weicht vom Paket ab: reine Python-Wheels aus dem Bundle, der Rest aus dem Netz (freigegeben).")
	wheelsDir := path.Join(opts.RemoteBundleDir, "wheels")

	var output strings.Builder
	sink := &lineWriter{onLine: func(line string) {
		output.WriteString(line + "\n")
		logLine("    " + line)
	}}
	err := r.Run(ctx, pipInstallCommand(wheelsDir, true), sink, sink)
	sink.flush()
	if err != nil && strings.Contains(output.String(), "no such option") {
		// Legacy images ship a pip without --break-system-packages.
		logLine("Legacy-Image ohne --break-system-packages: zweiter Versuch ohne den Schalter.")
		output.Reset()
		err = r.Run(ctx, pipInstallCommand(wheelsDir, false), sink, sink)
		sink.flush()
	}
	if err != nil {
		if ctx.Err() != nil {
			return Marker{}, ctx.Err()
		}
		return emit(Fail, onlineDepsFailureCode(err, output.String())), nil
	}

	if err := r.Run(ctx, writeStampCommand(stamp, step.Fingerprint, opts.BundleVersion), io.Discard, io.Discard); err != nil {
		return Marker{}, fmt.Errorf("writing the stamp of step %s: %w", step.ID, err)
	}
	return emit(OK, ""), nil
}

// onlineDepsFailureCode maps a failed pip run to the codes step 50 reports.
func onlineDepsFailureCode(err error, output string) string {
	if exit, ok := err.(interface{ ExitStatus() int }); ok && exit.ExitStatus() == exitNoWheels {
		return "WHEELS_MISSING"
	}
	if strings.Contains(output, "externally-managed-environment") {
		return "PIP_EXTERNALLY_MANAGED"
	}
	return "PIP_INSTALL_FAILED"
}

// pipInstallCommand installs the pure Python wheels of the bundle. Run from
// the wheels directory with a relative glob, so no file name needs quoting.
func pipInstallCommand(wheelsDir string, breakSystemPackages bool) string {
	flag := ""
	if breakSystemPackages {
		flag = " --break-system-packages"
	}
	return fmt.Sprintf(
		`cd %s && set -- ./*-none-any.whl && [ -e "$1" ] || exit %d; sudo python3 -m pip install --upgrade%s "$@"`,
		transport.ShellQuote(wheelsDir), exitNoWheels, flag)
}

// stampMatchesCommand succeeds when the stamp says step 50 is done for this
// bundle. Like step_done in step.sh: with a fingerprint it must match, without
// one the bundle version decides.
func stampMatchesCommand(stamp, fingerprint, bundleVersion string) string {
	want := "bundle=" + bundleVersion
	if fingerprint != "" {
		want = "fingerprint=" + fingerprint
	}
	return fmt.Sprintf("test -f %s && grep -qxF %s %s",
		transport.ShellQuote(stamp), transport.ShellQuote(want), transport.ShellQuote(stamp))
}

// writeStampCommand writes the same lines as step_write_stamp in step.sh.
func writeStampCommand(stamp, fingerprint, bundleVersion string) string {
	content := `printf 'bundle=%s\nzeit=%s\n' ` + transport.ShellQuote(bundleVersion) + ` "$(date -Is)"`
	if fingerprint != "" {
		content += `; printf 'fingerprint=%s\n' ` + transport.ShellQuote(fingerprint)
	}
	return fmt.Sprintf("mkdir -p %s && { %s; } > %s",
		transport.ShellQuote(path.Dir(stamp)), content, transport.ShellQuote(stamp))
}
