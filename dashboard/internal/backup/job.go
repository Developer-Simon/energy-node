package backup

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrBusy meldet, dass bereits ein Backup-Auftrag laeuft.
var ErrBusy = errors.New("backup: job already running")

// Phase ist ein Schritt eines Auftrags.
type Phase struct {
	Key  string `json:"key"`
	Done bool   `json:"done"`
}

// JobStatus beschreibt den zuletzt gestarteten Auftrag.
type JobStatus struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind"`
	Phases  []Phase `json:"phases"`
	Current int     `json:"current"`
	Error   string  `json:"error"`
	Result  string  `json:"result"`
	Running bool    `json:"running"`
}

// Jobs fuehrt hoechstens einen Auftrag gleichzeitig aus.
type Jobs struct {
	mu     sync.Mutex
	seq    int
	status JobStatus
}

// Start laeuft fn in einer Goroutine. phase(i) markiert die Schritte vor i als
// erledigt und setzt i als aktuellen Schritt. Laeuft schon ein Auftrag, liefert
// Start ErrBusy. Nach dem Ende bleibt der Status lesbar, bis der naechste Start kommt.
func (j *Jobs) Start(kind string, phases []string, fn func(ctx context.Context, phase func(int)) (string, error)) (string, error) {
	j.mu.Lock()
	if j.status.Running {
		j.mu.Unlock()
		return "", ErrBusy
	}
	j.seq++
	id := fmt.Sprintf("job-%d", j.seq)
	ps := make([]Phase, len(phases))
	for i, key := range phases {
		ps[i] = Phase{Key: key}
	}
	j.status = JobStatus{ID: id, Kind: kind, Phases: ps, Running: true}
	j.mu.Unlock()

	phase := func(i int) {
		j.mu.Lock()
		defer j.mu.Unlock()
		for k := range j.status.Phases {
			if k < i {
				j.status.Phases[k].Done = true
			}
		}
		j.status.Current = i
	}

	go func() {
		result, err := fn(context.Background(), phase)
		j.mu.Lock()
		defer j.mu.Unlock()
		j.status.Running = false
		j.status.Result = result
		if err != nil {
			j.status.Error = err.Error()
		}
	}()
	return id, nil
}

// Status liefert eine Kopie des aktuellen Status. Ohne Auftrag den Nullwert.
func (j *Jobs) Status() JobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	st := j.status
	st.Phases = append([]Phase(nil), j.status.Phases...)
	return st
}
