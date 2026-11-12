package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

// defaultCheckTimeout bounds one marker read. A drive that hangs must not
// hold up the tick.
const defaultCheckTimeout = 2 * time.Second

// reasonDriveMissing is the Status.Reason while the drive is not usable.
const reasonDriveMissing = "Laufwerk nicht verfügbar, Ausweichspeicher aktiv"

// Watcher follows the second drive and moves the runtime path between the
// drive and the fallback. Tick runs at start (before the recorders) and then
// every 30 seconds.
//
// The fields after Paths are hooks. A nil hook uses the real function, the
// tests replace them. Reload is wired to energystats.Store.Reload by the
// caller.
type Watcher struct {
	Config Config
	Paths  Paths
	// NodeID is the machine id the drive marker must hold.
	NodeID string
	// Now is the clock for Status.Since. Nil means time.Now.
	Now func() time.Time
	// Check reports whether mount carries the marker of nodeID. Nil means VolumeOK.
	Check func(mount, nodeID string) bool
	// CheckTimeout bounds Check. Zero means defaultCheckTimeout.
	CheckTimeout time.Duration
	// Switch moves the runtime path. Nil means the package Switch.
	Switch func(p Paths, target Mode) error
	// Merge brings fallbackDir onto driveDir. Nil means the package Merge.
	Merge func(fallbackDir, driveDir string) error
	// Reload writes open changes and drops the cache of the energy store.
	Reload func() error

	mu     sync.Mutex
	status Status
}

// Status returns the current mode, when it was entered and why.
func (w *Watcher) Status() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}

// Tick checks the drive once and makes the runtime path match. The returned
// status is the one after the tick. The finding storage_drive_missing is up
// while the mode is a fallback.
func (w *Watcher) Tick(ctx context.Context) Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.Config.UseData {
		return w.status
	}
	w.status.Reason = ""
	if w.driveReady(ctx) {
		w.toDrive()
	} else {
		w.toFallback()
	}
	return w.status
}

// toDrive moves the runtime data onto the drive. The order matters: open
// changes go to the fallback first, then the fallback is merged into the
// drive, then the path switches, then the cache is dropped so reads come
// from the drive. Only a merged fallback is cleared.
func (w *Watcher) toDrive() {
	if w.status.Mode == ModeDrive {
		return
	}
	if err := w.reload(); err != nil {
		w.fail("Zwischenspeichern vor dem Umschalten", err)
		return
	}
	fallbackDir := w.fallbackDir()
	mergeErr := w.merge(fallbackDir, DriveRuntimeDir(w.Paths))
	var skipped *energystats.MergeSkipped
	if mergeErr != nil && !errors.As(mergeErr, &skipped) {
		w.fail("Zusammenführen", mergeErr)
		return
	}
	if err := w.switchTo(ModeDrive); err != nil {
		w.fail("Umschalten auf das Laufwerk", err)
		return
	}
	w.enter(ModeDrive)
	if err := w.reload(); err != nil {
		w.fail("Neu laden vom Laufwerk", err)
	}
	if mergeErr != nil {
		// Unreadable months stay in the fallback folder, so nothing is lost.
		w.fail("Zusammenführen", mergeErr)
		return
	}
	if err := clearDir(fallbackDir); err != nil {
		w.fail("Ausweichordner leeren", err)
	}
}

// toFallback moves the runtime data to the configured fallback. A change
// from the drive drops the cache after the switch, so the next reads come
// from the fallback.
func (w *Watcher) toFallback() {
	w.status.Reason = reasonDriveMissing
	target := w.fallbackMode()
	if w.status.Mode == target {
		return
	}
	wasDrive := w.status.Mode == ModeDrive
	if err := w.switchTo(target); err != nil {
		w.fail("Umschalten auf den Ausweichspeicher", err)
		return
	}
	w.enter(target)
	if wasDrive {
		if err := w.reload(); err != nil {
			w.fail("Neu laden vom Ausweichspeicher", err)
		}
	}
}

// driveReady reads the marker with a time limit. A read that does not
// finish in time counts as a missing drive.
func (w *Watcher) driveReady(ctx context.Context) bool {
	check := w.Check
	if check == nil {
		check = VolumeOK
	}
	timeout := w.CheckTimeout
	if timeout <= 0 {
		timeout = defaultCheckTimeout
	}
	done := make(chan bool, 1)
	go func() { done <- check(w.Paths.Mount, w.NodeID) }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case ok := <-done:
		return ok
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}

func (w *Watcher) fallbackMode() Mode {
	if w.Config.Fallback == FallbackRAM {
		return ModeFallbackRAM
	}
	return ModeFallbackSystem
}

func (w *Watcher) fallbackDir() string {
	if w.fallbackMode() == ModeFallbackRAM {
		return w.Paths.RAMFallback
	}
	return w.Paths.SystemFallback
}

func (w *Watcher) switchTo(target Mode) error {
	if w.Switch != nil {
		return w.Switch(w.Paths, target)
	}
	return Switch(w.Paths, target)
}

func (w *Watcher) merge(fallbackDir, driveDir string) error {
	if w.Merge != nil {
		return w.Merge(fallbackDir, driveDir)
	}
	return Merge(fallbackDir, driveDir)
}

func (w *Watcher) reload() error {
	if w.Reload == nil {
		return nil
	}
	return w.Reload()
}

// enter sets the mode and its start time. The reason of this tick stays.
func (w *Watcher) enter(m Mode) {
	w.status.Mode = m
	w.status.Since = w.now()
}

// fail records a step that did not work. The tick goes on, the mode stays.
func (w *Watcher) fail(step string, err error) {
	w.status.Reason = fmt.Sprintf("%s: %v", step, err)
}

func (w *Watcher) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}
