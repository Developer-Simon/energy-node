package autolearn

import (
	"context"
	"sync"
	"testing"
	"time"
)

// sleepTask waits for d or until the context ends, whichever comes first.
func sleepTask(name string, d time.Duration) Task {
	return Task{Detector: name, Run: func(ctx context.Context) []Candidate {
		select {
		case <-time.After(d):
			return []Candidate{{Detector: name}}
		case <-ctx.Done():
			return nil
		}
	}}
}

// blockTask waits until the context ends and never finishes on its own.
func blockTask(name string) Task {
	return Task{Detector: name, Run: func(ctx context.Context) []Candidate {
		<-ctx.Done()
		return nil
	}}
}

func TestPoolBudgetLeavesUnfinishedTasks(t *testing.T) {
	quick := Task{Detector: "quick", Run: func(ctx context.Context) []Candidate {
		return []Candidate{{Detector: "quick"}}
	}}
	tasks := []Task{quick, blockTask("slow"), blockTask("late")}

	done, unfinished, took := RunPool(context.Background(), 1, 100*time.Millisecond, tasks)

	if len(done) != 1 || done[0].Detector != "quick" {
		t.Fatalf("done = %+v, want exactly the quick task", done)
	}
	if len(unfinished) != 2 {
		t.Fatalf("unfinished = %d, want 2", len(unfinished))
	}
	if unfinished[0].Detector != "slow" || unfinished[1].Detector != "late" {
		t.Fatalf("unfinished order = %s, %s, want slow, late", unfinished[0].Detector, unfinished[1].Detector)
	}
	if _, ok := took["quick"]; !ok {
		t.Fatalf("took has no entry for quick: %v", took)
	}
}

func TestPoolNextRoundStartsWithUnfinished(t *testing.T) {
	var mu sync.Mutex
	var order []string
	record := func(name string, d time.Duration) Task {
		return Task{Detector: name, Run: func(ctx context.Context) []Candidate {
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return nil
			}
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return []Candidate{{Detector: name}}
		}}
	}
	// a needs longer than the first budget, b is quick but waits behind it.
	_, unfinished, _ := RunPool(context.Background(), 1, 50*time.Millisecond,
		[]Task{record("a", 300*time.Millisecond), record("b", 0)})
	if len(unfinished) != 2 {
		t.Fatalf("first round unfinished = %d, want 2", len(unfinished))
	}

	// The next round puts the unfinished tasks in front of the new ones.
	next := append(unfinished, record("c", 0))
	done, rest, _ := RunPool(context.Background(), 1, 5*time.Second, next)
	if len(rest) != 0 {
		t.Fatalf("second round left %d tasks", len(rest))
	}
	if len(done) != 3 {
		t.Fatalf("second round done = %d, want 3", len(done))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Fatalf("run order = %v, want a, b, c", order)
	}
}

func TestPoolFourWorkersAreFasterThanOne(t *testing.T) {
	const n = 8
	const each = 60 * time.Millisecond
	tasks := func() []Task {
		out := make([]Task, n)
		for i := range out {
			out[i] = sleepTask("sleep", each)
		}
		return out
	}

	start := time.Now()
	RunPool(context.Background(), 1, 10*time.Second, tasks())
	one := time.Since(start)

	start = time.Now()
	done, unfinished, _ := RunPool(context.Background(), 4, 10*time.Second, tasks())
	four := time.Since(start)

	if len(done) != n || len(unfinished) != 0 {
		t.Fatalf("four workers: done %d, unfinished %d, want %d and 0", len(done), len(unfinished), n)
	}
	// Serial needs about 480 ms, four workers about 120 ms. Require only a
	// clear gain so the test stays tolerant on loaded machines.
	if four >= one*3/4 {
		t.Fatalf("four workers took %v, one worker %v: no clear speed-up", four, one)
	}
}

func TestPoolUnfinishedKeepsInputOrderWithWorkers(t *testing.T) {
	// Several workers report the cut-off tasks in whatever order they wake
	// up, so repeat the run and require the input order every time.
	for run := 0; run < 20; run++ {
		tasks := []Task{blockTask("t0"), blockTask("t1"), blockTask("t2"), blockTask("t3")}
		done, unfinished, _ := RunPool(context.Background(), 2, 20*time.Millisecond, tasks)
		if len(done) != 0 {
			t.Fatalf("run %d: done = %d, want 0", run, len(done))
		}
		if len(unfinished) != 4 {
			t.Fatalf("run %d: unfinished = %d, want 4", run, len(unfinished))
		}
		for i, u := range unfinished {
			if want := tasks[i].Detector; u.Detector != want {
				t.Fatalf("run %d: unfinished[%d] = %s, want %s", run, i, u.Detector, want)
			}
		}
	}
}

func TestRunPoolRecoversPanic(t *testing.T) {
	ok := func(name string) Task {
		return Task{Detector: name, Run: func(ctx context.Context) []Candidate {
			return []Candidate{{Detector: name}}
		}}
	}
	boom := Task{Detector: "boom", Device: "dev", Run: func(ctx context.Context) []Candidate {
		panic("detector bug")
	}}

	done, unfinished, took := RunPool(context.Background(), 2, 5*time.Second, []Task{ok("a"), boom, ok("c")})

	if len(done) != 2 || done[0].Detector != "a" || done[1].Detector != "c" {
		t.Fatalf("done = %+v, want the candidates of a and c", done)
	}
	if len(unfinished) != 0 {
		t.Fatalf("unfinished = %d, want 0: a panicking task counts as finished", len(unfinished))
	}
	if _, ok := took["boom"]; !ok {
		t.Fatalf("took has no entry for the panicking task: %v", took)
	}
}
