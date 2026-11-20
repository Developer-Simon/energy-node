package host

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
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

func TestRunApplySelectionRefusesWithoutList(t *testing.T) {
	h := &Host{client: &transport.Client{}, manifest: applyTestManifest(), resolved: &hostapi.ResolvedInfo{Kind: "github"}}
	err := h.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModeApplySelection}, &recordingSink{})
	var apiErr *hostapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "APPLY_NOT_AVAILABLE" || apiErr.Status != http.StatusConflict {
		t.Fatalf("err = %#v, want APPLY_NOT_AVAILABLE/409", err)
	}
}

func TestRunApplySelectionRunsOnlyTheListAndRecords(t *testing.T) {
	origRun, origRec, origNode := runSteps, recordInstalled, nodeSelection
	t.Cleanup(func() { runSteps, recordInstalled, nodeSelection = origRun, origRec, origNode })
	var ran []steps.RunOptions
	runSteps = func(_ context.Context, opts steps.RunOptions) error { ran = append(ran, opts); return nil }
	recorded := 0
	recordInstalled = func(context.Context, *transport.Client, string, string) error { recorded++; return nil }
	nodeSelection = func(*Host) *hostapi.SelectionView {
		return &hostapi.SelectionView{Steps: map[string]bool{"81": false}, Source: "node"}
	}

	h := &Host{
		cfg:      Config{RemoteBundleDir: "/b", RemoteStateDir: "/s"},
		client:   &transport.Client{},
		manifest: applyTestManifest(),
		resolved: &hostapi.ResolvedInfo{Kind: "node", Installed: true},
		pending:  selectionOf(map[string]bool{"81": true}),
	}
	if err := h.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModeApplySelection}, &recordingSink{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(ran) != 1 {
		t.Fatalf("steps.Run called %d times, want 1", len(ran))
	}
	var ids []string
	for _, step := range ran[0].Steps {
		ids = append(ids, step.ID)
	}
	if want := []string{"65", "81"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ran steps %v, want %v", ids, want)
	}
	if ran[0].Force {
		t.Errorf("Force = true, want false: apply-selection keeps the stamps")
	}
	if recorded != 1 {
		t.Errorf("recordInstalled called %d times, want 1", recorded)
	}
}
