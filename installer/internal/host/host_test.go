package host_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/host"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// Der Compiler ist hier der eigentliche Test: der Wirt muss den vollstaendigen
// Vertrag erfuellen, sonst laesst sich die Oberflaeche gar nicht an ihn binden.
var _ hostapi.Backend = (*host.Host)(nil)

func writeManifest(t *testing.T, dir string) {
	t.Helper()
	manifest := map[string]any{
		"version":       "v1.4.2",
		"uname_machine": []string{"armv6l"},
		"python_abi":    "cp311",
		"python_minor":  "3.11",
		"target_user":   "orgelbau",
		"target_base":   "/opt/energy-node",
		"components":    map[string]string{"dashboard": "v2.1.0"},
		"steps": []map[string]any{
			{"id": "10", "optional": false},
			{"id": "40", "optional": true, "default": true},
		},
		"files": map[string]string{},
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func TestDescribeReportsAnInstallerHostThatNeedsAConnection(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir)
	h, err := host.New(host.Config{BundleDir: dir, IdentityDir: t.TempDir(), KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := h.Describe()
	if got.Host != hostapi.HostInstaller || !got.NeedsConnection {
		t.Errorf("Describe() = %+v", got)
	}
	if got.BundleVersion != "v1.4.2" {
		t.Errorf("bundle version = %q, want the manifest's", got.BundleVersion)
	}
	if len(got.EntryPoints) != 3 {
		t.Errorf("entry points = %v, want install, redeploy and diagnose", got.EntryPoints)
	}
}

func TestManifestViewMirrorsTheBundleManifest(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir)
	h, err := host.New(host.Config{BundleDir: dir, IdentityDir: t.TempDir(), KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	view, err := h.Manifest(context.Background())
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	if view.BundleVersion != "v1.4.2" || view.TargetUser != "orgelbau" {
		t.Errorf("ManifestView = %+v", view)
	}
	if len(view.Steps) != 2 || !view.Steps[1].Optional || !view.Steps[1].Default {
		t.Errorf("steps = %+v, want the optional step to keep its default", view.Steps)
	}
}

func TestEveryConnectedCallFailsBeforeConnect(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir)
	h, err := host.New(host.Config{BundleDir: dir, IdentityDir: t.TempDir(), KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if _, err := h.Precheck(ctx); err == nil {
		t.Errorf("Precheck without a connection must fail")
	}
	if _, err := h.Plan(ctx); err == nil {
		t.Errorf("Plan without a connection must fail")
	}
	if _, err := h.Diagnose(ctx); err == nil {
		t.Errorf("Diagnose without a connection must fail")
	}
	if err := h.Run(ctx, hostapi.RunRequest{Mode: hostapi.ModeInstall}, nopSink{}); err == nil {
		t.Errorf("Run without a connection must fail")
	}
}

type nopSink struct{}

func (nopSink) Marker(string, string, string) {}
func (nopSink) Log(string, string)            {}

func TestPrecheckComparesTheNodeFactsAgainstTheManifest(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir)
	h, err := host.New(host.Config{BundleDir: dir, IdentityDir: t.TempDir(), KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Die reine Bewertungsfunktion ist exportiert, damit genau dieser
	// Vergleich ohne SSH pruefbar ist - er ist die einzige Entscheidung, die
	// dieser Wirt selbst trifft.
	view := h.EvaluatePreflight(host.PreflightFacts{
		OSID: "debian", OSVersionID: "12", Arch: "aarch64", PythonABI: "cp311",
		DiskFreeMB: 4096, SudoNopasswd: true, Internet: true,
	})
	if view.ArchOK {
		t.Errorf("aarch64 must not pass a bundle built for armv6l")
	}
	if !view.PythonABIOK {
		t.Errorf("cp311 matches the manifest and must pass")
	}
	if len(view.Blocking) == 0 || view.Blocking[0] != "ARCH_MISMATCH" {
		t.Errorf("blocking = %v, want ARCH_MISMATCH", view.Blocking)
	}

	ok := h.EvaluatePreflight(host.PreflightFacts{
		OSID: "debian", OSVersionID: "12", Arch: "armv6l", PythonABI: "cp311",
		DiskFreeMB: 200, SudoNopasswd: false, Internet: false,
	})
	if len(ok.Blocking) != 0 {
		t.Errorf("blocking = %v, want nothing blocking - low disk and no sudo are warnings", ok.Blocking)
	}
	if len(ok.Warnings) != 3 {
		t.Errorf("warnings = %v, want disk, sudo and internet", ok.Warnings)
	}
}
