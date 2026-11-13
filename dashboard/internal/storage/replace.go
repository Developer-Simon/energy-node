package storage

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// replaceStagingName is the directory in data_dir that holds the copy of the
// old drive's data while the old drive is unmounted. Finish deletes it after
// the green restart.
const replaceStagingName = "replace-staging"

// ReplaceRequest is the choice of the new drive. It has the fields of SetupRequest.
type ReplaceRequest struct {
	Kname      string
	UUID       string
	Format     bool
	UseData    bool
	UseBackups bool
	Fallback   Fallback
}

// replacePlan is a validated replace request.
type replacePlan struct {
	req   SetupRequest
	drive Drive
	prev  Config
	// oldOK is set when the old drive is mounted and carries this node's marker.
	oldOK bool
	// stageRuntime and stageBackups copy the old drive's data to the staging
	// before the old drive is unmounted, because the mount point is shared.
	stageRuntime bool
	stageBackups bool
	stagingDir   string
	// lost is set when the backups lived only on the missing drive.
	lost bool
}

// Replace moves the runtime data and the backups from the current drive, or
// from the fallback and the system disk when the drive is missing, onto
// another drive, then points the runtime path at it. The old drive is never
// wiped. The job runs synchronously, like Setup.
func (s *Service) Replace(ctx context.Context, req ReplaceRequest) error {
	plan, err := s.prepareReplace(ctx, SetupRequest(req))
	if err != nil {
		return err
	}
	if err := s.claim(PhaseSwitch); err != nil {
		return err
	}
	return s.runReplace(ctx, plan)
}

// StartReplace validates req and runs the replace job in the background. A
// rejected request returns its error at once, like StartSetup.
func (s *Service) StartReplace(req ReplaceRequest) error {
	plan, err := s.prepareReplace(context.Background(), SetupRequest(req))
	if err != nil {
		return err
	}
	if err := s.claim(PhaseSwitch); err != nil {
		return err
	}
	go func() { _ = s.runReplace(context.Background(), plan) }()
	return nil
}

func (s *Service) prepareReplace(ctx context.Context, req SetupRequest) (replacePlan, error) {
	if req.Fallback != FallbackSystem && req.Fallback != FallbackRAM {
		return replacePlan{}, fail(CodeFallbackInvalid, nil)
	}
	if !req.UseData && !req.UseBackups {
		return replacePlan{}, fail(CodeNothingSelected, nil)
	}
	prev, err := LoadConfig(s.DataDir)
	if err != nil {
		return replacePlan{}, fail(CodeConfigFailed, err)
	}
	if prev.UUID == "" {
		return replacePlan{}, fail(CodeNotSetUp, nil)
	}
	_, pending, err := readPending(s.DataDir)
	if err != nil {
		return replacePlan{}, fail(CodeCleanupFailed, err)
	}
	if pending {
		return replacePlan{}, fail(CodeSwitchFailed, errors.New("a previous job waits for its follow-up"))
	}
	stagingDir := filepath.Join(s.DataDir, replaceStagingName)
	for _, p := range []string{
		filepath.Join(s.DataDir, oldRuntimeName),
		filepath.Join(s.DataDir, backupsDirName+legacyOldSuffix),
		stagingDir,
	} {
		if _, err := os.Lstat(p); err == nil {
			return replacePlan{}, fail(CodeSwitchFailed, errors.New("leftover of an earlier job: "+p))
		}
	}

	drives, err := s.listDrives(ctx)
	if err != nil {
		return replacePlan{}, err
	}
	drive, err := matchDrive(drives, req.Kname, req.UUID)
	if err != nil {
		return replacePlan{}, err
	}
	if drive.UUID == prev.UUID {
		return replacePlan{}, fail(CodeSameDrive, nil)
	}
	if err := checkFormatChoice(req.Format, drive); err != nil {
		return replacePlan{}, err
	}

	oldOK := VolumeOK(s.Paths.Mount, s.NodeID)
	plan := replacePlan{
		req:          req,
		drive:        drive,
		prev:         prev,
		oldOK:        oldOK,
		stageRuntime: oldOK && prev.UseData,
		stageBackups: oldOK && prev.UseBackups && req.UseBackups,
		stagingDir:   stagingDir,
		lost:         prev.UseBackups && !oldOK && !hasFiles(filepath.Join(s.DataDir, backupsDirName)),
	}
	if plan.stageRuntime || plan.stageBackups {
		var need uint64
		if plan.stageRuntime {
			_, n, err := walkFiles(DriveRuntimeDir(s.Paths))
			if err != nil {
				return replacePlan{}, fail(CodeNoSpace, err)
			}
			need += n
		}
		if plan.stageBackups {
			_, n, err := walkFiles(filepath.Join(s.Paths.Mount, backupsDirName))
			if err != nil {
				return replacePlan{}, fail(CodeNoSpace, err)
			}
			need += n
		}
		free, err := s.freeSpace(s.DataDir)
		if err != nil {
			return replacePlan{}, fail(CodeNoSpace, err)
		}
		if free < need {
			return replacePlan{}, fail(CodeNoSpace, errors.New("the system disk has too little space for the staging"))
		}
	}
	return plan, nil
}

func (s *Service) freeSpace(dir string) (uint64, error) {
	if s.Deps.FreeSpace != nil {
		return s.Deps.FreeSpace(dir)
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}

// runReplace runs the steps of a validated replace. Any failure runs the undo
// steps in reverse order, so the old setup is back as it was.
func (s *Service) runReplace(ctx context.Context, p replacePlan) (err error) {
	var undo []func()
	defer func() {
		if err != nil {
			for i := len(undo) - 1; i >= 0; i-- {
				undo[i]()
			}
		}
		s.end(err)
	}()

	req := p.req
	prev := p.prev
	staged := p.stageRuntime || p.stageBackups
	runtimeLink := isSymlink(s.Paths.Runtime)
	if p.lost {
		s.setWarn(CodeBackupsLost)
	}

	// The services stop first, so the data does not change under the copy.
	s.setPhase(PhaseSwitch)
	if err = s.step("stop"); err != nil {
		return fail(CodeStopFailed, err)
	}
	undo = append(undo, func() { _ = s.Deps.StartServices() })
	if err = s.Deps.StopServices(); err != nil {
		return fail(CodeStopFailed, err)
	}

	if staged {
		s.setPhase(PhaseCopy)
		if err = s.step("stage"); err != nil {
			return fail(CodeCopyFailed, err)
		}
		undo = append(undo, func() { _ = os.RemoveAll(p.stagingDir) })
		stage := p.stagePairs(s.Paths)
		if err = copyPairs(stage); err != nil {
			return fail(CodeCopyFailed, err)
		}
		s.setPhase(PhaseVerify)
		if err = s.step("verify_stage"); err != nil {
			return fail(CodeVerifyFailed, err)
		}
		if err = verifyPairs(stage); err != nil {
			return fail(CodeVerifyFailed, err)
		}
	}

	// The mount point is shared, so the old drive leaves it before the new one comes in.
	if p.oldOK {
		s.setPhase(PhaseMount)
		if err = s.step("unmount"); err != nil {
			return fail(CodeUnmountFailed, err)
		}
		if _, err = s.Deps.Storage(ctx, "unmount", ""); err != nil {
			return fail(CodeUnmountFailed, err)
		}
		undo = append(undo, func() { _, _ = s.Deps.Storage(context.Background(), "mount", prev.UUID) })
	}

	uuid := p.drive.UUID
	if req.Format {
		s.setPhase(PhaseFormat)
		if err = s.step("format"); err != nil {
			return fail(CodeFormatFailed, err)
		}
		out, ferr := s.Deps.Storage(ctx, "format", p.drive.Kname)
		if ferr != nil {
			return fail(CodeFormatFailed, withHelperOutput(out, ferr))
		}
		if !uuidPattern.MatchString(out) || out == prev.UUID {
			return fail(CodeFormatFailed, errors.New("helper printed no new uuid"))
		}
		uuid = out
	}

	s.setPhase(PhaseMount)
	if err = s.step("mount"); err != nil {
		return fail(CodeMountFailed, err)
	}
	if _, err = s.Deps.Storage(ctx, "mount", uuid); err != nil {
		return fail(CodeMountFailed, err)
	}
	undo = append(undo, func() { _, _ = s.Deps.Storage(context.Background(), "unmount", "") })
	if !VolumeOK(s.Paths.Mount, s.NodeID) {
		return fail(CodeDriveForeign, nil)
	}

	runtimeSrc := filepath.Join(p.stagingDir, "runtime")
	if !p.stageRuntime {
		runtimeSrc, err = resolveDir(s.Paths.Runtime)
		if err != nil {
			return fail(CodeCopyFailed, err)
		}
	}
	backupsSrc := filepath.Join(s.DataDir, backupsDirName)
	if p.stageBackups {
		backupsSrc = filepath.Join(p.stagingDir, "backups")
	}
	pairs := p.copyPairs(s.Paths, runtimeSrc, backupsSrc, runtimeLink)
	for _, pr := range pairs {
		if err = os.MkdirAll(pr.dst, 0o750); err != nil {
			return fail(CodeCopyFailed, err)
		}
	}

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

	var backupsOld string
	if req.UseData {
		s.setPhase(PhaseSwitch)
		if err = s.step("switch"); err != nil {
			return fail(CodeSwitchFailed, err)
		}
		if runtimeLink {
			prevLink, lerr := os.Readlink(s.Paths.Runtime)
			if lerr != nil {
				return fail(CodeSwitchFailed, lerr)
			}
			undo = append(undo, func() { _ = repointSymlink(s.Paths.Runtime, prevLink) })
		} else {
			oldDir := filepath.Join(s.DataDir, oldRuntimeName)
			moved, merr := moveAside(s.Paths.Runtime, oldDir)
			if merr != nil {
				return fail(CodeSwitchFailed, merr)
			}
			if moved {
				undo = append(undo, func() { _ = os.Rename(oldDir, s.Paths.Runtime) })
			}
		}
		if req.UseBackups {
			backupsOld, err = moveBackupsAside(s.DataDir)
			if err != nil {
				return fail(CodeSwitchFailed, err)
			}
			if backupsOld != "" {
				dir := filepath.Join(s.DataDir, backupsDirName)
				undo = append(undo, func() { _ = os.Rename(backupsOld, dir) })
			}
		}
		if err = Switch(s.Paths, ModeDrive); err != nil {
			return fail(CodeSwitchFailed, err)
		}
		if !runtimeLink {
			undo = append(undo, func() { _ = removeSymlink(s.Paths.Runtime) })
		}
	} else if runtimeLink {
		s.setPhase(PhaseSwitch)
		if err = s.step("switch"); err != nil {
			return fail(CodeSwitchFailed, err)
		}
		prevLink, lerr := os.Readlink(s.Paths.Runtime)
		if lerr != nil {
			return fail(CodeSwitchFailed, lerr)
		}
		undo = append(undo, func() { _ = repointSymlink(s.Paths.Runtime, prevLink) })
		if err = Switch(s.Paths, ModeFallbackSystem); err != nil {
			return fail(CodeSwitchFailed, err)
		}
	}

	if err = s.step("save"); err != nil {
		return fail(CodeSaveFailed, err)
	}
	cfg := Config{
		UUID:       uuid,
		Name:       p.drive.Name,
		Fallback:   req.Fallback,
		UseData:    req.UseData,
		UseBackups: req.UseBackups,
		Legacy:     prev.Legacy,
		BackupsOld: backupsOld,
	}
	if staged && req.UseData {
		cfg.ReplaceStaging = p.stagingDir
	}
	undo = append(undo, func() { _ = SaveConfig(s.DataDir, prev) })
	if err = SaveConfig(s.DataDir, cfg); err != nil {
		return fail(CodeSaveFailed, err)
	}

	if req.UseData {
		if err = s.step("restart"); err != nil {
			return fail(CodeRestartFailed, err)
		}
		undo = append(undo, func() { _ = clearPending(s.DataDir) })
		if err = writePending(s.DataDir, pendingSetup); err != nil {
			return fail(CodeRestartFailed, err)
		}
		if err = s.Deps.RestartDashboard(ctx); err != nil {
			return fail(CodeRestartFailed, err)
		}
		return nil
	}

	// Without runtime data there is no restart and no Finish. The verified
	// copy on the drive is the only place the old copies are needed.
	s.setPhase(PhaseCleanup)
	var old []string
	if staged {
		old = append(old, p.stagingDir)
	}
	if req.UseBackups {
		old = append(old, filepath.Join(s.DataDir, backupsDirName))
	}
	if err = removeOld(old); err != nil {
		return fail(CodeCleanupFailed, err)
	}
	return nil
}

// stagePairs lists the old drive's trees that the staging keeps.
func (p replacePlan) stagePairs(paths Paths) []pair {
	var out []pair
	if p.stageRuntime {
		out = append(out, pair{src: DriveRuntimeDir(paths), dst: filepath.Join(p.stagingDir, "runtime")})
	}
	if p.stageBackups {
		out = append(out, pair{src: filepath.Join(paths.Mount, backupsDirName), dst: filepath.Join(p.stagingDir, "backups")})
	}
	return out
}

// copyPairs lists the trees the new drive gets. The runtime goes to the drive
// with use_data, to the system fallback without it, and nowhere when the
// runtime path is a real directory that stays on the system disk.
func (p replacePlan) copyPairs(paths Paths, runtimeSrc, backupsSrc string, runtimeLink bool) []pair {
	var out []pair
	switch {
	case p.req.UseData:
		out = append(out, pair{src: runtimeSrc, dst: DriveRuntimeDir(paths)})
	case runtimeLink:
		out = append(out, pair{src: runtimeSrc, dst: paths.SystemFallback})
	}
	if p.req.UseBackups {
		out = append(out, pair{src: backupsSrc, dst: filepath.Join(paths.Mount, backupsDirName)})
	}
	var keep []pair
	for _, pr := range out {
		if samePath(pr.src, pr.dst) {
			continue
		}
		keep = append(keep, pr)
	}
	return keep
}

// samePath reports whether a and b name the same non-empty path.
func samePath(a, b string) bool {
	return a != "" && filepath.Clean(a) == filepath.Clean(b)
}

func isSymlink(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

// repointSymlink points the symlink at target again, the way Switch does.
func repointSymlink(link, target string) error {
	tmp := link + ".new"
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, link); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// walkFiles counts the regular files under dir and their bytes. A missing dir counts nothing.
func walkFiles(dir string) (int, uint64, error) {
	if missing(dir) {
		return 0, 0, nil
	}
	var n int
	var size uint64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		n++
		size += uint64(fi.Size())
		return nil
	})
	return n, size, err
}

// hasFiles reports whether dir holds at least one regular file.
func hasFiles(dir string) bool {
	n, _, err := walkFiles(dir)
	return err == nil && n > 0
}
