package auth

import "time"

// failureLimiter zählt Fehlversuche je Schlüssel und insgesamt in einem
// gleitenden Fenster. Ist eine Grenze erreicht, sind weitere Versuche bis
// zum Ablauf der ältesten Fehlversuche gesperrt. perKey 0 heißt ohne Grenze
// je Schlüssel.
type failureLimiter struct {
	perKey int
	total  int
	window time.Duration
	byKey  map[string][]time.Time
	all    []time.Time
}

func newFailureLimiter(perKey, total int, window time.Duration) *failureLimiter {
	return &failureLimiter{perKey: perKey, total: total, window: window, byKey: map[string][]time.Time{}}
}

// allowed meldet, ob ein Versuch mit diesem Schlüssel jetzt erlaubt ist.
func (l *failureLimiter) allowed(key string, now time.Time) bool {
	l.prune(now)
	if l.total > 0 && len(l.all) >= l.total {
		return false
	}
	if l.perKey > 0 && len(l.byKey[key]) >= l.perKey {
		return false
	}
	return true
}

// fail verbucht einen Fehlversuch mit diesem Schlüssel.
func (l *failureLimiter) fail(key string, now time.Time) {
	l.prune(now)
	l.byKey[key] = append(l.byKey[key], now)
	l.all = append(l.all, now)
}

// prune wirft Fehlversuche aus dem Fenster heraus.
func (l *failureLimiter) prune(now time.Time) {
	for key, times := range l.byKey {
		kept := keepRecent(times, now, l.window)
		if len(kept) == 0 {
			delete(l.byKey, key)
		} else {
			l.byKey[key] = kept
		}
	}
	l.all = keepRecent(l.all, now, l.window)
}

func keepRecent(times []time.Time, now time.Time, window time.Duration) []time.Time {
	kept := times[:0]
	for _, t := range times {
		if now.Sub(t) < window {
			kept = append(kept, t)
		}
	}
	return kept
}
