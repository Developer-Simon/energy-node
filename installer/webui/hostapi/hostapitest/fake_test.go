package hostapitest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
	"github.com/Developer-Simon/energy-node-webui/hostapi/hostapitest"
)

// Der Compiler prueft hier, was kein Test pruefen kann: dass die Attrappe das
// vollstaendige Interface erfuellt. Aendert sich Backend, faellt das hier auf
// und nicht erst im Wirt.
var _ hostapi.Backend = (*hostapitest.FakeBackend)(nil)

type recordingSink struct {
	markers []string
	logs    []string
	notes   []string
}

func (r *recordingSink) Marker(stepID, state, detail string) {
	r.markers = append(r.markers, stepID+" "+state+" "+detail)
}

func (r *recordingSink) Log(stepID, line string) {
	r.logs = append(r.logs, stepID+": "+line)
}

func (r *recordingSink) Message(stepID, key string, args map[string]string) {
	r.notes = append(r.notes, stepID+": "+key)
}

func TestRunPlaysTheScriptInOrder(t *testing.T) {
	fake := hostapitest.NewFake()
	fake.Script(
		hostapitest.FakeStep{ID: "10", State: "ok", Log: []string{"one", "two"}},
		hostapitest.FakeStep{ID: "20", State: "skip", Detail: "nicht ausgewaehlt"},
	)
	sink := &recordingSink{}
	if err := fake.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModeInstall}, sink); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"10 begin ", "10 ok ", "20 begin ", "20 skip nicht ausgewaehlt"}
	if len(sink.markers) != len(want) {
		t.Fatalf("markers = %v, want %v", sink.markers, want)
	}
	for i := range want {
		if sink.markers[i] != want[i] {
			t.Errorf("marker %d = %q, want %q", i, sink.markers[i], want[i])
		}
	}
	if len(sink.logs) != 2 {
		t.Errorf("logs = %v, want two lines", sink.logs)
	}
}

func TestRunReturnsATypedErrorOnAFailingStep(t *testing.T) {
	fake := hostapitest.NewFake()
	fake.Script(hostapitest.FakeStep{ID: "50", State: "fail", Detail: "PIP_EXTERNALLY_MANAGED"})
	err := fake.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModeInstall}, &recordingSink{})
	var typed *hostapi.Error
	if err == nil {
		t.Fatalf("Run returned no error for a failing step")
	}
	if !asHostapiError(err, &typed) || typed.Code != "PIP_EXTERNALLY_MANAGED" {
		t.Fatalf("err = %v, want a *hostapi.Error carrying the fault code", err)
	}
}

func asHostapiError(err error, target **hostapi.Error) bool {
	typed, ok := err.(*hostapi.Error)
	if ok {
		*target = typed
	}
	return ok
}

func TestRunStopsOnACancelledContext(t *testing.T) {
	fake := hostapitest.NewFake()
	fake.StepDelay = 10 * 1000 * 1000 // 10ms
	fake.Script(hostapitest.FakeStep{ID: "10"}, hostapitest.FakeStep{ID: "20"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fake.Run(ctx, hostapi.RunRequest{Mode: hostapi.ModeInstall}, &recordingSink{}); err == nil {
		t.Fatalf("Run ignored a cancelled context")
	}
}

// jobStepIDs schreibt einen Auftrag auf das Geraet und liest den ersten
// Zustand des Verlaufs, der die Schrittfolge zeigt.
func jobStepIDs(t *testing.T, device string) []string {
	t.Helper()
	fake := hostapitest.NewFake()
	if err := fake.MediaWrite(context.Background(), hostapi.MediaWriteRequest{Device: device, Drive: "/dev/sdb"}); err != nil {
		t.Fatalf("MediaWrite(%s): %v", device, err)
	}
	job, err := fake.MediaJob(context.Background())
	if err != nil {
		t.Fatalf("MediaJob(%s): %v", device, err)
	}
	ids := make([]string, 0, len(job.Steps))
	for _, s := range job.Steps {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestFakeDebianJobHasPatchAndNoCustomize(t *testing.T) {
	got := jobStepIDs(t, "deb-amd64")
	want := []string{"download", "checksum", "patch", "rights", "write", "verify", "eject"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("debian steps = %v, want %v", got, want)
	}
	pi := jobStepIDs(t, "pi4")
	for _, id := range pi {
		if id == "patch" {
			t.Errorf("pi steps = %v, must not have patch", pi)
		}
	}
	if pi[5] != "customize" {
		t.Errorf("pi steps = %v, want customize at index 5", pi)
	}
}
