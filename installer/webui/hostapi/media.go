package hostapi

import (
	"context"
	"encoding/json"
	"net/http"
)

// Fehlercodes der Medienvorbereitung. Die Oberflaeche uebersetzt sie ueber
// den Katalog (error.<CODE>).
const (
	MediaErrUnsupported      = "MEDIA_UNSUPPORTED"
	MediaErrImagerMissing    = "MEDIA_IMAGER_MISSING"
	MediaErrOSListUnavail    = "MEDIA_OSLIST_UNAVAILABLE"
	MediaErrDownloadFailed   = "MEDIA_DOWNLOAD_FAILED"
	MediaErrChecksumMismatch = "MEDIA_CHECKSUM_MISMATCH"
	MediaErrRightsDenied     = "MEDIA_RIGHTS_DENIED"
	MediaErrDriveGone        = "MEDIA_DRIVE_GONE"
	MediaErrWriteFailed      = "MEDIA_WRITE_FAILED"
	MediaErrVerifyFailed     = "MEDIA_VERIFY_FAILED"
	MediaErrTooSmall         = "MEDIA_TOO_SMALL"
	MediaErrBusy             = "MEDIA_BUSY"
	MediaErrConfirmMismatch  = "MEDIA_CONFIRM_MISMATCH"
	MediaErrInvalidSettings  = "MEDIA_INVALID_SETTINGS"
	MediaErrCancelled        = "MEDIA_CANCELLED"
)

// maxMediaBody begrenzt den Rumpf von POST /api/media/write.
const maxMediaBody = 64 << 10

// MediaLink ist ein Verweis auf den Imager: kind ist "discover" (der
// App-Store) oder "website".
type MediaLink struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

// MediaImagerStatus beschreibt den gefundenen Raspberry Pi Imager.
type MediaImagerStatus struct {
	Found   bool   `json:"found"`
	Kind    string `json:"kind"`
	Version string `json:"version"`
	TooOld  bool   `json:"too_old"`
}

// MediaStatus sagt, ob der Rechner Medien vorbereiten kann.
type MediaStatus struct {
	Supported bool              `json:"supported"`
	Platform  string            `json:"platform"` // runtime.GOOS
	Imager    MediaImagerStatus `json:"imager"`
	Links     []MediaLink       `json:"links"`
}

// MediaImage ist ein Betriebssystem-Image aus der Liste.
type MediaImage struct {
	Name         string `json:"name"`
	Arch         string `json:"arch"`
	DownloadSize int64  `json:"download_size"`
	ExtractSize  int64  `json:"extract_size"`
	Cached       bool   `json:"cached"`
	ReleaseDate  string `json:"release_date"`
}

// MediaDevice ist ein unterstuetztes Geraet. Image ist nil, wenn die
// OS-Liste nicht erreichbar ist und nichts gespeichert wurde.
type MediaDevice struct {
	ID           string      `json:"id"`
	Group        string      `json:"group"`
	Experimental bool        `json:"experimental"`
	Image        *MediaImage `json:"image"`
}

// MediaDefaults sind die Vorgaben des Formulars.
type MediaDefaults struct {
	Hostname      string            `json:"hostname"`
	User          string            `json:"user"`
	Timezone      string            `json:"timezone"`
	Keyboard      string            `json:"keyboard"`
	WifiCountry   string            `json:"wifi_country"`
	WifiSSID      string            `json:"wifi_ssid"`
	WifiFreqMHz   int               `json:"wifi_freq_mhz"`  // 0 = unbekannt
	ZoneCountries map[string]string `json:"zone_countries"` // Zeitzone -> Laendercode
	Timezones     []string          `json:"timezones"`
	Keyboards     []string          `json:"keyboards"`
}

// MediaCatalog ist die Geraeteliste mit Images und die Vorgaben.
type MediaCatalog struct {
	Devices  []MediaDevice `json:"devices"`
	Offline  bool          `json:"offline"`
	Defaults MediaDefaults `json:"defaults"`
}

// MediaPartition ist eine Partition eines Laufwerks.
type MediaPartition struct {
	Name   string `json:"name"`
	Label  string `json:"label"`
	FSType string `json:"fstype"`
	Used   int64  `json:"used"`
}

// MediaDrive ist ein Laufwerk, das beschrieben werden koennte.
type MediaDrive struct {
	Path       string           `json:"path"`
	Model      string           `json:"model"`
	Size       int64            `json:"size"`
	Transport  string           `json:"transport"`
	Removable  bool             `json:"removable"`
	Partitions []MediaPartition `json:"partitions"`
}

// MediaNameCheck prueft einen Rechnernamen.
type MediaNameCheck struct {
	Valid      bool   `json:"valid"`
	Taken      bool   `json:"taken"`
	Suggestion string `json:"suggestion"`
}

// MediaWifiCheck ist der Rumpf von POST /api/media/wifi-check.
type MediaWifiCheck struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
}

// MediaWifiCheckResult sagt, ob dieser Rechner das Netz mit genau diesem
// Passwort kennt: "match", "mismatch" oder "unknown".
type MediaWifiCheckResult struct {
	Result string `json:"result"`
}

// MediaWifi sind die WLAN-Zugangsdaten fuers Geraet.
type MediaWifi struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
	Country  string `json:"country"`
}

// MediaSettings sind die Einstellungen, die das Geraet beim ersten Start hat.
type MediaSettings struct {
	Hostname string     `json:"hostname"`
	User     string     `json:"user"`
	Password string     `json:"password"`
	Timezone string     `json:"timezone"`
	Keyboard string     `json:"keyboard"`
	Wifi     *MediaWifi `json:"wifi"`
}

// MediaWriteRequest ist der Rumpf von POST /api/media/write.
type MediaWriteRequest struct {
	Device   string        `json:"device"`
	Drive    string        `json:"drive"`
	Confirm  string        `json:"confirm"`
	Settings MediaSettings `json:"settings"`
}

// MediaStep ist ein Schritt des Schreibauftrags.
type MediaStep struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

// MediaTarget sagt, wo das fertige Geraet zu erreichen ist.
type MediaTarget struct {
	Host    string `json:"host"`
	User    string `json:"user"`
	KeyPath string `json:"key_path"`
}

// MediaJob ist der Stand des Schreibauftrags, wie ihn die Oberflaeche abfragt.
type MediaJob struct {
	State      string       `json:"state"`
	Steps      []MediaStep  `json:"steps"`
	Percent    int          `json:"percent"`
	BytesDone  int64        `json:"bytes_done"`
	BytesTotal int64        `json:"bytes_total"`
	Rate       int64        `json:"rate"`
	Error      *ErrorView   `json:"error"`
	Log        []string     `json:"log"`
	Target     *MediaTarget `json:"target"`
}

// ErrorView ist ein Fehler mit Code und unuebersetztem Zusatz.
type ErrorView struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// MediaReachability sagt, ob der fertige Node antwortet und ob sein
// Schluessel zum Installer passt.
type MediaReachability struct {
	Reachable  bool `json:"reachable"`
	KeyMatches bool `json:"key_matches"`
}

// MediaProvider ist optional. Ein Backend ohne sie beantwortet /api/media/*
// mit 501 NOT_SUPPORTED.
type MediaProvider interface {
	MediaStatus(ctx context.Context) (MediaStatus, error)
	MediaCatalog(ctx context.Context) (MediaCatalog, error)
	MediaDrives(ctx context.Context, internal bool) ([]MediaDrive, error)
	MediaCheckName(ctx context.Context, name string) (MediaNameCheck, error)
	MediaCheckWifi(ctx context.Context, req MediaWifiCheck) (MediaWifiCheckResult, error)
	MediaWrite(ctx context.Context, req MediaWriteRequest) error
	MediaJob(ctx context.Context) (MediaJob, error)
	MediaCancel(ctx context.Context) error
	MediaReachable(ctx context.Context, host string) (MediaReachability, error)
}

// mediaProvider prueft Methode und Faehigkeit. Ist eine von beiden falsch,
// ist die Antwort schon geschrieben und ok false.
func (s *Server) mediaProvider(w http.ResponseWriter, r *http.Request, method string) (MediaProvider, bool) {
	if r.Method != method {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return nil, false
	}
	provider, ok := s.opts.Backend.(MediaProvider)
	if !ok {
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "")
		return nil, false
	}
	return provider, true
}

func (s *Server) handleMediaStatus(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.mediaProvider(w, r, http.MethodGet)
	if !ok {
		return
	}
	view, err := provider.MediaStatus(r.Context())
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleMediaCatalog(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.mediaProvider(w, r, http.MethodGet)
	if !ok {
		return
	}
	view, err := provider.MediaCatalog(r.Context())
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// handleMediaDrives: ohne internal=1 nur Wechselmedien.
func (s *Server) handleMediaDrives(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.mediaProvider(w, r, http.MethodGet)
	if !ok {
		return
	}
	internal := r.URL.Query().Get("internal") == "1"
	drives, err := provider.MediaDrives(r.Context(), internal)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	if drives == nil {
		drives = []MediaDrive{}
	}
	writeJSON(w, http.StatusOK, drives)
}

func (s *Server) handleMediaName(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.mediaProvider(w, r, http.MethodGet)
	if !ok {
		return
	}
	view, err := provider.MediaCheckName(r.Context(), r.URL.Query().Get("name"))
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// handleMediaWifi prueft das WLAN-Passwort gegen die Profile des Rechners.
// POST, damit das Passwort nicht in der Adresse steht.
func (s *Server) handleMediaWifi(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.mediaProvider(w, r, http.MethodPost)
	if !ok {
		return
	}
	var req MediaWifiCheck
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMediaBody)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	view, err := provider.MediaCheckWifi(r.Context(), req)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// handleMediaWrite startet den Schreibauftrag. 202, weil er im Hintergrund
// laeuft; den Stand fragt die Oberflaeche ueber /api/media/job ab.
func (s *Server) handleMediaWrite(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.mediaProvider(w, r, http.MethodPost)
	if !ok {
		return
	}
	var req MediaWriteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMediaBody)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if err := provider.MediaWrite(r.Context(), req); err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, struct{}{})
}

func (s *Server) handleMediaJob(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.mediaProvider(w, r, http.MethodGet)
	if !ok {
		return
	}
	view, err := provider.MediaJob(r.Context())
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleMediaCancel(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.mediaProvider(w, r, http.MethodPost)
	if !ok {
		return
	}
	if err := provider.MediaCancel(r.Context()); err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) handleMediaReachable(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.mediaProvider(w, r, http.MethodGet)
	if !ok {
		return
	}
	view, err := provider.MediaReachable(r.Context(), r.URL.Query().Get("host"))
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
