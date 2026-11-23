package autolearn

import (
	"context"
	"log"
	"sort"
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
		left  []indexedTask
	)
	take := func() (int, Task, bool) {
		mu.Lock()
		defer mu.Unlock()
		if next >= len(tasks) {
			return 0, Task{}, false
		}
		i := next
		next++
		return i, tasks[i], true
	}

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i, t, ok := take()
				if !ok {
					return
				}
				if ctx.Err() != nil {
					mu.Lock()
					left = append(left, indexedTask{i, t})
					mu.Unlock()
					continue
				}
				start := time.Now()
				// The result channel is buffered, so a task that outlives the
				// budget does not block on the send after the pool has moved on.
				ch := make(chan []Candidate, 1)
				go func() {
					// A panicking task counts as finished without candidates, so
					// one detector bug does not stop the daemon.
					var out []Candidate
					defer func() {
						if r := recover(); r != nil {
							log.Printf("autolearn: detector %s/%s panicked: %v", t.Detector, t.Device, r)
						}
						ch <- out
					}()
					out = t.Run(ctx)
				}()
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
					left = append(left, indexedTask{i, t})
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	// Workers report in completion order, so restore the order of the input.
	sort.Slice(left, func(a, b int) bool { return left[a].i < left[b].i })
	for _, it := range left {
		unfinished = append(unfinished, it.t)
	}
	return done, unfinished, total
}

// indexedTask keeps the input position of a task that did not finish.
type indexedTask struct {
	i int
	t Task
}
