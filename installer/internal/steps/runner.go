package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/selection"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// RunOptions configures one call to Run: the connection to use, where the
// bundle was deployed (Task 8's Deploy), which steps to run and in what
// order, and where to send progress. Steps is the whole filtering
// mechanism -- Plan B-II passes manifest.Steps for a full run and a single
// entry for the developer CLI's --only.
type RunOptions struct {
	Client          *transport.Client
	RemoteBundleDir string // where Deploy extracted the bundle
	RemoteStateDir  string // e.g. /var/lib/energy-node-installer
	BundleVersion   string // manifest.Version, becomes EN_BUNDLE_VERSION
	TargetUser      string // optional; becomes EN_TARGET_USER if set
	TargetBase      string // optional; becomes EN_TARGET_BASE if set
	Steps           []bundle.StepEntry
	Selection       *selection.Selection // optional; uploaded before the first step
	OnMarker        func(Marker)
	OnLog           func(stepID, line string)
}

// StepFailure is returned by Run when a step reports "fail". Code is the
// stable fault code from the marker (e.g. "PIP_EXTERNALLY_MANAGED");
// internal/faults (Plan B-II) maps it to operator-facing text.
type StepFailure struct {
	StepID string
	Code   string
}

func (e *StepFailure) Error() string {
	return fmt.Sprintf("step %s failed: %s", e.StepID, e.Code)
}

// Run executes opts.Steps in order over opts.Client. It uploads
// opts.Selection to <RemoteStateDir>/selection.json first if one is given,
// then runs each step's script and waits for a terminal marker (ok, skip or
// fail). It stops at the first fail and returns a *StepFailure; it does not
// filter which steps run by selection -- see Task 10's rationale -- so
// Steps itself is the only filter.
func Run(ctx context.Context, opts RunOptions) error {
	if opts.Selection != nil {
		raw, err := json.Marshal(opts.Selection)
		if err != nil {
			return fmt.Errorf("encoding selection: %w", err)
		}
		remoteSelection := path.Join(opts.RemoteStateDir, "selection.json")
		if err := opts.Client.UploadBytes(raw, remoteSelection, 0o644); err != nil {
			return fmt.Errorf("uploading selection.json: %w", err)
		}
	}

	for _, step := range opts.Steps {
		terminal, err := runOneStep(ctx, opts, step)
		if err != nil {
			return err
		}
		if terminal.Kind == Fail {
			return &StepFailure{StepID: step.ID, Code: terminal.Detail}
		}
	}
	return nil
}

func runOneStep(ctx context.Context, opts RunOptions, step bundle.StepEntry) (Marker, error) {
	bootstrapDir := path.Join(opts.RemoteBundleDir, "bootstrap")
	scriptPath, err := resolveScriptPath(ctx, opts.Client, bootstrapDir, step.ID)
	if err != nil {
		return Marker{}, err
	}

	env := map[string]string{
		"EN_STATE_DIR": opts.RemoteStateDir,
		// Set explicitly rather than relying on lib/step.sh's own default
		// (EN_SELECTION:-EN_STATE_DIR/selection.json): a step invoked this
		// way is a single "bash script.sh" command, and nothing guarantees
		// every script sources that library before touching the variable.
		"EN_SELECTION":      path.Join(opts.RemoteStateDir, "selection.json"),
		"EN_BUNDLE_DIR":     opts.RemoteBundleDir,
		"EN_BUNDLE_VERSION": opts.BundleVersion,
	}
	if opts.TargetUser != "" {
		env["EN_TARGET_USER"] = opts.TargetUser
	}
	if opts.TargetBase != "" {
		env["EN_TARGET_BASE"] = opts.TargetBase
	}
	command := transport.BuildCommand(env, "bash "+transport.ShellQuote(scriptPath))

	var terminal Marker
	haveTerminal := false
	stdout := &lineWriter{onLine: func(line string) {
		if m, ok := ParseMarker(line); ok {
			if opts.OnMarker != nil {
				opts.OnMarker(m)
			}
			if m.Kind != Begin {
				terminal, haveTerminal = m, true
			}
			return
		}
		if opts.OnLog != nil {
			opts.OnLog(step.ID, line)
		}
	}}

	var stderr bytes.Buffer
	runErr := opts.Client.Run(ctx, command, stdout, &stderr)
	stdout.flush()

	if !haveTerminal {
		return Marker{}, fmt.Errorf("step %s ended without a terminal marker: %v (stderr: %q)", step.ID, runErr, stderr.String())
	}
	return terminal, nil
}

// resolveScriptPath finds the one bootstrap script for a step id. The
// manifest carries only ids, not filenames -- 10-apt.sh, 81-apsystems.sh
// and so on follow the "<id>-*.sh" convention that Komponente A owns, so a
// second id-to-filename table in Go would just be another place for the
// two sides to drift apart.
func resolveScriptPath(ctx context.Context, client *transport.Client, bootstrapDir, stepID string) (string, error) {
	pattern := stepID + "-*.sh"
	command := fmt.Sprintf("find %s -maxdepth 1 -name %s",
		transport.ShellQuote(bootstrapDir), transport.ShellQuote(pattern))
	var stdout, stderr bytes.Buffer
	if err := client.Run(ctx, command, &stdout, &stderr); err != nil {
		return "", fmt.Errorf("locating script for step %s: %w (stderr: %s)", stepID, err, stderr.String())
	}
	matches := strings.Fields(stdout.String())
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no bootstrap script found for step %s in %s", stepID, bootstrapDir)
	default:
		return "", fmt.Errorf("more than one bootstrap script matches step %s: %v", stepID, matches)
	}
}

// lineWriter buffers partial writes and calls onLine once per complete
// line, trimming a trailing \r so it behaves the same whether the remote
// shell emits \n or \r\n. flush must be called after the writer will
// receive no more data, to deliver a final line that never got a
// terminating \n.
type lineWriter struct {
	buf    []byte
	onLine func(line string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		w.onLine(strings.TrimSuffix(string(w.buf[:idx]), "\r"))
		w.buf = w.buf[idx+1:]
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	if len(w.buf) > 0 {
		w.onLine(string(w.buf))
		w.buf = nil
	}
}
