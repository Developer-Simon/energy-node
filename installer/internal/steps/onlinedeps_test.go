package steps

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
)

// fakeRemote beantwortet jeden Aufruf von Run über handler und merkt sich die Befehle.
type fakeRemote struct {
	commands []string
	handler  func(command string) (output string, err error)
}

func (f *fakeRemote) Run(_ context.Context, command string, stdout, stderr io.Writer) error {
	f.commands = append(f.commands, command)
	output, err := f.handler(command)
	if output != "" {
		_, _ = io.WriteString(stderr, output)
	}
	return err
}

type exitError int

func (e exitError) Error() string   { return "exit" }
func (e exitError) ExitStatus() int { return int(e) }

// reply is one scripted answer of the pip install command.
type reply struct {
	output string
	err    error
}

// scripted answers the stamp check and the stamp write with the given errors
// and the install commands in order.
func scripted(stamp, write error, installs ...reply) func(string) (string, error) {
	n := 0
	return func(command string) (string, error) {
		switch {
		case strings.HasPrefix(command, "test -f "):
			return "", stamp
		case strings.HasPrefix(command, "mkdir -p "):
			return "", write
		}
		if n >= len(installs) {
			return "", errors.New("unexpected install call")
		}
		r := installs[n]
		n++
		return r.output, r.err
	}
}

func onlineDepsOptions(markers *[]Marker, logs *[]string) RunOptions {
	return RunOptions{
		RemoteBundleDir: "/var/lib/energy-node-installer/bundle",
		RemoteStateDir:  "/var/lib/energy-node-installer",
		BundleVersion:   "v1.4.2",
		OnMarker:        func(m Marker) { *markers = append(*markers, m) },
		OnLog:           func(_, line string) { *logs = append(*logs, line) },
	}
}

func markerKinds(markers []Marker) []Kind {
	kinds := make([]Kind, 0, len(markers))
	for _, m := range markers {
		kinds = append(kinds, m.Kind)
	}
	return kinds
}

func countContaining(commands []string, sub string) int {
	n := 0
	for _, c := range commands {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

func TestOnlineDepsSkipsWhenStampMatches(t *testing.T) {
	var markers []Marker
	var logs []string
	opts := onlineDepsOptions(&markers, &logs)
	r := &fakeRemote{handler: scripted(nil, nil)}

	m, err := installDepsOnline(context.Background(), r, opts, bundle.StepEntry{ID: "50", Fingerprint: "abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Kind != Skip || m.Detail != "bereits erledigt" {
		t.Fatalf("marker = %+v, want Skip with detail %q", m, "bereits erledigt")
	}
	if want := []Kind{Begin, Skip}; !reflect.DeepEqual(markerKinds(markers), want) {
		t.Fatalf("marker kinds = %v, want %v", markerKinds(markers), want)
	}
	if n := countContaining(r.commands, "pip install"); n != 0 {
		t.Errorf("install calls = %d, want 0", n)
	}
	if n := countContaining(r.commands, "mkdir -p "); n != 0 {
		t.Errorf("stamp write calls = %d, want 0", n)
	}
}

func TestOnlineDepsInstallsAndStamps(t *testing.T) {
	var markers []Marker
	var logs []string
	opts := onlineDepsOptions(&markers, &logs)
	r := &fakeRemote{handler: scripted(errors.New("nein"), nil, reply{})}

	m, err := installDepsOnline(context.Background(), r, opts, bundle.StepEntry{ID: "50", Fingerprint: "abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Kind != OK {
		t.Fatalf("marker = %+v, want OK", m)
	}
	if want := []Kind{Begin, OK}; !reflect.DeepEqual(markerKinds(markers), want) {
		t.Fatalf("marker kinds = %v, want %v", markerKinds(markers), want)
	}
	if len(r.commands) != 3 {
		t.Fatalf("commands = %d, want 3 (check, install, write): %v", len(r.commands), r.commands)
	}
	if !strings.HasPrefix(r.commands[0], "test -f ") {
		t.Errorf("first command = %q, want the stamp check", r.commands[0])
	}
	install := r.commands[1]
	if !strings.Contains(install, "pip install") {
		t.Errorf("second command = %q, want the install", install)
	}
	if !strings.Contains(install, "--break-system-packages") {
		t.Errorf("install command = %q, want --break-system-packages", install)
	}
	if !strings.Contains(install, "/var/lib/energy-node-installer/bundle/wheels") {
		t.Errorf("install command = %q, want the wheels directory", install)
	}
	write := r.commands[2]
	if !strings.Contains(write, "fingerprint=%s") || !strings.Contains(write, "abc123") {
		t.Errorf("write command = %q, want the fingerprint abc123", write)
	}
}

func TestOnlineDepsRetriesLegacyPip(t *testing.T) {
	var markers []Marker
	var logs []string
	opts := onlineDepsOptions(&markers, &logs)
	r := &fakeRemote{handler: scripted(errors.New("nein"), nil,
		reply{output: "no such option: --break-system-packages", err: errors.New("exit 2")},
		reply{},
	)}

	m, err := installDepsOnline(context.Background(), r, opts, bundle.StepEntry{ID: "50", Fingerprint: "abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Kind != OK {
		t.Fatalf("marker = %+v, want OK", m)
	}
	if n := countContaining(r.commands, "pip install"); n != 2 {
		t.Fatalf("install calls = %d, want 2", n)
	}
	var installs []string
	for _, c := range r.commands {
		if strings.Contains(c, "pip install") {
			installs = append(installs, c)
		}
	}
	if strings.Contains(installs[1], "--break-system-packages") {
		t.Errorf("second install = %q, must not contain --break-system-packages", installs[1])
	}
}

func TestOnlineDepsReportsExternallyManaged(t *testing.T) {
	var markers []Marker
	var logs []string
	opts := onlineDepsOptions(&markers, &logs)
	r := &fakeRemote{handler: scripted(errors.New("nein"), nil,
		reply{output: "error: externally-managed-environment", err: errors.New("exit 1")},
	)}

	m, err := installDepsOnline(context.Background(), r, opts, bundle.StepEntry{ID: "50", Fingerprint: "abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Kind != Fail || m.Detail != "PIP_EXTERNALLY_MANAGED" {
		t.Fatalf("marker = %+v, want Fail with PIP_EXTERNALLY_MANAGED", m)
	}
	if n := countContaining(r.commands, "mkdir -p "); n != 0 {
		t.Errorf("stamp write calls = %d, want 0", n)
	}
}

func TestOnlineDepsReportsMissingWheels(t *testing.T) {
	var markers []Marker
	var logs []string
	opts := onlineDepsOptions(&markers, &logs)
	r := &fakeRemote{handler: scripted(errors.New("nein"), nil,
		reply{err: exitError(70)},
	)}

	m, err := installDepsOnline(context.Background(), r, opts, bundle.StepEntry{ID: "50", Fingerprint: "abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Kind != Fail || m.Detail != "WHEELS_MISSING" {
		t.Fatalf("marker = %+v, want Fail with WHEELS_MISSING", m)
	}
}

func TestOnlineDepsReportsInstallFailure(t *testing.T) {
	var markers []Marker
	var logs []string
	opts := onlineDepsOptions(&markers, &logs)
	r := &fakeRemote{handler: scripted(errors.New("nein"), nil,
		reply{err: errors.New("boom")},
	)}

	m, err := installDepsOnline(context.Background(), r, opts, bundle.StepEntry{ID: "50", Fingerprint: "abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Kind != Fail || m.Detail != "PIP_INSTALL_FAILED" {
		t.Fatalf("marker = %+v, want Fail with PIP_INSTALL_FAILED", m)
	}
}

func TestOnlineDepsReturnsCancellation(t *testing.T) {
	var markers []Marker
	var logs []string
	opts := onlineDepsOptions(&markers, &logs)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &fakeRemote{handler: scripted(errors.New("nein"), nil,
		reply{err: context.Canceled},
	)}

	_, err := installDepsOnline(ctx, r, opts, bundle.StepEntry{ID: "50", Fingerprint: "abc123"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	for _, m := range markers {
		if m.Kind == Fail {
			t.Fatalf("got a Fail marker %+v on cancellation, want none", m)
		}
	}
}

func TestOnlineDepsStampWithoutFingerprint(t *testing.T) {
	const stamp = "/var/lib/energy-node-installer/steps/50"

	check := stampMatchesCommand(stamp, "", "v1.4.2")
	if !strings.Contains(check, "bundle=v1.4.2") {
		t.Errorf("stamp check = %q, want bundle=v1.4.2", check)
	}
	if strings.Contains(check, "fingerprint=") {
		t.Errorf("stamp check = %q, must not contain fingerprint=", check)
	}

	write := writeStampCommand(stamp, "", "v1.4.2")
	if strings.Contains(write, "fingerprint=") {
		t.Errorf("stamp write = %q, must not contain fingerprint=", write)
	}
}

func TestOnlineDepsQuotesWheelsDir(t *testing.T) {
	cmd := pipInstallCommand("/pfad mit leerzeichen/wheels", true)
	if !strings.Contains(cmd, "'/pfad mit leerzeichen/wheels'") {
		t.Errorf("command = %q, want the wheels directory in single quotes", cmd)
	}
}

func TestOnlineDepsLogsVersionMismatch(t *testing.T) {
	var markers []Marker
	var logs []string
	opts := onlineDepsOptions(&markers, &logs)
	r := &fakeRemote{handler: scripted(errors.New("nein"), nil, reply{})}

	if _, err := installDepsOnline(context.Background(), r, opts, bundle.StepEntry{ID: "50", Fingerprint: "abc123"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, line := range logs {
		if strings.Contains(line, "Python-Version weicht vom Paket ab") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("log lines = %q, want one with %q", logs, "Python-Version weicht vom Paket ab")
	}
}

func TestOnlineDepsPipIgnoresPackagesTheSystemOwns(t *testing.T) {
	// Debian ships urllib3 without a RECORD file; --upgrade would try to
	// uninstall it and fail with uninstall-no-record-file.
	cmd := pipInstallCommand("/var/lib/energy-node-installer/bundle/wheels", true)
	if !strings.Contains(cmd, "--ignore-installed") || strings.Contains(cmd, "--upgrade") {
		t.Errorf("command = %q, want --ignore-installed and no --upgrade", cmd)
	}
}
