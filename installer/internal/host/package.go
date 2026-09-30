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
	"strconv"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/bundlesource"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// Die Nahtstellen zum Node; Tests ersetzen sie, weil sie SSH brauchen.
var (
	detectMachine = defaultDetectMachine
	stageBundle   = func(ctx context.Context, c *transport.Client, archive, remoteDir string, onProgress func(done, total int64)) error {
		return bundle.DeployProgress(ctx, c, archive, remoteDir, onProgress)
	}
	verifyStaged          = defaultVerifyStaged
	readInstalledManifest = bundle.ReadInstalledManifest
	stageDelta            = func(ctx context.Context, c *transport.Client, bundleDir string, changed, removed []string, remoteDir string, onProgress func(done, total int64)) error {
		return bundle.DeployDelta(ctx, c, bundleDir, changed, removed, remoteDir, onProgress)
	}
)

// uploadProgress turns byte counts into one note per 5 % step, so the UI
// shows movement without a message for every 32 KiB packet.
func uploadProgress(notef func(string, map[string]string)) func(done, total int64) {
	lastStep := 0
	return func(done, total int64) {
		if total <= 0 {
			return
		}
		step := int(done * 20 / total) // 0..20, one per 5 %
		if step <= lastStep {
			return
		}
		lastStep = step
		notef("package.log.upload_progress", map[string]string{
			"percent": strconv.Itoa(step * 5),
			"done":    megabytes(done),
			"total":   megabytes(total),
		})
	}
}

func megabytes(n int64) string { return fmt.Sprintf("%.1f", float64(n)/(1<<20)) }

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
func (h *Host) prepare(ctx context.Context, sink hostapi.Sink, forceFull bool) error {
	client, err := h.connected()
	if err != nil {
		return err
	}
	sink.Marker("package", "begin", "")
	logf := func(line string) { sink.Log("package", line) }
	notef := func(key string, args map[string]string) { sink.Message("package", key, args) }
	if err := h.doPrepare(ctx, client, logf, notef, forceFull); err != nil {
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

func (h *Host) doPrepare(ctx context.Context, client *transport.Client, logf func(string), notef func(string, map[string]string), forceFull bool) error {
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
	notef("package.log.upload", map[string]string{})
	usedDelta, err := h.stage(ctx, client, resolved, archive, forceFull, notef)
	if err != nil {
		return &hostapi.Error{Code: "PACKAGE_STAGE_FAILED", Detail: err.Error()}
	}
	notef("package.log.verify", map[string]string{})
	if err := verifyStaged(ctx, client, h.cfg.RemoteBundleDir, resolved.Signed); err != nil {
		if usedDelta {
			return &hostapi.Error{Code: "PACKAGE_VERIFY_FAILED_DELTA", Detail: err.Error(), Status: http.StatusConflict}
		}
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

// stage transfers the resolved bundle to the node: an incremental delta
// against installed-manifest.json by default, or the existing full replace
// when forceFull was asked for or there is no installed manifest to diff
// against (a first-ever prepare). It reports whether it took the delta
// path -- doPrepare needs that to decide how to react if verifyStaged then
// fails: PACKAGE_VERIFY_FAILED_DELTA only for the delta path, never for a
// full transfer's own (unrelated) verify failure.
func (h *Host) stage(ctx context.Context, client *transport.Client, resolved *bundlesource.Resolved, archive string, forceFull bool, notef func(string, map[string]string)) (usedDelta bool, err error) {
	if !forceFull {
		if installed := readInstalledManifest(ctx, client, h.cfg.RemoteStateDir); installed != nil {
			changed, removed := bundle.DiffManifest(installed, resolved.Manifest)
			if err := stageDelta(ctx, client, resolved.Dir, changed, removed, h.cfg.RemoteBundleDir, uploadProgress(notef)); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	if err := stageBundle(ctx, client, archive, h.cfg.RemoteBundleDir, uploadProgress(notef)); err != nil {
		return false, err
	}
	return false, nil
}
