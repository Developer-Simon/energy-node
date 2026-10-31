package host

import (
	"context"
	"time"

	"github.com/Developer-Simon/energy-node-installer/internal/unitlogs"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// unitLogsInterval ist der Abstand, in dem der Status der Units geprueft wird.
const unitLogsInterval = 2 * time.Second

// UnitLogs streamt Journal und Status der Live-Units ueber SSH.
func (h *Host) UnitLogs(ctx context.Context, req hostapi.UnitLogsRequest, sink hostapi.UnitLogSink) error {
	client, err := h.connected()
	if err != nil {
		return err
	}
	h.mu.Lock()
	redactor := hostapi.NewRedactor(h.sudoPassword)
	h.mu.Unlock()
	return unitlogs.Stream(ctx, client, req, redactor, sink, unitLogsInterval)
}
