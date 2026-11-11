package host

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/Developer-Simon/energy-node-installer/internal/media"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Die Vorgaben des Formulars fuer Rechnername und Benutzer.
const (
	defaultMediaHostname = "energy-node"
	defaultMediaUser     = "node"
)

// Die Quelle des Imagers und die Adressen zum Herunterladen.
const (
	mediaDiscoverURL = "appstream://" + media.FlatpakID
	mediaWebsiteURL  = "https://www.raspberrypi.com/software/"
)

var _ hostapi.MediaProvider = (*Host)(nil)

// imagerEnv ruft die echten Programme auf.
var imagerEnv = media.Env{
	LookPath: exec.LookPath,
	Output: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	},
}

// mediaSupported liefert MEDIA_UNSUPPORTED, wenn der Rechner keine Medien
// vorbereiten kann oder kein Medienverzeichnis gesetzt ist.
func (h *Host) mediaSupported() error {
	if runtime.GOOS != "linux" || h.cfg.MediaDir == "" {
		return apiError(hostapi.MediaErrUnsupported, http.StatusNotImplemented, runtime.GOOS)
	}
	return nil
}

// writer legt den Schreiber beim ersten Aufruf an.
func (h *Host) writer() (*media.Writer, error) {
	h.mediaMu.Lock()
	defer h.mediaMu.Unlock()
	if h.mediaW != nil {
		return h.mediaW, nil
	}
	keyPath, _, err := GenerateEd25519(h.cfg.IdentityDir)
	if err != nil {
		return nil, err
	}
	h.mediaW = &media.Writer{
		Cache: &media.Cache{Dir: h.cfg.MediaDir},
		Imager: func(ctx context.Context) (media.Imager, bool) {
			return media.FindImager(ctx, imagerEnv)
		},
		Drives:     media.ListDrives,
		Run:        execRunner,
		KnownHosts: h.cfg.KnownHostsPath,
		KeyPath:    keyPath,
		WorkDir:    filepath.Join(h.cfg.MediaDir, "jobs"),
	}
	return h.mediaW, nil
}

// execRunner startet name mit args. stdout und stderr laufen in eine Pipe;
// Cancel des Kontexts beendet den Prozess.
func execRunner(ctx context.Context, name string, args ...string) (io.ReadCloser, func() (int, error), error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return nil, nil, err
	}
	pw.Close()
	wait := func() (int, error) {
		// Den Rest lesen, damit der Prozess nie an einer vollen Pipe haengt.
		_, _ = io.Copy(io.Discard, pr)
		err := cmd.Wait()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		if err != nil {
			return -1, err
		}
		return 0, nil
	}
	return pr, wait, nil
}

func (h *Host) MediaStatus(ctx context.Context) (hostapi.MediaStatus, error) {
	status := hostapi.MediaStatus{Platform: runtime.GOOS, Links: []hostapi.MediaLink{}}
	if runtime.GOOS != "linux" {
		return status, nil
	}
	status.Supported = true
	status.Links = []hostapi.MediaLink{
		{Kind: "discover", URL: mediaDiscoverURL},
		{Kind: "website", URL: mediaWebsiteURL},
	}
	im, ok := media.FindImager(ctx, imagerEnv)
	status.Imager = hostapi.MediaImagerStatus{Found: ok}
	if ok {
		status.Imager.Kind = string(im.Kind)
		status.Imager.Version = im.Version
		status.Imager.TooOld = im.TooOld()
	}
	return status, nil
}

func (h *Host) MediaCatalog(ctx context.Context) (hostapi.MediaCatalog, error) {
	if err := h.mediaSupported(); err != nil {
		return hostapi.MediaCatalog{}, err
	}
	cache := &media.Cache{Dir: h.cfg.MediaDir}
	list, offline, err := cache.OSList(ctx)
	if err != nil {
		// Ohne Liste und ohne gespeicherte Kopie bleibt nur die Geraeteliste ohne Images.
		list, offline = nil, true
	}
	view := hostapi.MediaCatalog{Offline: offline, Defaults: mediaDefaults(media.HostDefaults())}
	for _, d := range media.Devices() {
		device := hostapi.MediaDevice{ID: d.ID, Group: d.Group, Experimental: d.Experimental}
		if list != nil {
			if img, ok := list.Find(d.ImageName, d.Tag); ok {
				device.Image = &hostapi.MediaImage{
					Name:         img.Name,
					Arch:         d.Arch,
					DownloadSize: img.DownloadSize,
					ExtractSize:  img.ExtractSize,
					Cached:       cache.Cached(img),
					ReleaseDate:  img.ReleaseDate,
				}
			}
		}
		view.Devices = append(view.Devices, device)
	}
	return view, nil
}

func mediaDefaults(d media.Defaults) hostapi.MediaDefaults {
	return hostapi.MediaDefaults{
		Hostname:    defaultMediaHostname,
		User:        defaultMediaUser,
		Timezone:    d.Timezone,
		Keyboard:    d.Keyboard,
		WifiCountry: d.WifiCountry,
		WifiSSID:    d.WifiSSID,
		Timezones:   d.Timezones,
		Keyboards:   d.Keyboards,
	}
}

func (h *Host) MediaDrives(ctx context.Context, internal bool) ([]hostapi.MediaDrive, error) {
	if err := h.mediaSupported(); err != nil {
		return nil, err
	}
	drives, err := media.ListDrives(ctx)
	if err != nil {
		return nil, err
	}
	out := []hostapi.MediaDrive{}
	for _, d := range media.Visible(drives, internal) {
		parts := []hostapi.MediaPartition{}
		for _, p := range d.Partitions {
			parts = append(parts, hostapi.MediaPartition{Name: p.Name, Label: p.Label, FSType: p.FSType, Used: p.Used})
		}
		out = append(out, hostapi.MediaDrive{
			Path:       d.Path,
			Model:      d.Model,
			Size:       d.Size,
			Transport:  d.Transport,
			Removable:  d.Removable,
			Partitions: parts,
		})
	}
	return out, nil
}

func (h *Host) MediaCheckName(ctx context.Context, name string) (hostapi.MediaNameCheck, error) {
	if err := h.mediaSupported(); err != nil {
		return hostapi.MediaNameCheck{}, err
	}
	c := media.CheckName(ctx, net.DefaultResolver, name)
	return hostapi.MediaNameCheck{Valid: c.Valid, Taken: c.Taken, Suggestion: c.Suggestion}, nil
}

func (h *Host) MediaWrite(ctx context.Context, req hostapi.MediaWriteRequest) error {
	if err := h.mediaSupported(); err != nil {
		return err
	}
	w, err := h.writer()
	if err != nil {
		return err
	}
	_, authorizedKey, err := GenerateEd25519(h.cfg.IdentityDir)
	if err != nil {
		return err
	}
	mreq := media.WriteRequest{
		Device:  req.Device,
		Drive:   req.Drive,
		Confirm: req.Confirm,
		Settings: media.Settings{
			Hostname:      req.Settings.Hostname,
			User:          req.Settings.User,
			AuthorizedKey: authorizedKey,
			Timezone:      req.Settings.Timezone,
			Keyboard:      req.Settings.Keyboard,
		},
		Password: req.Settings.Password,
	}
	if wifi := req.Settings.Wifi; wifi != nil {
		mreq.Settings.Wifi = &media.Wifi{SSID: wifi.SSID, Password: wifi.Password, Country: wifi.Country}
	}
	start := w.Start
	if h.mediaStart != nil {
		start = h.mediaStart
	}
	return mediaError(start(ctx, mreq))
}

func (h *Host) MediaJob(ctx context.Context) (hostapi.MediaJob, error) {
	if err := h.mediaSupported(); err != nil {
		return hostapi.MediaJob{}, err
	}
	w, err := h.writer()
	if err != nil {
		return hostapi.MediaJob{}, err
	}
	v := w.View()
	job := hostapi.MediaJob{
		State:      string(v.State),
		Steps:      []hostapi.MediaStep{},
		Percent:    v.Percent,
		BytesDone:  v.BytesDone,
		BytesTotal: v.BytesTotal,
		Rate:       v.Rate,
		Log:        append([]string{}, v.Log...),
	}
	for _, s := range v.Steps {
		job.Steps = append(job.Steps, hostapi.MediaStep{ID: string(s.ID), State: string(s.State)})
	}
	if v.Error != nil {
		job.Error = &hostapi.ErrorView{Code: v.Error.Code, Detail: v.Error.Detail}
	}
	if v.Target != nil {
		job.Target = &hostapi.MediaTarget{Host: v.Target.Host, User: v.Target.User, KeyPath: v.Target.KeyPath}
	}
	return job, nil
}

func (h *Host) MediaCancel(ctx context.Context) error {
	if err := h.mediaSupported(); err != nil {
		return err
	}
	w, err := h.writer()
	if err != nil {
		return err
	}
	w.Cancel()
	return nil
}

// MediaReachable prueft, ob der Node unter host antwortet. Der erwartete
// Schluessel ist der, den der Installer fuer diesen Host in known_hosts gepinnt hat.
func (h *Host) MediaReachable(ctx context.Context, host string) (hostapi.MediaReachability, error) {
	if err := h.mediaSupported(); err != nil {
		return hostapi.MediaReachability{}, err
	}
	if host == "" || strings.ContainsRune(host, '/') || strings.ContainsFunc(host, unicode.IsSpace) {
		return hostapi.MediaReachability{}, apiError("BAD_REQUEST", http.StatusBadRequest, host)
	}
	r := media.Probe(ctx, host, pinnedKey(h.cfg.KnownHostsPath, host))
	return hostapi.MediaReachability{Reachable: r.Reachable, KeyMatches: r.KeyMatches}, nil
}

// pinnedKey liest den Schluessel, den known_hosts fuer host fuehrt. nil heisst: keiner.
func pinnedKey(path, host string) ssh.PublicKey {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	want := knownhosts.Normalize(host)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.HasPrefix(fields[0], "@") {
			fields = fields[1:]
		}
		if len(fields) < 3 || strings.HasPrefix(fields[0], "|1|") {
			continue
		}
		for _, h := range strings.Split(fields[0], ",") {
			if knownhosts.Normalize(h) != want {
				continue
			}
			key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(fields[1] + " " + fields[2]))
			if err != nil {
				return nil
			}
			return key
		}
	}
	return nil
}

// mediaError bildet die Fehler des Medienpakets auf Codes und Statuscodes ab.
func mediaError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, media.ErrBusy):
		return apiError(hostapi.MediaErrBusy, http.StatusConflict, err.Error())
	case errors.Is(err, media.ErrDriveGone):
		return apiError(hostapi.MediaErrDriveGone, http.StatusConflict, err.Error())
	case errors.Is(err, media.ErrConfirm):
		return apiError(hostapi.MediaErrConfirmMismatch, http.StatusBadRequest, err.Error())
	case errors.Is(err, media.ErrSettings):
		return apiError(hostapi.MediaErrInvalidSettings, http.StatusBadRequest, err.Error())
	case errors.Is(err, media.ErrImagerMissing):
		return apiError(hostapi.MediaErrImagerMissing, http.StatusConflict, err.Error())
	case errors.Is(err, media.ErrTooSmall):
		return apiError(hostapi.MediaErrTooSmall, http.StatusBadRequest, err.Error())
	}
	return err
}

func apiError(code string, status int, detail string) *hostapi.Error {
	return &hostapi.Error{Code: code, Status: status, Detail: detail}
}
