//go:build windows

package host

import (
	"context"

	"github.com/Developer-Simon/energy-node-installer/internal/media"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// mediaWingetCommand installiert den Imager ueber winget.
const mediaWingetCommand = "winget install -e --id RaspberryPiFoundation.RaspberryPiImager"

// mediaPlatform meldet, dass dieser Rechner Medien vorbereiten kann.
func mediaPlatform() (supported bool, links []hostapi.MediaLink) {
	return true, []hostapi.MediaLink{
		{Kind: "website", URL: mediaWebsiteURL},
		{Kind: "winget", Command: mediaWingetCommand},
	}
}

var mediaRunner media.Runner = elevatedRunner

// locateImager findet unter Windows noch keinen Imager.
func locateImager(ctx context.Context) (media.Imager, bool) {
	return media.Imager{}, false
}
