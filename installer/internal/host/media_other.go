//go:build !linux && !windows

package host

import (
	"context"

	"github.com/Developer-Simon/energy-node-installer/internal/media"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// mediaPlatform meldet, dass dieser Rechner keine Medien vorbereitet.
func mediaPlatform() (supported bool, links []hostapi.MediaLink) {
	return false, []hostapi.MediaLink{}
}

var mediaRunner media.Runner = execRunner

// locateImager findet auf diesem System keinen Imager.
func locateImager(ctx context.Context) (media.Imager, bool) {
	return media.Imager{}, false
}
