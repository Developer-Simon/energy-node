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

// Fetch finds, downloads, unpacks, validates and installs the bundle, logging
// one human-readable line per stage. If DestDir already holds the newest
// version it does nothing else. Any failure leaves DestDir as it was.
func (f *Fetcher) Fetch(ctx context.Context, log func(line string)) error {
	if log == nil {
		log = func(string) {}
	}
	log("Suche das neueste Release fuer " + f.Arch)
	asset, err := f.Client.FindAsset(ctx, f.Arch)
	if err != nil {
		return err
	}
	log("Neueste Version: " + asset.Version)
	if candidateVersion(f.DestDir, f.Arch) == asset.Version {
		log("Version " + asset.Version + " liegt bereits bereit")
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
	log("Lade " + asset.Name)
	if err := f.Client.Download(ctx, asset, archive, log); err != nil {
		return err
	}

	log("Entpacke das Paket")
	staged := filepath.Join(work, "bundle")
	if err := extractArchive(archive, staged); err != nil {
		return err
	}
	if _, err := validate(staged, f.Arch); err != nil {
		return err
	}

	log("Lege das Paket bereit")
	if err := swapIn(staged, f.DestDir); err != nil {
		return err
	}
	log("Paket " + asset.Version + " liegt bereit")
	return nil
}
