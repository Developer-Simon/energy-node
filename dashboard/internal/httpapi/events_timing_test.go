package httpapi

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/diagnostics"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func TestEventsReportPushPerWrite(t *testing.T) {
	reg := testRegistryWithPower(t)
	store := settings.NewStore(t.TempDir())
	if err := store.SaveSettings(settings.Default()); err != nil {
		t.Fatal(err)
	}
	resolver := energy.NewResolver(nil)
	engine := diagnostics.NewEngine(reg, nil)

	var pushes atomic.Int64
	streams := &EventStreams{Push: func(time.Duration) { pushes.Add(1) }}
	handler := handleEventsWithStreams(reg, store, resolver, engine, nil, streams)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		request := httptest.NewRequest("GET", "/api/v1/events", nil).WithContext(ctx)
		handler(httptest.NewRecorder(), request)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for pushes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	if pushes.Load() < 1 {
		t.Fatal("Push wurde beim ersten Rumpf nicht aufgerufen")
	}
}
