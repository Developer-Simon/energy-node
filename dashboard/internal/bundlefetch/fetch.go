package bundlefetch

import (
	"context"
	"os"
	"path/filepath"
)

// Fetcher brings the newest release bundle for Arch into DestDir.
type Fetcher struct {
	Client  *Client
	Arch    string
	DestDir string
}

// Note reports one stage of a fetch as a catalog key of the installer web
// UI (package.log.*) with its placeholder values. The redeploy screen
// translates it, so nothing here is user-facing text.
type Note func(key string, args map[string]string)

// Fetch finds, downloads, unpacks, validates and installs the bundle,
// noting each stage. If DestDir already holds the newest version it does
// nothing else. Any failure leaves DestDir as it was.
func (f *Fetcher) Fetch(ctx context.Context, note Note) error {
	if note == nil {
		note = func(string, map[string]string) {}
	}
	note("package.log.github_search", map[string]string{"arch": f.Arch})
	asset, err := f.Client.FindAsset(ctx, f.Arch)
	if err != nil {
		return err
	}
	note("package.log.latest", map[string]string{"version": asset.Version})
	if candidateVersion(f.DestDir, f.Arch) == asset.Version {
		note("package.log.already_ready", map[string]string{"version": asset.Version})
		return nil
	}

	// The scratch directory sits next to DestDir so the final rename never
	// crosses a filesystem boundary.
	work := f.DestDir + ".work"
	if err := os.RemoveAll(work); err != nil {
		return installFailed(err)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return installFailed(err)
	}
	defer os.RemoveAll(work)

	archive := filepath.Join(work, asset.Name)
	note("package.log.download", map[string]string{"name": asset.Name})
	if err := f.Client.Download(ctx, asset, archive, note); err != nil {
		return err
	}

	note("package.log.extract", nil)
	staged := filepath.Join(work, "bundle")
	if err := extractArchive(archive, staged); err != nil {
		return err
	}
	if _, err := validate(staged, f.Arch); err != nil {
		return err
	}

	note("package.log.stage", nil)
	if err := swapIn(staged, f.DestDir); err != nil {
		return err
	}
	note("package.log.ready", map[string]string{"version": asset.Version})
	return nil
}
