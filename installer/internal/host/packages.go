package host

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/bundlesource"
	"github.com/Developer-Simon/energy-node-installer/internal/selection"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// packageSteps maps a pinned package to the step that installs it. A package
// that maps to no step never asks.
var packageSteps = map[string]string{"tailscale": "40", "caddy": "70"}

// packageBinaries names the file that proves a package is installed even when
// dpkg does not list it.
var packageBinaries = map[string]string{"tailscale": "/usr/sbin/tailscaled", "caddy": "/usr/bin/caddy"}

// packageInstalled and installFromRepo are seams for tests; production runs
// the defaults below.
var (
	packageInstalled = defaultPackageInstalled
	installFromRepo  = defaultInstallFromRepo
)

func defaultPackageInstalled(ctx context.Context, client *transport.Client, name string) bool {
	bin := packageBinaries[name]
	if bin == "" {
		bin = "/nonexistent"
	}
	check := fmt.Sprintf(
		"dpkg-query -W -f='${Status}' %s 2>/dev/null | grep -q '^install ok installed$' || test -x %s",
		transport.ShellQuote(name), transport.ShellQuote(bin))
	return client.Run(ctx, check, &strings.Builder{}, &strings.Builder{}) == nil
}

// defaultInstallFromRepo installs a package from the vendor's apt repository
// on the node. The name comes from packageSteps, never from free text.
func defaultInstallFromRepo(ctx context.Context, client *transport.Client, remoteBundleDir, name string) error {
	lib := path.Join(remoteBundleDir, "bootstrap", "lib", "packages.sh")
	inner := "source " + transport.ShellQuote(lib) + " && pkg_setup_repo " + transport.ShellQuote(name)
	command := "env EN_BUNDLE_DIR=" + transport.ShellQuote(remoteBundleDir) +
		" bash -c " + transport.ShellQuote(inner) +
		" && sudo apt-get update && sudo apt-get install -y " + transport.ShellQuote(name)
	var stderr strings.Builder
	if err := client.Run(ctx, command, &strings.Builder{}, &stderr); err != nil {
		return fmt.Errorf("installing %s from the vendor repository: %w (stderr: %s)", name, err, stderr.String())
	}
	return nil
}

// checkPackages asks the operator when a pinned package that the selected
// steps need could not be downloaded and is not installed on the node. A
// remembered answer decides without asking: "repo" installs it on the node
// (only for update=apt), "skip" leaves it to the deselected step.
func (h *Host) checkPackages(ctx context.Context, client *transport.Client, manifest *bundle.Manifest, missing []string, sel *selection.Selection) error {
	var ask []string
	repoPossible := true
	for _, name := range missing {
		step, known := packageSteps[name]
		if !known || !sel.Selected(step) {
			continue
		}
		if packageInstalled(ctx, client, name) {
			continue
		}
		apt := manifest.Packages[name].Update == "apt"
		switch h.packageChoice(name) {
		case "repo":
			if apt {
				if err := installFromRepo(ctx, client, h.cfg.RemoteBundleDir, name); err != nil {
					return &hostapi.Error{Code: "PACKAGE_REPO_FAILED", Detail: err.Error()}
				}
				continue
			}
		case "skip":
			continue
		}
		ask = append(ask, name)
		if !apt {
			repoPossible = false
		}
	}
	if len(ask) == 0 {
		return nil
	}
	names := strings.Join(ask, ", ")
	return &hostapi.Error{
		Code:   "PACKAGE_UNAVAILABLE",
		Detail: names,
		Status: http.StatusConflict,
		Args:   map[string]string{"names": names, "repo_possible": strconv.FormatBool(repoPossible)},
	}
}

// packageChoice liefert die gemerkte Antwort zu einem Paket oder "".
func (h *Host) packageChoice(name string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.packageChoices[name]
}

// rememberPackageChoices merkt sich die Antworten der Rueckfrage fuer die
// Sitzung. Unbekannte Werte sind ein Fehler, sie wuerden sonst still fragen.
func (h *Host) rememberPackageChoices(choices map[string]string) error {
	for name, choice := range choices {
		if choice != "repo" && choice != "skip" {
			return &hostapi.Error{Code: "BAD_REQUEST", Detail: "unbekannte Antwort fuer " + name + ": " + choice, Status: http.StatusBadRequest}
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for name, choice := range choices {
		if h.packageChoices == nil {
			h.packageChoices = map[string]string{}
		}
		h.packageChoices[name] = choice
	}
	return nil
}

// prepareSelection is the selection the run will use: the pending one or the
// manifest's defaults, with every package skipped in this session deselected.
func (h *Host) prepareSelection(manifest *bundle.Manifest) *selection.Selection {
	base := h.pendingSelection()
	if base == nil {
		base = selection.DefaultFor(manifest)
	}
	sel := &selection.Selection{Steps: map[string]bool{}}
	for id, on := range base.Steps {
		sel.Steps[id] = on
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for name, choice := range h.packageChoices {
		if step, known := packageSteps[name]; known && choice == "skip" {
			sel.Steps[step] = false
		}
	}
	return sel
}

// stagePackages is the seam for putting pinned files on the node. Tests
// replace it; production runs defaultStagePackages.
var stagePackages = defaultStagePackages

// defaultStagePackages makes remoteDir hold exactly the files pkgs names: a
// file whose SHA-256 on the node already matches the pin is left alone,
// a missing or differing one is uploaded, and every other file in remoteDir
// is removed. Only the top level of remoteDir is touched.
func defaultStagePackages(ctx context.Context, client *transport.Client, remoteDir string, pkgs []bundlesource.LocalPackage, progress func(done, total int64)) error {
	quoted := transport.ShellQuote(remoteDir)
	listing := fmt.Sprintf(
		"mkdir -p %[1]s && chmod 0755 %[1]s && cd %[1]s && find . -maxdepth 1 -type f -print0 | xargs -0 -r sha256sum",
		quoted)
	var stdout, stderr strings.Builder
	if err := client.Run(ctx, listing, &stdout, &stderr); err != nil {
		return fmt.Errorf("listing %s: %w (stderr: %s)", remoteDir, err, stderr.String())
	}
	present, err := parseRemoteSums(stdout.String())
	if err != nil {
		return fmt.Errorf("listing %s: %w", remoteDir, err)
	}

	want := make(map[string]string, len(pkgs))
	for _, p := range pkgs {
		want[p.File] = p.SHA256
	}

	var stale []string
	for name := range present {
		if _, ok := want[name]; !ok {
			stale = append(stale, transport.ShellQuote(name))
		}
	}
	if len(stale) > 0 {
		remove := fmt.Sprintf("cd %s && rm -f -- %s", quoted, strings.Join(stale, " "))
		if err := client.Run(ctx, remove, &strings.Builder{}, &strings.Builder{}); err != nil {
			return fmt.Errorf("removing stale files in %s: %w", remoteDir, err)
		}
	}

	var upload []bundlesource.LocalPackage
	var total int64
	for _, p := range pkgs {
		if present[p.File] == p.SHA256 {
			continue
		}
		info, err := os.Stat(p.Path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", p.Path, err)
		}
		upload = append(upload, p)
		total += info.Size()
	}

	var offset int64
	for _, p := range upload {
		if err := ctx.Err(); err != nil {
			return err
		}
		base := offset
		var onProgress func(done, total int64)
		if progress != nil {
			onProgress = func(done, _ int64) { progress(base+done, total) }
		}
		if err := client.UploadFileProgress(p.Path, path.Join(remoteDir, p.File), 0o644, onProgress); err != nil {
			return fmt.Errorf("uploading %s: %w", p.File, err)
		}
		if info, err := os.Stat(p.Path); err == nil {
			offset += info.Size()
		}
	}
	return nil
}

// parseRemoteSums reads sha256sum output ("<hash>  ./<name>" per line) into
// a map from file name to hash. Escaped lines, which start with a backslash,
// are skipped the same way bundle.HashRemoteDir skips them.
func parseRemoteSums(output string) (map[string]string, error) {
	sums := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" || strings.HasPrefix(line, "\\") {
			continue
		}
		if len(line) <= 66 || line[64:66] != "  " {
			return nil, fmt.Errorf("malformed sha256sum line: %q", line)
		}
		sums[strings.TrimPrefix(line[66:], "./")] = line[:64]
	}
	return sums, nil
}
