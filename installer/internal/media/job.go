package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// StepID names one step of a write job.
type StepID string

const (
	StepDownload  StepID = "download"
	StepChecksum  StepID = "checksum"
	StepRights    StepID = "rights"
	StepWrite     StepID = "write"
	StepVerify    StepID = "verify"
	StepCustomize StepID = "customize"
	StepEject     StepID = "eject"
)

// StepState is the state of one step: "pending" | "active" | "done" | "failed".
type StepState string

const (
	StepPending StepState = "pending"
	StepActive  StepState = "active"
	StepDone    StepState = "done"
	StepFailed  StepState = "failed"
)

// StepView is one step as shown to the browser.
type StepView struct {
	ID    StepID    `json:"id"`
	State StepState `json:"state"`
}

// JobState is the state of the write job: "idle" | "running" | "done" | "failed" | "cancelled".
type JobState string

const (
	JobIdle      JobState = "idle"
	JobRunning   JobState = "running"
	JobDone      JobState = "done"
	JobFailed    JobState = "failed"
	JobCancelled JobState = "cancelled"
)

// JobError is the error of a failed or cancelled job.
type JobError struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// JobView is the snapshot of the write job that the browser polls.
type JobView struct {
	State      JobState   `json:"state"`
	Steps      []StepView `json:"steps"`
	Percent    int        `json:"percent"` // des aktiven Schritts
	BytesDone  int64      `json:"bytes_done"`
	BytesTotal int64      `json:"bytes_total"`
	Rate       int64      `json:"rate"` // Byte/s, gleitend ueber 3 s
	Error      *JobError  `json:"error"`
	Log        []string   `json:"log"`    // letzte 200 Zeilen
	Target     *Target    `json:"target"` // erst bei done
}

// Target tells where the finished device can be reached.
type Target struct {
	Host    string `json:"host"`
	User    string `json:"user"`
	KeyPath string `json:"key_path"`
}

// WriteRequest is one write job as requested by the browser.
type WriteRequest struct {
	Device   string   // Device.ID
	Drive    string   // Drive.Path
	Confirm  string   // Wechselmedium: "yes"; intern: exakt Drive.Model
	Settings Settings // PasswordHash leer; Klartext in Password
	Password string   // Notfall-Passwort im Klartext, "" = keins
}

// Runner starts a command and returns its combined stdout and stderr.
type Runner func(ctx context.Context, name string, args ...string) (output io.ReadCloser, wait func() (exitCode int, err error), err error)

// Writer runs at most one write job at a time.
type Writer struct {
	Cache      *Cache
	Imager     func(ctx context.Context) (Imager, bool)   // FindImager mit echtem Env
	Drives     func(ctx context.Context) ([]Drive, error) // ListDrives
	Run        Runner                                     // exec, stdout+stderr zusammen
	KnownHosts string                                     // ~/.energy-node/known_hosts
	KeyPath    string                                     // privater Installer-Schluessel, landet in Target.KeyPath
	WorkDir    string                                     // <Cache.Dir>/jobs

	mu     sync.Mutex
	busy   bool
	cancel context.CancelFunc
	view   JobView
}

var (
	ErrBusy          = errors.New("media: a write is already running")
	ErrDriveGone     = errors.New("media: drive is no longer listed")
	ErrConfirm       = errors.New("media: confirmation does not match the drive")
	ErrSettings      = errors.New("media: settings are invalid")
	ErrImagerMissing = errors.New("media: raspberry pi imager is missing or too old")
	ErrTooSmall      = errors.New("media: drive is smaller than the image")

	errNoImage = errors.New("media: no image for this device in the OS list")
)

const (
	codeWriteFailed  = "MEDIA_WRITE_FAILED"
	codeVerifyFailed = "MEDIA_VERIFY_FAILED"
	codeRightsDenied = "MEDIA_RIGHTS_DENIED"
	codeCancelled    = "MEDIA_CANCELLED"

	logLimit   = 200
	rateWindow = 3 * time.Second
)

var wifiCountryPattern = regexp.MustCompile(`^[A-Z]{2}$`)

// plan is a validated write request with everything the job needs.
type plan struct {
	req    WriteRequest
	device Device
	drive  Drive
	image  Image
	imager Imager
}

// Start checks the request synchronously and then starts the job in the background.
// The checks run in this order: busy, settings, device, drive, confirmation,
// image, imager, drive size.
func (w *Writer) Start(ctx context.Context, req WriteRequest) error {
	w.mu.Lock()
	if w.busy {
		w.mu.Unlock()
		return ErrBusy
	}
	w.busy = true
	w.mu.Unlock()

	p, err := w.check(ctx, req)
	if err != nil {
		w.mu.Lock()
		w.busy = false
		w.mu.Unlock()
		return err
	}

	// The job outlives the request that started it, so it must not inherit its cancellation.
	jobCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	w.mu.Lock()
	w.cancel = cancel
	w.view = JobView{State: JobRunning, Steps: newSteps()}
	w.mu.Unlock()

	go func() {
		defer cancel()
		w.run(jobCtx, p)
	}()
	return nil
}

// check performs the synchronous validation of req and returns the resolved plan.
func (w *Writer) check(ctx context.Context, req WriteRequest) (*plan, error) {
	if !validSettings(req.Settings) {
		return nil, ErrSettings
	}
	dev, ok := DeviceByID(req.Device)
	if !ok {
		return nil, ErrSettings
	}
	drives, err := w.Drives(ctx)
	if err != nil {
		return nil, fmt.Errorf("media: list drives: %w", err)
	}
	var drive Drive
	found := false
	for _, d := range drives {
		if d.Path == req.Drive {
			drive, found = d, true
			break
		}
	}
	if !found {
		return nil, ErrDriveGone
	}
	if drive.Removable {
		if req.Confirm != "yes" {
			return nil, ErrConfirm
		}
	} else if req.Confirm != drive.Model {
		return nil, ErrConfirm
	}
	list, _, err := w.Cache.OSList(ctx)
	if err != nil {
		return nil, fmt.Errorf("media: OS list: %w", err)
	}
	image, ok := list.Find(dev.ImageName, dev.Tag)
	if !ok {
		return nil, errNoImage
	}
	imager, ok := w.Imager(ctx)
	if !ok || imager.TooOld() {
		return nil, ErrImagerMissing
	}
	if drive.Size < image.ExtractSize {
		return nil, ErrTooSmall
	}
	return &plan{req: req, device: dev, drive: drive, image: image, imager: imager}, nil
}

// validSettings checks the user settings. The password hash must be empty,
// because the browser sends the password in clear text.
func validSettings(s Settings) bool {
	if s.PasswordHash != "" || !ValidHostname(s.Hostname) || !ValidUser(s.User) {
		return false
	}
	if s.Wifi != nil && (s.Wifi.SSID == "" || !wifiCountryPattern.MatchString(s.Wifi.Country)) {
		return false
	}
	return true
}

// newSteps returns the step list of a new job, all pending.
func newSteps() []StepView {
	ids := []StepID{StepDownload, StepChecksum, StepRights, StepWrite, StepVerify, StepCustomize, StepEject}
	steps := make([]StepView, len(ids))
	for i, id := range ids {
		steps[i] = StepView{ID: id, State: StepPending}
	}
	return steps
}

// run executes the job and stores its final state.
func (w *Writer) run(ctx context.Context, p *plan) {
	state, jobErr := w.execute(ctx, p)
	w.finish(state, jobErr)
}

// execute runs the steps in order and returns the final state. Its temporary
// folder is removed on every path.
func (w *Writer) execute(ctx context.Context, p *plan) (JobState, *JobError) {
	r := &jobRun{w: w, ctx: ctx, p: p}

	imgPath, err := r.ensureImage()
	if err != nil {
		return r.fail(codeWriteFailed, err.Error())
	}

	if err := os.MkdirAll(w.WorkDir, 0o700); err != nil {
		return r.fail(codeWriteFailed, err.Error())
	}
	tmp, err := os.MkdirTemp(w.WorkDir, "job-")
	if err != nil {
		return r.fail(codeWriteFailed, err.Error())
	}
	defer os.RemoveAll(tmp)

	args, err := r.prepareFiles(tmp)
	if err != nil {
		return r.fail(codeWriteFailed, err.Error())
	}
	args.Image = imgPath
	args.Drive = p.drive.Path
	args.SystemDrive = !p.drive.Removable
	args.ReadDirs = []string{tmp, filepath.Dir(imgPath)}
	return r.runImager(args)
}

// jobRun holds the state of one job run. Only the run goroutine touches it.
// Shared state goes through Writer.update.
type jobRun struct {
	w      *Writer
	ctx    context.Context
	p      *plan
	active StepID // step that is currently active, "" if none

	hk       HostKey
	phase    Phase // last write or verify phase seen
	sawDone  bool
	errMsg   string
	errPhase Phase
	samples  []rateSample
}

type rateSample struct {
	at   time.Time
	done int64
}

// update applies f to the view under the writer lock.
func (w *Writer) update(f func(v *JobView)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	f(&w.view)
}

// setStep sets the state of one step.
func (w *Writer) setStep(id StepID, state StepState) {
	w.update(func(v *JobView) {
		for i := range v.Steps {
			if v.Steps[i].ID == id {
				v.Steps[i].State = state
			}
		}
	})
}

// fail marks the active step as failed. A cancelled context wins over the code.
func (r *jobRun) fail(code, detail string) (JobState, *JobError) {
	if r.active != "" {
		r.w.setStep(r.active, StepFailed)
	}
	if r.ctx.Err() != nil {
		return JobCancelled, &JobError{Code: codeCancelled, Detail: "cancelled by user"}
	}
	return JobFailed, &JobError{Code: code, Detail: detail}
}

// move makes next the active step and closes the previous one.
func (r *jobRun) move(next StepID) {
	if r.active != "" && r.active != next {
		r.w.setStep(r.active, StepDone)
	}
	r.active = next
	r.w.update(func(v *JobView) {
		for i := range v.Steps {
			if v.Steps[i].ID == next {
				v.Steps[i].State = StepActive
			}
		}
		v.Percent = 0
		v.BytesDone = 0
	})
}

// done closes one step.
func (r *jobRun) done(id StepID) {
	r.w.setStep(id, StepDone)
	if r.active == id {
		r.active = ""
	}
}

// setPercent shows the percent of the active step, for download and checksum.
func (r *jobRun) setPercent(done, total int64) {
	pct := 0
	if total > 0 {
		pct = int(done * 100 / total)
	}
	r.w.update(func(v *JobView) { v.Percent = pct })
}

// ensureImage runs the download and checksum steps.
func (r *jobRun) ensureImage() (string, error) {
	img := r.p.image
	if r.w.Cache.Cached(img) {
		r.w.setStep(StepDownload, StepDone)
		r.move(StepChecksum)
	}
	path, err := r.w.Cache.Ensure(r.ctx, img, r.onDownload, r.onVerify)
	if err != nil {
		return "", err
	}
	r.w.setStep(StepDownload, StepDone)
	r.done(StepChecksum)
	return path, nil
}

func (r *jobRun) onDownload(done, total int64) {
	if r.active != StepDownload {
		r.move(StepDownload)
	}
	r.setPercent(done, total)
}

func (r *jobRun) onVerify(done, total int64) {
	if r.active != StepChecksum {
		r.move(StepChecksum)
	}
	r.setPercent(done, total)
}

// prepareFiles writes the first-boot files into tmp and returns the matching write arguments.
func (r *jobRun) prepareFiles(tmp string) (WriteArgs, error) {
	hk, err := NewHostKey()
	if err != nil {
		return WriteArgs{}, fmt.Errorf("host key: %w", err)
	}
	r.hk = hk

	s := r.p.req.Settings
	if r.p.req.Password != "" {
		hash, err := HashPassword(r.p.req.Password)
		if err != nil {
			return WriteArgs{}, fmt.Errorf("password hash: %w", err)
		}
		s.PasswordHash = hash
	}

	var a WriteArgs
	switch format := r.p.image.InitFormat; format {
	case "cloudinit-rpi", "cloudinit":
		a.UserData = filepath.Join(tmp, "user-data")
		if err := os.WriteFile(a.UserData, UserData(s, hk), 0o600); err != nil {
			return WriteArgs{}, fmt.Errorf("write user-data: %w", err)
		}
		if s.Wifi != nil {
			a.NetworkConfig = filepath.Join(tmp, "network-config")
			if err := os.WriteFile(a.NetworkConfig, NetworkConfig(s), 0o600); err != nil {
				return WriteArgs{}, fmt.Errorf("write network-config: %w", err)
			}
		}
	case "systemd":
		a.FirstRun = filepath.Join(tmp, "firstrun.sh")
		if err := os.WriteFile(a.FirstRun, FirstRunScript(s, hk), 0o600); err != nil {
			return WriteArgs{}, fmt.Errorf("write firstrun.sh: %w", err)
		}
	default:
		return WriteArgs{}, fmt.Errorf("init_format %s", format)
	}
	return a, nil
}

// runImager starts the imager, follows its output and decides the outcome.
func (r *jobRun) runImager(a WriteArgs) (JobState, *JobError) {
	name, args := r.p.imager.Command(a)
	r.move(StepRights)

	out, wait, err := r.w.Run(r.ctx, name, args...)
	if err != nil {
		return r.fail(codeWriteFailed, err.Error())
	}
	defer out.Close()

	cr := &countReader{r: out}
	logs := &logSink{w: r.w}
	perr := ParseImagerOutput(io.TeeReader(cr, logs), r.onProgress)
	logs.flush()
	code, werr := wait()

	if r.ctx.Err() != nil {
		return r.fail(codeCancelled, "cancelled by user")
	}
	if perr != nil {
		return r.fail(codeWriteFailed, perr.Error())
	}
	if r.errMsg != "" {
		code := codeWriteFailed
		if r.errPhase == PhaseVerify {
			code = codeVerifyFailed
		}
		return r.fail(code, r.errMsg)
	}
	if (code == 126 || code == 127) && name == "pkexec" && cr.n == 0 {
		return r.fail(codeRightsDenied, "pkexec was dismissed or refused")
	}
	if werr != nil {
		return r.fail(codeWriteFailed, werr.Error())
	}
	if code != 0 {
		return r.fail(codeWriteFailed, fmt.Sprintf("imager exit code %d", code))
	}
	if !r.sawDone {
		return r.fail(codeWriteFailed, "imager ended without success")
	}

	host := r.p.req.Settings.Hostname + ".local"
	if err := transport.PinHostKey(r.w.KnownHosts, host, r.hk.Public); err != nil {
		return r.fail(codeWriteFailed, "pin host key: "+err.Error())
	}
	r.done(StepEject)
	r.w.update(func(v *JobView) {
		v.Target = &Target{Host: host, User: r.p.req.Settings.User, KeyPath: r.w.KeyPath}
	})
	return JobDone, nil
}

// onProgress maps one imager event to steps and percent.
func (r *jobRun) onProgress(p Progress) {
	switch p.Phase {
	case PhasePrepare:
		if r.active == StepRights {
			r.done(StepRights)
		}
	case PhaseWrite:
		r.phase = PhaseWrite
		if r.active != StepWrite {
			r.move(StepWrite)
		}
		r.progress(p.Percent)
	case PhaseVerify:
		r.phase = PhaseVerify
		if r.active != StepVerify {
			r.move(StepVerify)
		}
		r.progress(p.Percent)
		if p.Percent >= 100 {
			r.move(StepCustomize)
		}
	case PhaseDone:
		r.sawDone = true
		for _, id := range []StepID{StepRights, StepWrite, StepVerify, StepCustomize} {
			r.w.setStep(id, StepDone)
		}
		r.move(StepEject)
	case PhaseError:
		r.errMsg = p.Message
		if r.errMsg == "" {
			r.errMsg = "imager reported an error"
		}
		r.errPhase = r.phase
	}
}

// progress updates percent, bytes and the rate for write and verify.
func (r *jobRun) progress(pct int) {
	size := r.p.image.ExtractSize
	done := int64(pct) * size / 100
	now := time.Now()
	r.samples = append(r.samples, rateSample{at: now, done: done})
	for len(r.samples) > 2 && now.Sub(r.samples[0].at) > rateWindow {
		r.samples = r.samples[1:]
	}
	var rate int64
	if n := len(r.samples); n >= 2 {
		first, last := r.samples[0], r.samples[n-1]
		if dt := last.at.Sub(first.at).Seconds(); dt > 0 && last.done > first.done {
			rate = int64(float64(last.done-first.done) / dt)
		}
	}
	r.w.update(func(v *JobView) {
		v.Percent = pct
		v.BytesDone = done
		v.BytesTotal = size
		v.Rate = rate
	})
}

// finish stores the final state and frees the writer for the next job.
func (w *Writer) finish(state JobState, jobErr *JobError) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.view.State = state
	w.view.Error = jobErr
	w.view.Rate = 0
	if state != JobDone {
		w.view.Target = nil
	}
	w.busy = false
	w.cancel = nil
}

// countReader counts the bytes read, so that a silent failure can be told apart.
type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// logSink keeps the imager output lines in the view. \r and \n both end a line.
type logSink struct {
	w       *Writer
	partial []byte
}

func (s *logSink) Write(p []byte) (int, error) {
	for _, b := range p {
		if b == '\r' || b == '\n' {
			s.flush()
			continue
		}
		s.partial = append(s.partial, b)
	}
	return len(p), nil
}

// flush appends the partial line to the view and clears the ESC[0K codes.
func (s *logSink) flush() {
	line := strings.ReplaceAll(string(s.partial), "\x1b[0K", "")
	s.partial = s.partial[:0]
	if strings.TrimSpace(line) == "" {
		return
	}
	s.w.update(func(v *JobView) {
		v.Log = append(v.Log, line)
		if len(v.Log) > logLimit {
			v.Log = v.Log[len(v.Log)-logLimit:]
		}
	})
}

// View returns a snapshot of the current job.
func (w *Writer) View() JobView {
	w.mu.Lock()
	defer w.mu.Unlock()
	v := w.view
	if v.State == "" {
		v.State = JobIdle
	}
	v.Steps = append([]StepView(nil), w.view.Steps...)
	v.Log = append([]string(nil), w.view.Log...)
	if w.view.Target != nil {
		t := *w.view.Target
		v.Target = &t
	}
	return v
}

// Cancel stops the running job. It does nothing when no job runs.
func (w *Writer) Cancel() {
	w.mu.Lock()
	cancel := w.cancel
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
