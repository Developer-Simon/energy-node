package host_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/host"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// Der Compiler ist hier der eigentliche Test: der Wirt muss den vollstaendigen
// Vertrag erfuellen, sonst laesst sich die Oberflaeche gar nicht an ihn binden.
var _ hostapi.Backend = (*host.Host)(nil)
var _ hostapi.PackageBackend = (*host.Host)(nil)

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

func (nopSink) Marker(string, string, string)             {}
func (nopSink) Log(string, string)                        {}
func (nopSink) Message(string, string, map[string]string) {}

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

func TestManifestViewCountsTheBundleFilesAndCarriesKinds(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"wheels/a.whl": "x",
		"wheels/b.whl": "x",
		"dashboard/energy-node-dashboard.service": "x",
		"config/config.json":                      "x",
	}
	for rel := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Repeat("a", 100)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := map[string]any{
		"version": "v1.4.2", "arch": "armv6", "uname_machine": []string{"armv6l"},
		"steps": []map[string]any{{"id": "83", "optional": true, "service_id": "shelly", "kind": "device"}},
		"files": files,
	}
	raw, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := host.New(host.Config{BundleDir: dir, IdentityDir: t.TempDir(), KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := h.Describe().BundleArch; got != "armv6" {
		t.Errorf("BundleArch = %q, want armv6", got)
	}
	view, err := h.Manifest(context.Background())
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	if view.WheelCount != 2 || view.UnitCount != 1 || view.TemplateCount != 1 {
		t.Errorf("counts = %d/%d/%d, want 2/1/1", view.WheelCount, view.UnitCount, view.TemplateCount)
	}
	if view.BundleBytes != 400 {
		t.Errorf("BundleBytes = %d, want 400", view.BundleBytes)
	}
	if view.Steps[0].Kind != "device" {
		t.Errorf("kind = %q, want device", view.Steps[0].Kind)
	}
}

func TestAUTCTimezoneIsAWarning(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir)
	h, err := host.New(host.Config{BundleDir: dir, IdentityDir: t.TempDir(), KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	view := h.EvaluatePreflight(host.PreflightFacts{
		Arch: "armv6l", PythonABI: "cp311", DiskFreeMB: 4096, SudoNopasswd: true, Internet: true,
		Timezone: "Etc/UTC", OSPrettyName: "Raspberry Pi OS Lite 12 (bookworm)", DiskTotalMB: 29700,
	})
	if len(view.Warnings) != 1 || view.Warnings[0] != "TIMEZONE_UTC" {
		t.Errorf("warnings = %v, want TIMEZONE_UTC", view.Warnings)
	}
	if view.Timezone != "Etc/UTC" || view.OSPrettyName == "" || view.DiskTotalMB != 29700 {
		t.Errorf("the facts were not copied: %+v", view)
	}
}
