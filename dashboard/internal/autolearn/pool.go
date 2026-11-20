package autolearn

import (
	"context"
	"sync"
	"time"
)

// Task is one detector run on one device (empty for site-wide detectors).
// Run must return when its context ends.
type Task struct {
	Detector string
	Device   string
	Run      func(ctx context.Context) []Candidate
}

// RunPool runs the tasks on the given number of workers within the time
// budget. When the budget is used up, the running tasks are cancelled through
// their context and, together with the tasks that never started, are returned
// as unfinished in their original order. took holds the time spent per
// detector. A task that returns after the budget is unfinished too, its
// candidates are dropped.
func RunPool(ctx context.Context, workers int, budget time.Duration, tasks []Task) (done []Candidate, unfinished []Task, took map[string]time.Duration) {
	if workers < 1 {
		workers = 1
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	var (
		mu    sync.Mutex
		next  int
		wg    sync.WaitGroup
		total = map[string]time.Duration{}
	)
	take := func() (Task, bool) {
		mu.Lock()
		defer mu.Unlock()
		if next >= len(tasks) {
			return Task{}, false
		}
		t := tasks[next]
		next++
		return t, true
	}
	giveUp := func(t Task) {
		mu.Lock()
		unfinished = append(unfinished, t)
		mu.Unlock()
	}

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				t, ok := take()
				if !ok {
					return
				}
				if ctx.Err() != nil {
					giveUp(t)
					continue
				}
				start := time.Now()
				// The result channel is buffered, so a task that outlives the
				// budget does not block on the send after the pool has moved on.
				ch := make(chan []Candidate, 1)
				go func() { ch <- t.Run(ctx) }()
				var out []Candidate
				finished := false
				select {
				case out = <-ch:
					finished = ctx.Err() == nil
				case <-ctx.Done():
				}
				elapsed := time.Since(start)

				mu.Lock()
				total[t.Detector] += elapsed
				if finished {
					done = append(done, out...)
				} else {
					unfinished = append(unfinished, t)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return done, unfinished, total
}
