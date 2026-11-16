package performance

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func TestTimingSumsPerPart(t *testing.T) {
	tm := NewTiming()
	tm.Add(PartMQTT, 2*time.Millisecond)
	tm.Add(PartMQTT, 2*time.Millisecond)
	tm.Add(PartPush, 5*time.Millisecond)
	tm.Add("unknown", time.Second)

	totals := tm.Totals()
	if totals[PartMQTT] != 4*time.Millisecond {
		t.Errorf("mqtt = %v, want 4ms", totals[PartMQTT])
	}
	if totals[PartPush] != 5*time.Millisecond {
		t.Errorf("push = %v, want 5ms", totals[PartPush])
	}
	for _, p := range []Part{PartHTTP, PartNode, PartBackground} {
		if totals[p] != 0 {
			t.Errorf("%s = %v, want 0", p, totals[p])
		}
	}
}

func TestTimingNilIsNoop(t *testing.T) {
	var tm *Timing
	tm.Add(PartHTTP, time.Second)
	tm.Since(PartHTTP, time.Now())
	_ = tm.Totals()
}

func TestMiddlewareSkipsEventStream(t *testing.T) {
	tm := NewTiming()
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Millisecond)
	})
	h := tm.Middleware(slow)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/energy/api/v1/events", nil))
	if got := tm.Totals()[PartHTTP]; got != 0 {
		t.Errorf("event stream counted: %v", got)
	}

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/system/config", nil))
	if got := tm.Totals()[PartHTTP]; got <= 0 {
		t.Errorf("http part = %v, want > 0", got)
	}
}

func TestReadRuntimeCPU(t *testing.T) {
	gc1, user1 := ReadRuntimeCPU()
	if gc1 < 0 || user1 < 0 {
		t.Fatalf("negative cpu: gc=%v user=%v", gc1, user1)
	}
	runtime.GC()
	gc2, user2 := ReadRuntimeCPU()
	if gc2 < gc1 {
		t.Errorf("gc cpu decreased: %v -> %v", gc1, gc2)
	}
	if user2 < 0 {
		t.Errorf("user cpu negative: %v", user2)
	}
}
