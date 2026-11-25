//go:build linux

package host

import (
	"context"

	"github.com/Developer-Simon/energy-node-installer/internal/media"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// mediaDiscoverURL oeffnet den Imager im App-Store (Flatpak).
const mediaDiscoverURL = "appstream://" + media.FlatpakID

// mediaPlatform meldet, dass dieser Rechner Medien vorbereiten kann.
func mediaPlatform() (supported bool, links []hostapi.MediaLink) {
	return true, []hostapi.MediaLink{
		{Kind: "discover", URL: mediaDiscoverURL},
		{Kind: "website", URL: mediaWebsiteURL},
	}
}

var mediaRunner media.Runner = execRunner

// locateImager sucht den Imager ueber Flatpak und den Systempfad.
func locateImager(ctx context.Context) (media.Imager, bool) {
	return media.FindImager(ctx, imagerEnv)
}
