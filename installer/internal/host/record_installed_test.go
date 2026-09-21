package host

import (
	"context"
	"errors"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

func TestRecordAfterFullRunRecordsOnlyForAFullRun(t *testing.T) {
	orig := recordInstalled
	t.Cleanup(func() { recordInstalled = orig })
	calls := 0
	recordInstalled = func(context.Context, *transport.Client, string, string) error { calls++; return nil }

	h := &Host{cfg: Config{RemoteBundleDir: "/b", RemoteStateDir: "/s"}}
	h.recordAfterFullRun(context.Background(), nil, "", &recordingSink{})
	if calls != 1 {
		t.Fatalf("full run: recordInstalled called %d times, want 1", calls)
	}
	h.recordAfterFullRun(context.Background(), nil, "81", &recordingSink{})
	if calls != 1 {
		t.Fatalf("single-step run must not record, calls = %d", calls)
	}
}

func TestRecordAfterFullRunTurnsAFailureIntoAWarning(t *testing.T) {
	orig := recordInstalled
	t.Cleanup(func() { recordInstalled = orig })
	recordInstalled = func(context.Context, *transport.Client, string, string) error { return errors.New("disk full") }

	sink := &recordingSink{}
	h := &Host{cfg: Config{RemoteBundleDir: "/b", RemoteStateDir: "/s"}}
	h.recordAfterFullRun(context.Background(), nil, "", sink)
	if len(sink.logs) != 1 {
		t.Fatalf("want exactly one warning line, got %v", sink.logs)
	}
}
