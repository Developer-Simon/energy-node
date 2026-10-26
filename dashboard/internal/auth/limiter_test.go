package auth

import (
	"testing"
	"time"
)

func TestLimiterBlocksPerKeyAndTotal(t *testing.T) {
	start := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	limiter := newFailureLimiter(2, 3, time.Minute)

	limiter.fail("a", start)
	limiter.fail("a", start)
	if limiter.allowed("a", start) {
		t.Fatal(`"a" must be blocked after 2 failures`)
	}
	if !limiter.allowed("b", start) {
		t.Fatal(`"b" must still be allowed after 2 failures on "a"`)
	}

	limiter.fail("b", start)
	if limiter.allowed("b", start) {
		t.Fatal("every key must be blocked once the total of 3 failures is reached")
	}

	later := start.Add(61 * time.Second)
	if !limiter.allowed("a", later) || !limiter.allowed("b", later) {
		t.Fatal("after the window has passed, both keys must be allowed again")
	}
}
