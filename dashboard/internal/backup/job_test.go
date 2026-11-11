package backup

import (
	"context"
	"errors"
	"testing"
	"time"
)

func waitIdle(t *testing.T, j *Jobs) JobStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := j.Status()
		if !st.Running {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatal("job still running after deadline")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestJobsRejectSecondStart(t *testing.T) {
	var j Jobs
	release := make(chan struct{})
	started := make(chan struct{})
	_, err := j.Start("create", []string{"a", "b"}, func(ctx context.Context, phase func(int)) (string, error) {
		phase(1)
		close(started)
		<-release
		return "fertig", nil
	})
	if err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if _, err := j.Start("create", []string{"a"}, func(context.Context, func(int)) (string, error) {
		return "", nil
	}); !errors.Is(err, ErrBusy) {
		t.Fatalf("second Start err = %v, want ErrBusy", err)
	}

	<-started
	st := j.Status()
	if !st.Running || st.Kind != "create" || st.Current != 1 {
		t.Fatalf("running status = %+v", st)
	}
	if !st.Phases[0].Done || st.Phases[1].Done {
		t.Fatalf("phases = %+v, want first done only", st.Phases)
	}

	close(release)
	st = waitIdle(t, &j)
	if st.Result != "fertig" || st.Error != "" {
		t.Fatalf("final status = %+v", st)
	}

	if _, err := j.Start("create", []string{"a"}, func(context.Context, func(int)) (string, error) {
		return "", errors.New("kaputt")
	}); err != nil {
		t.Fatalf("Start after finish: %v", err)
	}
	st = waitIdle(t, &j)
	if st.Error != "kaputt" {
		t.Fatalf("error status = %+v", st)
	}
}

func TestJobsZeroStatus(t *testing.T) {
	var j Jobs
	if st := j.Status(); st.Running || st.ID != "" || len(st.Phases) != 0 {
		t.Fatalf("zero status = %+v", st)
	}
}
