package host_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/host"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// The unit log stream is found by type assertion: a wrong signature would
// not fail to compile, it would silently answer 501.
var _ hostapi.UnitLogsProvider = (*host.Host)(nil)

// unitLogsNopSink is only passed where Stream never gets far enough to use it.
type unitLogsNopSink struct{}

func (unitLogsNopSink) Hello(string)              {}
func (unitLogsNopSink) Line(hostapi.UnitLogLine)  {}
func (unitLogsNopSink) Status(hostapi.UnitStatus) {}

func TestUnitLogsFailsBeforeConnect(t *testing.T) {
	var h host.Host
	err := h.UnitLogs(context.Background(), hostapi.UnitLogsRequest{Units: hostapi.LiveUnits, Backlog: hostapi.LiveBacklog}, unitLogsNopSink{})
	var apiErr *hostapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "NOT_CONNECTED" || apiErr.Status != http.StatusConflict {
		t.Fatalf("err = %#v, want NOT_CONNECTED/409", err)
	}
}
