package storage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Phases of the drive jobs, as the UI shows them.
const (
	PhaseFormat  = "format"
	PhaseMount   = "mount"
	PhaseCopy    = "copy"
	PhaseVerify  = "verify"
	PhaseSwitch  = "switch"
	PhaseCleanup = "cleanup"
)

// Error codes of the drive jobs. Each one needs a catalog entry in the UI.
const (
	CodeFailed          = "storage_failed"
	CodeBusy            = "storage_busy"
	CodeUnknownDrive    = "storage_unknown_drive"
	CodeNeedsFormat     = "storage_needs_format"
	CodeFormatNotNeeded = "storage_format_not_needed"
	CodeNothingSelected = "storage_nothing_selected"
	CodeFallbackInvalid = "storage_fallback_invalid"
	CodeAlreadySetUp    = "storage_already_set_up"
	CodeNotSetUp        = "storage_not_set_up"
	CodeNotOnDrive      = "storage_not_on_drive"
	CodeDriveMissing    = "storage_drive_missing"
	CodeDriveForeign    = "storage_drive_foreign"
	CodeDrivesFailed    = "storage_drives_failed"
	CodeFormatFailed    = "storage_format_failed"
	CodeMountFailed     = "storage_mount_failed"
	CodeUnmountFailed   = "storage_unmount_failed"
	CodeCopyFailed      = "storage_copy_failed"
	CodeVerifyFailed    = "storage_verify_failed"
	CodeStopFailed      = "storage_stop_failed"
	CodeStartFailed     = "storage_start_failed"
	CodeConfigFailed    = "storage_config_failed"
	CodeSwitchFailed    = "storage_switch_failed"
	CodeSaveFailed      = "storage_save_failed"
	CodeRestartFailed   = "storage_restart_failed"
	CodeCleanupFailed   = "storage_cleanup_failed"
	CodeSameDrive       = "storage_same_drive"
	CodeNoSpace         = "storage_no_space"
	// CodeBackupsLost is a warning in the job result, not an error.
	CodeBackupsLost = "storage_backups_lost"
)

// oldRuntimeName is the sibling in data_dir that keeps the runtime data
// setup moved off the system disk, until Finish removes it.
const oldRuntimeName = "runtime.old"

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Error is a failure of a drive job with its catalog code.
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Code + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

func fail(code string, err error) error {
	return &Error{Code: code, Err: err}
}

// withHelperOutput keeps the output the root helper printed before it failed,
// such as DEVICE_MOUNTED, in front of the error. Without it the reason is lost.
func withHelperOutput(out string, err error) error {
	// The helper prints the code and the reason on separate lines, the log
	// entry stays on one line.
	out = strings.Join(strings.Fields(strings.ReplaceAll(out, "\n", " | ")), " ")
	out = strings.Trim(out, " |")
	if out == "" {
		return err
	}
	return fmt.Errorf("%s: %w", out, err)
}

// CodeOf returns the code of err, or "" when err has none.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// Job is the state of the last drive job. Phase is the step that runs or
// failed, Err the code of the failure, Done is set once the job has ended.
type Job struct {
	Phase string `json:"phase"`
	Err   string `json:"err"`
	Done  bool   `json:"done"`
	// Warn is a warning code of the job that did not fail it, "" when none.
	Warn string `json:"warn,omitempty"`
}

// SetupRequest is the drive choice of the user.
type SetupRequest struct {
	Kname      string
	UUID       string
	Format     bool
	UseData    bool
	UseBackups bool
	Fallback   Fallback
}

// Deps are the effects of a job. The production wiring passes the systemactions
// executor and the real lister, the tests replace them.
type Deps struct {
	Lister Lister
	// Storage runs a verb of the root helper, like systemactions.Executor.Storage.
	Storage func(ctx context.Context, verb, arg string) (string, error)
	// StopServices and StartServices stop and start the services that use the data.
	StopServices  func() error
	StartServices func() error
	// RestartDashboard restarts the dashboard, so it reads the new runtime path.
	RestartDashboard func(ctx context.Context) error
	// ReadRuntimeDir returns the paths.runtime_dir the config holds, "" when none.
	ReadRuntimeDir func() string
	// ApplyRuntimeDir writes paths.runtime_dir. "" removes it.
	ApplyRuntimeDir func(ctx context.Context, dir string) error
	// Step runs before each step and may fail it. Nil does nothing.
	Step func(name string) error
	// FreeSpace returns the bytes available to unprivileged writes in dir. Nil uses statfs.
	FreeSpace func(dir string) (uint64, error)
}

// Service runs the drive setup and removal jobs. Only one job runs at a time.
type Service struct {
	Paths   Paths
	DataDir string
	// NodeID is the machine id the volume marker must hold.
	NodeID string
	// Legacy names the legacy locations used while runtime_dir is unset.
	Legacy Legacy
	Deps   Deps

	mu   sync.Mutex
	busy bool
	job  Job
}

// setupPlan is a validated setup request.
type setupPlan struct {
	req       SetupRequest
	drive     Drive
	prev      Config
	hadConfig bool
	// legacy is set when runtime_dir is unset, so the data comes from the legacy places.
	legacy bool
	// legacyAll are the legacy entries, fixed at validation time. legacyPlaces
	// are the distinct legacy locations, which setup renames aside.
	legacyAll    []legacyEntry
	legacyPlaces []legacyEntry
}

// Busy reports whether a setup, remove or resume job is running.
func (s *Service) Busy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy
}

// Job returns the state of the last job.
func (s *Service) Job() Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.job
}

// Setup validates req and runs the setup job synchronously.
func (s *Service) Setup(ctx context.Context, req SetupRequest) error {
	plan, err := s.prepareSetup(ctx, req)
	if err != nil {
		return err
	}
	if err := s.claim(PhaseFormat); err != nil {
		return err
	}
	return s.runSetup(ctx, plan)
}

// StartSetup validates req and runs the setup job in the background. A
// rejected request returns its error at once.
func (s *Service) StartSetup(req SetupRequest) error {
	plan, err := s.prepareSetup(context.Background(), req)
	if err != nil {
		return err
	}
	if err := s.claim(PhaseFormat); err != nil {
		return err
	}
	go func() {
		if err := s.runSetup(context.Background(), plan); err != nil {
			log.Printf("storage setup failed: %v", err)
		}
	}()
	return nil
}

func (s *Service) prepareSetup(ctx context.Context, req SetupRequest) (setupPlan, error) {
	if req.Fallback != FallbackSystem && req.Fallback != FallbackRAM {
		return setupPlan{}, fail(CodeFallbackInvalid, nil)
	}
	if !req.UseData && !req.UseBackups {
		return setupPlan{}, fail(CodeNothingSelected, nil)
	}
	prev, err := LoadConfig(s.DataDir)
	if err != nil {
		return setupPlan{}, fail(CodeConfigFailed, err)
	}
	if prev.UUID != "" {
		return setupPlan{}, fail(CodeAlreadySetUp, nil)
	}
	drive, err := s.findDrive(ctx, req.Kname, req.UUID)
	if err != nil {
		return setupPlan{}, err
	}
	if err := checkFormatChoice(req.Format, drive); err != nil {
		return setupPlan{}, err
	}
	plan := setupPlan{req: req, drive: drive, prev: prev}
	_, statErr := os.Stat(filepath.Join(s.DataDir, configFile))
	plan.hadConfig = statErr == nil
	if req.UseData {
		if _, err := os.Lstat(filepath.Join(s.DataDir, oldRuntimeName)); err == nil {
			return setupPlan{}, fail(CodeSwitchFailed, errors.New("old runtime data still present"))
		}
		if req.UseBackups {
			if _, err := os.Lstat(filepath.Join(s.DataDir, backupsDirName+legacyOldSuffix)); err == nil {
				return setupPlan{}, fail(CodeSwitchFailed, errors.New("old backups still present"))
			}
		}
		plan.legacy = s.readRuntimeDir() == ""
		if plan.legacy {
			plan.legacyAll = s.legacyEntries()
			plan.legacyPlaces = uniqueByPath(plan.legacyAll)
			if err := checkLegacyOld(plan.legacyPlaces); err != nil {
				return setupPlan{}, fail(CodeSwitchFailed, err)
			}
		}
	}
	return plan, nil
}

// findDrive returns the drive the lister offers under kname, and under uuid
// when one is given. Anything else is unknown, also a system disk.
func (s *Service) findDrive(ctx context.Context, kname, uuid string) (Drive, error) {
	drives, err := s.listDrives(ctx)
	if err != nil {
		return Drive{}, err
	}
	return matchDrive(drives, kname, uuid)
}

// listDrives asks the lister for the drives.
func (s *Service) listDrives(ctx context.Context) ([]Drive, error) {
	if s.Deps.Lister == nil {
		return nil, fail(CodeDrivesFailed, nil)
	}
	drives, err := s.Deps.Lister.Drives(ctx)
	if err != nil {
		return nil, fail(CodeDrivesFailed, err)
	}
	return drives, nil
}

// matchDrive picks the drive under kname, and under uuid when one is given.
func matchDrive(drives []Drive, kname, uuid string) (Drive, error) {
	for _, d := range drives {
		if kname != "" && d.Kname == kname && (uuid == "" || d.UUID == uuid) {
			return d, nil
		}
	}
	return Drive{}, fail(CodeUnknownDrive, nil)
}

// checkFormatChoice refuses a format choice that does not match the drive.
func checkFormatChoice(format bool, drive Drive) error {
	switch {
	case format && !drive.NeedsFormat:
		return fail(CodeFormatNotNeeded, nil)
	case !format && drive.NeedsFormat:
		return fail(CodeNeedsFormat, nil)
	case !drive.NeedsFormat && drive.UUID == "":
		return fail(CodeUnknownDrive, nil)
	}
	return nil
}

// runSetup runs the steps of a validated setup. Any failure after the first
// change runs the undo steps in reverse order, so the system disk and the
// configuration are back where they were.
func (s *Service) runSetup(ctx context.Context, p setupPlan) (err error) {
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
		if !uuidPattern.MatchString(out) {
			return fail(CodeFormatFailed, errors.New("helper printed no uuid"))
		}
		uuid = out
	}

	s.setPhase(PhaseMount)
	if err = s.step("mount"); err != nil {
		return fail(CodeMountFailed, err)
	}
	if _, merr := s.Deps.Storage(ctx, "mount", uuid); merr != nil {
		return fail(CodeMountFailed, merr)
	}
	undo = append(undo, func() { _, _ = s.Deps.Storage(context.Background(), "unmount", "") })
	if !VolumeOK(s.Paths.Mount, s.NodeID) {
		return fail(CodeDriveForeign, nil)
	}

	pairs, err := s.setupPairs(p)
	if err != nil {
		return fail(CodeCopyFailed, err)
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

	// backupsOld is the backups.old that holds the system copy of the archives
	// while the drive copy is in use. It is recorded in storage.json.
	var backupsOld string
	if req.UseData {
		s.setPhase(PhaseSwitch)
		if err = s.step("stop"); err != nil {
			return fail(CodeStopFailed, err)
		}
		// Some services may have stopped even when the call fails, so the start is registered first.
		undo = append(undo, func() { _ = s.Deps.StartServices() })
		if err = s.Deps.StopServices(); err != nil {
			return fail(CodeStopFailed, err)
		}

		// Changes made while the services ran go over now, then the copy is checked again.
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

		if err = s.step("config"); err != nil {
			return fail(CodeConfigFailed, err)
		}
		prior := s.Deps.ReadRuntimeDir()
		undo = append(undo, func() { _ = s.Deps.ApplyRuntimeDir(context.Background(), prior) })
		if err = s.Deps.ApplyRuntimeDir(ctx, s.Paths.Runtime); err != nil {
			return fail(CodeConfigFailed, err)
		}

		if err = s.step("switch"); err != nil {
			return fail(CodeSwitchFailed, err)
		}
		oldDir := filepath.Join(s.DataDir, oldRuntimeName)
		moved, merr := moveAside(s.Paths.Runtime, oldDir)
		if merr != nil {
			return fail(CodeSwitchFailed, merr)
		}
		if moved {
			undo = append(undo, func() { _ = os.Rename(oldDir, s.Paths.Runtime) })
		}
		if p.legacy {
			// The legacy files move aside only now, after the copy was verified.
			moves, lerr := legacyMoveAside(p.legacyPlaces)
			if lerr != nil {
				return fail(CodeSwitchFailed, lerr)
			}
			undo = append(undo, func() { undoMoves(moves) })
		}
		if req.UseBackups {
			// The archives are on the drive and verified, so the system copy
			// goes aside too. Finish deletes it after the green restart.
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
		undo = append(undo, func() { _ = removeSymlink(s.Paths.Runtime) })
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
	}
	if req.UseData && p.legacy {
		cfg.Legacy = true
		cfg.LegacyOld = oldPaths(p.legacyPlaces)
	}
	cfg.BackupsOld = backupsOld
	undo = append(undo, func() { restoreConfig(s.DataDir, p) })
	if err = SaveConfig(s.DataDir, cfg); err != nil {
		return fail(CodeSaveFailed, err)
	}

	if !req.UseData && req.UseBackups {
		// Without runtime data there is no restart and no Finish, so the old
		// archives go now. The drive copy was verified above.
		s.setPhase(PhaseCleanup)
		if err = removeOld([]string{filepath.Join(s.DataDir, backupsDirName)}); err != nil {
			return fail(CodeCleanupFailed, err)
		}
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
	}
	return nil
}

// setupPairs lists the trees setup copies: the runtime data into the drive's
// runtime directory, the backups into the drive's backups directory. Without
// runtime_dir the runtime data comes from the legacy places.
func (s *Service) setupPairs(p setupPlan) ([]pair, error) {
	var pairs []pair
	if p.req.UseData && p.legacy {
		for _, e := range p.legacyAll {
			pairs = append(pairs, pair{src: e.path, dst: filepath.Join(DriveRuntimeDir(s.Paths), e.rel)})
		}
	} else if p.req.UseData {
		src, err := resolveDir(s.Paths.Runtime)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, pair{src: src, dst: DriveRuntimeDir(s.Paths)})
	}
	if p.req.UseBackups {
		pairs = append(pairs, pair{
			src: filepath.Join(s.DataDir, "backups"),
			dst: filepath.Join(s.Paths.Mount, "backups"),
		})
	}
	return pairs, nil
}

// claim takes the single job slot and marks the job as started at phase.
func (s *Service) claim(phase string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return fail(CodeBusy, nil)
	}
	s.busy = true
	s.job = Job{Phase: phase}
	return nil
}

// readRuntimeDir returns the runtime_dir the config holds, "" when none.
func (s *Service) readRuntimeDir() string {
	if s.Deps.ReadRuntimeDir == nil {
		return ""
	}
	return s.Deps.ReadRuntimeDir()
}

// setWarn records a warning code of the running job.
func (s *Service) setWarn(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.job.Warn = code
}

func (s *Service) setPhase(phase string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.job.Phase = phase
}

// end marks the job as done and frees the slot. A failure without a code gets
// the generic one, so the UI always has something to show.
func (s *Service) end(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.busy = false
	s.job.Done = true
	s.job.Err = ""
	if err != nil {
		s.job.Err = CodeOf(err)
		if s.job.Err == "" {
			s.job.Err = CodeFailed
		}
	}
}

// step calls the test hook for a named step.
func (s *Service) step(name string) error {
	if s.Deps.Step == nil {
		return nil
	}
	return s.Deps.Step(name)
}

// restoreConfig puts storage.json back as the plan found it: the old content,
// or no file at all.
func restoreConfig(dataDir string, p setupPlan) {
	if p.hadConfig {
		_ = SaveConfig(dataDir, p.prev)
		return
	}
	_ = os.Remove(filepath.Join(dataDir, configFile))
}
