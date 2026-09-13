package hostapi

import "sync"

// runState haelt den einen laufenden Lauf. Inhalt bekommt er in Task 9.
type runState struct {
	mu sync.Mutex
}
