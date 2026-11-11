package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sync"
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

	go w.run(jobCtx, p)
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

// run executes the job. Part A stub: it waits for cancellation.
func (w *Writer) run(ctx context.Context, p *plan) {
	<-ctx.Done()
	w.finish(JobCancelled, nil)
}

// finish stores the final state and frees the writer for the next job.
func (w *Writer) finish(state JobState, jobErr *JobError) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.view.State = state
	w.view.Error = jobErr
	w.busy = false
	w.cancel = nil
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
