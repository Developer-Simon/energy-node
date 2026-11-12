package storage

import (
	"context"
	"path/filepath"
)

// Remove moves the runtime data and the backups back to the system disk,
// unmounts the drive and forgets it. The drive itself is not wiped.
func (s *Service) Remove(ctx context.Context) error {
	cfg, err := s.prepareRemove()
	if err != nil {
		return err
	}
	if err := s.claim(PhaseCopy); err != nil {
		return err
	}
	return s.runRemove(ctx, cfg)
}

// prepareRemove checks that a drive is set up, present and holds the runtime
// data when the data is on the drive. Otherwise the data would be copied over
// newer data on the fallback.
func (s *Service) prepareRemove() (Config, error) {
	cfg, err := LoadConfig(s.DataDir)
	if err != nil {
		return Config{}, fail(CodeConfigFailed, err)
	}
	if cfg.UUID == "" {
		return Config{}, fail(CodeNotSetUp, nil)
	}
	if !VolumeOK(s.Paths.Mount, s.NodeID) {
		return Config{}, fail(CodeDriveMissing, nil)
	}
	if cfg.UseData && !s.runtimeOnDrive() {
		return Config{}, fail(CodeNotOnDrive, nil)
	}
	return cfg, nil
}

func (s *Service) runRemove(ctx context.Context, cfg Config) (err error) {
	var undo []func()
	defer func() {
		if err != nil {
			for i := len(undo) - 1; i >= 0; i-- {
				undo[i]()
			}
		}
		s.end(err)
	}()

	pairs := s.removePairs(cfg)

	s.setPhase(PhaseCopy)
	if err = s.step("copy"); err != nil {
		return fail(CodeCopyFailed, err)
	}
	if err = copyPairs(pairs); err != nil {
		return fail(CodeCopyFailed, err)
	}

	s.setPhase(PhaseVerify)
	if err = s.step("verify"); err != nil {
		return fail(CodeVerifyFailed, err)
	}
	if err = verifyPairs(pairs); err != nil {
		return fail(CodeVerifyFailed, err)
	}

	if cfg.UseData {
		s.setPhase(PhaseSwitch)
		if err = s.step("stop"); err != nil {
			return fail(CodeStopFailed, err)
		}
		// Some services may have stopped even when the call fails, so the start is registered first.
		undo = append(undo, func() { _ = s.Deps.StartServices() })
		if err = s.Deps.StopServices(); err != nil {
			return fail(CodeStopFailed, err)
		}

		if err = s.step("copy_final"); err != nil {
			return fail(CodeCopyFailed, err)
		}
		if err = copyPairs(pairs); err != nil {
			return fail(CodeCopyFailed, err)
		}
		if err = s.step("verify_final"); err != nil {
			return fail(CodeVerifyFailed, err)
		}
		if err = verifyPairs(pairs); err != nil {
			return fail(CodeVerifyFailed, err)
		}
	}

	if cfg.UseData {
		if err = s.step("switch"); err != nil {
			return fail(CodeSwitchFailed, err)
		}
		if cfg.Legacy {
			// The legacy layout goes back to runtime_dir unset and the runtime
			// path is removed, so the services read the legacy places again.
			prior := s.readRuntimeDir()
			undo = append(undo, func() { _ = s.Deps.ApplyRuntimeDir(context.Background(), prior) })
			if err = s.Deps.ApplyRuntimeDir(ctx, ""); err != nil {
				return fail(CodeConfigFailed, err)
			}
			if err = removeSymlink(s.Paths.Runtime); err != nil {
				return fail(CodeSwitchFailed, err)
			}
			undo = append(undo, func() { _ = Switch(s.Paths, ModeDrive) })
		} else {
			if err = Switch(s.Paths, ModeFallbackSystem); err != nil {
				return fail(CodeSwitchFailed, err)
			}
			undo = append(undo, func() { _ = Switch(s.Paths, ModeDrive) })
		}
	}

	// The runtime path has left the drive, so the drive can be unmounted now.
	// The remount is registered last, so on a rollback it runs before the
	// switch back to the drive.
	if err = s.step("unmount"); err != nil {
		return fail(CodeUnmountFailed, err)
	}
	if _, err = s.Deps.Storage(ctx, "unmount", ""); err != nil {
		return fail(CodeUnmountFailed, err)
	}
	undo = append(undo, func() { _, _ = s.Deps.Storage(context.Background(), "mount", cfg.UUID) })

	if err = s.step("save"); err != nil {
		return fail(CodeSaveFailed, err)
	}
	undo = append(undo, func() { _ = SaveConfig(s.DataDir, cfg) })
	if err = SaveConfig(s.DataDir, Config{Fallback: cfg.Fallback}); err != nil {
		return fail(CodeSaveFailed, err)
	}

	if cfg.UseData {
		if err = s.step("restart"); err != nil {
			return fail(CodeRestartFailed, err)
		}
		undo = append(undo, func() { _ = clearPending(s.DataDir) })
		if err = writePending(s.DataDir, pendingRemove); err != nil {
			return fail(CodeRestartFailed, err)
		}
		if err = s.Deps.RestartDashboard(ctx); err != nil {
			return fail(CodeRestartFailed, err)
		}
	}
	return nil
}

// removePairs lists the trees remove copies back: the runtime data from the
// drive root to the system fallback, without the marker, the backups and
// lost+found, and the backups to the data directory.
func (s *Service) removePairs(cfg Config) []pair {
	var pairs []pair
	if cfg.UseData && cfg.Legacy {
		// Back to the legacy places, one copy per place. Battery ids that share
		// the legacy state file restore from the first of them.
		for _, e := range uniqueByPath(s.legacyEntries()) {
			pairs = append(pairs, pair{src: filepath.Join(DriveRuntimeDir(s.Paths), e.rel), dst: e.path})
		}
	} else if cfg.UseData {
		pairs = append(pairs, pair{
			src: DriveRuntimeDir(s.Paths),
			dst: s.Paths.SystemFallback,
		})
	}
	if cfg.UseBackups {
		pairs = append(pairs, pair{
			src: filepath.Join(s.Paths.Mount, "backups"),
			dst: filepath.Join(s.DataDir, "backups"),
		})
	}
	return pairs
}

// StartRemove runs Remove in the background. A rejected request returns its error at once.
func (s *Service) StartRemove() error {
	cfg, err := s.prepareRemove()
	if err != nil {
		return err
	}
	if err := s.claim(PhaseCopy); err != nil {
		return err
	}
	go func() { _ = s.runRemove(context.Background(), cfg) }()
	return nil
}

// Finish is the follow-up step after a restart with green status. On the
// drive it deletes the old runtime data that setup kept, then starts the
// services that the job stopped, and removes the pending marker.
func (s *Service) Finish(ctx context.Context) error {
	if err := s.claim(PhaseCleanup); err != nil {
		return err
	}
	err := s.finish()
	if err == nil {
		if cerr := clearPending(s.DataDir); cerr != nil {
			err = fail(CodeCleanupFailed, cerr)
		}
	}
	s.end(err)
	return err
}

// Resume runs Finish on boot when a job left a pending marker and the status
// is green: the drive is present and the runtime path leads to the target of
// the job. It reports whether it ran. Without green status the marker stays,
// and the next boot tries again.
func (s *Service) Resume(ctx context.Context) (bool, error) {
	kind, pending, err := readPending(s.DataDir)
	if err != nil {
		return false, fail(CodeCleanupFailed, err)
	}
	if !pending || !s.green(kind) {
		return false, nil
	}
	if err := s.Finish(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// green reports whether the runtime path has reached the target of a job of kind.
func (s *Service) green(kind string) bool {
	switch kind {
	case pendingSetup:
		return VolumeOK(s.Paths.Mount, s.NodeID) && s.runtimeOnDrive()
	case pendingRemove:
		return s.runtimeOn(s.Paths.SystemFallback)
	}
	return false
}

func (s *Service) finish() error {
	cfg, err := LoadConfig(s.DataDir)
	if err != nil {
		return fail(CodeCleanupFailed, err)
	}
	if VolumeOK(s.Paths.Mount, s.NodeID) && s.runtimeOnDrive() {
		if err := mergeLeftoverEnergyStats(s.DataDir, DriveRuntimeDir(s.Paths)); err != nil {
			return fail(CodeCleanupFailed, err)
		}
		old := append([]string{filepath.Join(s.DataDir, oldRuntimeName)}, cfg.LegacyOld...)
		if cfg.BackupsOld != "" {
			old = append(old, cfg.BackupsOld)
		}
		if cfg.ReplaceStaging != "" {
			old = append(old, cfg.ReplaceStaging)
		}
		if err := removeOld(old); err != nil {
			return fail(CodeCleanupFailed, err)
		}
	}
	if err := s.Deps.StartServices(); err != nil {
		return fail(CodeStartFailed, err)
	}
	return nil
}

// runtimeOnDrive reports whether the runtime path leads to the drive runtime directory.
func (s *Service) runtimeOnDrive() bool {
	return s.runtimeOn(DriveRuntimeDir(s.Paths))
}

// runtimeOn reports whether the runtime path leads to dir.
func (s *Service) runtimeOn(dir string) bool {
	rt, err := filepath.EvalSymlinks(s.Paths.Runtime)
	if err != nil {
		return false
	}
	target, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	return rt == target
}
