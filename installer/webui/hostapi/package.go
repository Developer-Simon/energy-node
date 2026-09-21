package hostapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// BundledInfo beschreibt das Bundle neben dem Programm.
type BundledInfo struct {
	Version string `json:"version"`
	Arch    string `json:"arch"`
}

// RepoInfo sagt, ob "Aus Repository bauen" angeboten werden kann. Reason ist
// leer, wenn Available wahr ist, sonst "OS_UNSUPPORTED" oder "TOOLS_MISSING".
type RepoInfo struct {
	Available bool   `json:"available"`
	Path      string `json:"path"`
	Reason    string `json:"reason,omitempty"`
}

// ResolvedInfo beschreibt das zuletzt vorbereitete Paket.
type ResolvedInfo struct {
	Kind   string `json:"kind"`
	Signed bool   `json:"signed"`
}

// PackageInfo ist der Teil von Description und Bootstrap, der die
// Paketauswahl ermoeglicht. Ein Wirt ohne Paketauswahl (Dashboard) laesst
// ihn nil.
type PackageInfo struct {
	Bundled  *BundledInfo  `json:"bundled"`
	Repo     RepoInfo      `json:"repo"`
	Resolved *ResolvedInfo `json:"resolved"`
}

// PackageSelection ist der Rumpf von PUT /api/package. Kind ist "bundled",
// "repo" oder "github"; Path gehoert nur zu "repo". Eine Paketdatei geht
// ueber POST /api/package/upload.
type PackageSelection struct {
	Kind string `json:"kind"`
	Path string `json:"path,omitempty"`
}

// PackageBackend ist die optionale Erweiterung eines Wirts, der das Paket
// waehlbar macht. Der Server prueft per Typzusicherung; ein Backend ohne sie
// (Dashboard) beantwortet die Routen mit 404.
type PackageBackend interface {
	SelectPackage(ctx context.Context, sel PackageSelection) error
	// UploadPackage nimmt die hochgeladene Paketdatei entgegen und waehlt
	// damit die Quelle "file".
	UploadPackage(ctx context.Context, name string, r io.Reader) error
}

// ModePrepare bereitet das Paket vor: aufloesen, auf den Node uebertragen,
// dort pruefen. Er laeuft ueber denselben Weg wie ein Lauf, damit Log,
// Abbruch und Filterung von Geheimnissen dieselben sind.
const ModePrepare RunMode = "prepare"

// maxPackageUpload begrenzt eine hochgeladene Paketdatei.
const maxPackageUpload = 2 << 30

func (s *Server) packageBackend(w http.ResponseWriter, r *http.Request) (PackageBackend, bool) {
	backend, ok := s.opts.Backend.(PackageBackend)
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", r.URL.Path)
	}
	return backend, ok
}

func (s *Server) handlePackage(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.packageBackend(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	var sel PackageSelection
	if err := json.NewDecoder(r.Body).Decode(&sel); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if err := backend.SelectPackage(r.Context(), sel); err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePackageUpload(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.packageBackend(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPackageUpload)
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
			return
		}
		if part.FormName() != "file" {
			continue
		}
		if err := backend.UploadPackage(r.Context(), part.FileName(), part); err != nil {
			writeBackendError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Feld file fehlt")
}
