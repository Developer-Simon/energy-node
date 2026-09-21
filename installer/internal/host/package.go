package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/bundlesource"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// Die Nahtstellen zum Node; Tests ersetzen sie, weil sie SSH brauchen.
var (
	detectMachine = defaultDetectMachine
	stageBundle   = func(ctx context.Context, c *transport.Client, archive, remoteDir string) error {
		return bundle.Deploy(ctx, c, archive, remoteDir)
	}
	verifyStaged = defaultVerifyStaged
)

func defaultDetectMachine(ctx context.Context, c *transport.Client) (string, error) {
	var stdout, stderr strings.Builder
	if err := c.Run(ctx, "uname -m", &stdout, &stderr); err != nil {
		return "", fmt.Errorf("%w (stderr: %s)", err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func defaultVerifyStaged(ctx context.Context, c *transport.Client, remoteDir string, signed bool) error {
	if signed {
		return bundle.VerifyRemote(ctx, c, remoteDir, bundle.EmbeddedPublicKeyPEM())
	}
	return bundle.VerifyRemoteDev(ctx, c, remoteDir)
}

// packageInfo baut den Teil von Description, den die Oberflaeche fuer die
// Paketauswahl braucht.
func (h *Host) packageInfo(manifest *bundle.Manifest, resolved *hostapi.ResolvedInfo) *hostapi.PackageInfo {
	info := &hostapi.PackageInfo{Resolved: resolved, Repo: hostapi.RepoInfo{Path: h.cfg.RepoPath}}
	if manifest != nil && h.bundledPresent() {
		info.Bundled = &hostapi.BundledInfo{Version: manifest.Version, Arch: manifest.Arch}
	}
	switch {
	case runtime.GOOS != "linux":
		info.Repo.Reason = "OS_UNSUPPORTED"
	case len(bundlesource.MissingTools()) > 0:
		info.Repo.Reason = "TOOLS_MISSING"
	default:
		info.Repo.Available = true
	}
	return info
}

// bundledPresent: das Bundle neben dem Programm liegt vor, unabhaengig davon,
// was gerade geladen ist.
func (h *Host) bundledPresent() bool {
	_, err := os.Stat(filepath.Join(h.cfg.BundleDir, "manifest.json"))
	return err == nil
}

func (h *Host) SelectPackage(ctx context.Context, sel hostapi.PackageSelection) error {
	req := bundlesource.Request{Kind: bundlesource.Kind(sel.Kind)}
	switch req.Kind {
	case bundlesource.KindBundled:
		if !h.bundledPresent() {
			return &hostapi.Error{Code: "NO_PACKAGE", Status: http.StatusBadRequest}
		}
	case bundlesource.KindGitHub:
	case bundlesource.KindRepo:
		if err := bundlesource.CheckRepo(sel.Path); err != nil {
			return &hostapi.Error{Code: err.Code, Detail: err.Detail, Status: http.StatusBadRequest}
		}
		req.Path = bundlesource.ExpandHome(sel.Path)
	default:
		return &hostapi.Error{Code: "BAD_REQUEST", Detail: "unbekannte Paketquelle: " + sel.Kind, Status: http.StatusBadRequest}
	}
	h.mu.Lock()
	h.choice = req
	h.mu.Unlock()
	return nil
}

func (h *Host) UploadPackage(ctx context.Context, name string, r io.Reader) error {
	if err := os.MkdirAll(h.workDir(), 0o755); err != nil {
		return &hostapi.Error{Code: "PACKAGE_UPLOAD_FAILED", Detail: err.Error()}
	}
	out, err := os.CreateTemp(h.workDir(), "upload-*.tar.gz")
	if err != nil {
		return &hostapi.Error{Code: "PACKAGE_UPLOAD_FAILED", Detail: err.Error()}
	}
	_, copyErr := io.Copy(out, r)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(out.Name())
		return &hostapi.Error{Code: "PACKAGE_UPLOAD_FAILED", Detail: errors.Join(copyErr, closeErr).Error()}
	}
	h.mu.Lock()
	previous := h.uploaded
	h.uploaded = out.Name()
	h.choice = bundlesource.Request{Kind: bundlesource.KindFile, Path: out.Name()}
	h.mu.Unlock()
	if previous != "" {
		os.Remove(previous)
	}
	return nil
}

func (h *Host) workDir() string {
	if h.cfg.Resolver != nil && h.cfg.Resolver.WorkDir != "" {
		return h.cfg.Resolver.WorkDir
	}
	return os.TempDir()
}

// Close raeumt auf, was der Wirt selbst angelegt hat: entpackte
// Zwischenstaende und die hochgeladene Paketdatei.
func (h *Host) Close() error {
	h.mu.Lock()
	cleanup, uploaded := h.cleanup, h.uploaded
	h.cleanup, h.uploaded = nil, ""
	h.mu.Unlock()
	if cleanup != nil {
		cleanup()
	}
	if uploaded != "" {
		os.Remove(uploaded)
	}
	return nil
}

// prepare loest die gewaehlte Quelle fuer die Architektur des Node auf,
// uebertraegt das Paket und prueft es dort. Erst danach kennen Manifest,
// Vorpruefung und Vorschau ein Paket.
func (h *Host) prepare(ctx context.Context, sink hostapi.Sink) error {
	client, err := h.connected()
	if err != nil {
		return err
	}
	sink.Marker("package", "begin", "")
	logf := func(line string) { sink.Log("package", line) }
	notef := func(key string, args map[string]string) { sink.Message("package", key, args) }
	if err := h.doPrepare(ctx, client, logf, notef); err != nil {
		apiErr := packageError(err)
		var typed *hostapi.Error
		if errors.As(apiErr, &typed) {
			sink.Marker("package", "fail", typed.Code)
		} else {
			sink.Marker("package", "fail", "PACKAGE_FAILED")
		}
		return apiErr
	}
	sink.Marker("package", "ok", "")
	return nil
}

func (h *Host) doPrepare(ctx context.Context, client *transport.Client, logf func(string), notef func(string, map[string]string)) error {
	if h.cfg.Resolver == nil {
		return &hostapi.Error{Code: "NO_PACKAGE", Status: http.StatusConflict}
	}
	h.mu.Lock()
	choice := h.choice
	h.mu.Unlock()
	if choice.Kind == "" {
		if h.bundledPresent() {
			choice.Kind = bundlesource.KindBundled
		} else {
			choice.Kind = bundlesource.KindGitHub
		}
	}

	notef("package.log.detect_arch", map[string]string{})
	machine, err := detectMachine(ctx, client)
	if err != nil {
		return err
	}
	arch, ok := bundlesource.ArchForMachine(machine)
	if !ok {
		return &hostapi.Error{Code: "ARCH_UNSUPPORTED", Detail: machine}
	}
	notef("package.log.arch_detected", map[string]string{"machine": machine, "arch": arch})

	choice.Arch = arch
	choice.Log = logf
	choice.Note = notef
	resolved, err := h.cfg.Resolver.Resolve(ctx, choice)
	if err != nil {
		return err
	}

	adopted := false
	defer func() {
		if !adopted {
			resolved.Cleanup()
		}
	}()

	archive := resolved.ArchivePath
	if archive == "" {
		if err := os.MkdirAll(h.workDir(), 0o755); err != nil {
			return err
		}
		packed, err := os.CreateTemp(h.workDir(), "bundled-*.tar.gz")
		if err != nil {
			return err
		}
		packed.Close()
		defer os.Remove(packed.Name())
		if err := bundle.PackDir(resolved.Dir, packed.Name()); err != nil {
			return err
		}
		archive = packed.Name()
	}

	if err := provisionRemoteStateDir(ctx, client, h.cfg.RemoteStateDir); err != nil {
		return &hostapi.Error{Code: "PACKAGE_STAGE_FAILED", Detail: err.Error()}
	}
	logf("Paket auf das Geraet uebertragen")
	if err := stageBundle(ctx, client, archive, h.cfg.RemoteBundleDir); err != nil {
		return &hostapi.Error{Code: "PACKAGE_STAGE_FAILED", Detail: err.Error()}
	}
	logf("Paket auf dem Geraet pruefen")
	if err := verifyStaged(ctx, client, h.cfg.RemoteBundleDir, resolved.Signed); err != nil {
		return err
	}

	h.adopt(resolved, choice.Kind)
	adopted = true
	return nil
}

// adopt macht das aufgeloeste Paket zum aktuellen und raeumt das vorige weg.
func (h *Host) adopt(resolved *bundlesource.Resolved, kind bundlesource.Kind) {
	h.mu.Lock()
	previous := h.cleanup
	h.manifest = resolved.Manifest
	h.bundleDir = resolved.Dir
	h.cleanup = resolved.Cleanup
	h.resolved = &hostapi.ResolvedInfo{Kind: string(kind), Signed: resolved.Signed}
	h.pending = nil // die Auswahl gehoert zum alten Manifest
	h.mu.Unlock()
	if previous != nil {
		previous()
	}
}

// packageError uebersetzt Fehler der Paketschicht in Fehler mit Code.
func packageError(err error) error {
	var (
		apiErr    *hostapi.Error
		sourceErr *bundlesource.Error
		bundleErr *bundle.Error
	)
	switch {
	case errors.As(err, &apiErr):
		return apiErr
	case errors.As(err, &sourceErr):
		return &hostapi.Error{Code: sourceErr.Code, Detail: sourceErr.Detail}
	case errors.As(err, &bundleErr):
		return &hostapi.Error{Code: string(bundleErr.Code), Detail: bundleErr.Message}
	case errors.Is(err, context.Canceled):
		return err
	default:
		return &hostapi.Error{Code: "PACKAGE_FAILED", Detail: err.Error()}
	}
}
