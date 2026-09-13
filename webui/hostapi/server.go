package hostapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strings"

	webui "github.com/Developer-Simon/energy-node-webui"
	"github.com/Developer-Simon/energy-node-webui/i18n"
)

// Options konfiguriert den Server. Backend und Catalogs sind Pflicht.
type Options struct {
	Backend  Backend
	Catalogs *i18n.Set
	// Language ist die aktive Sprache beim ersten Laden. Leer heisst: die
	// Rueckfallsprache.
	Language string
	// LanguageFixed schaltet den Umschalter in der Oberflaeche ab. Der
	// Dashboard-Wirt setzt das (E8): derselbe Katalog, derselbe Code, nur eine
	// feste statt einer sichtbaren Wahl.
	LanguageFixed bool
	// Token ist das Einmal-Token aus der geoeffneten URL. Leer schaltet die
	// Pruefung ab - fuer einen Wirt, der selbst authentifiziert.
	Token string
	// BasePath ist der Pfadpraefix, unter dem der Server haengt, ohne
	// abschliessenden Schraegstrich.
	BasePath string
}

// Server ist Schicht 2.
type Server struct {
	opts Options
	bus  *Bus
	tmpl *template.Template
	mux  *http.ServeMux
	run  runState
	conn connection
}

// Bootstrap ist die Antwort von GET /api/bootstrap: alles, was die Oberflaeche
// braucht, bevor sie das erste Mal zeichnet.
type Bootstrap struct {
	Host            HostKind `json:"host"`
	EntryPoints     []string `json:"entry_points"`
	NeedsConnection bool     `json:"needs_connection"`
	BundleVersion   string   `json:"bundle_version"`
	AssetVersion    string   `json:"asset_version"`
	Language        string   `json:"language"`
	LanguageFixed   bool     `json:"language_fixed"`
	Languages       []string `json:"languages"`
	BasePath        string   `json:"base_path"`
}

// New baut den Server und registriert alle Routen.
func New(opts Options) (*Server, error) {
	if opts.Backend == nil {
		return nil, errors.New("hostapi: Options.Backend fehlt")
	}
	if opts.Catalogs == nil {
		return nil, errors.New("hostapi: Options.Catalogs fehlt")
	}
	if opts.Language == "" || !opts.Catalogs.Has(opts.Language) {
		opts.Language = i18n.Fallback
	}
	opts.BasePath = strings.TrimSuffix(opts.BasePath, "/")

	tmpl, err := template.ParseFS(webui.Templates(), "*.html")
	if err != nil {
		return nil, err
	}

	s := &Server{opts: opts, bus: NewBus(5000), tmpl: tmpl, mux: http.NewServeMux()}
	s.routes()
	return s, nil
}

// Bus liefert den Ereignis-Bus. Ein Wirt, der einen Lauf ausserhalb von
// POST /api/run anstoesst (Plan D: die Updater-Unit laeuft weiter, waehrend
// das Dashboard neu startet), speist ihn direkt.
func (s *Server) Bus() *Bus { return s.bus }

// Handler liefert den fertigen HTTP-Handler samt Token-Pruefung.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := s.trimBase(r.URL.Path)
		if !strings.HasPrefix(path, "/assets/") && !s.tokenOK(r) {
			writeError(w, http.StatusUnauthorized, "TOKEN_INVALID", "")
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = path
		s.mux.ServeHTTP(w, r2)
	})
}

func (s *Server) routes() {
	s.mux.Handle("/assets/", webui.StaticHandler("/assets/"))
	s.mux.HandleFunc("/", s.handleShell)
	s.mux.HandleFunc("/api/bootstrap", s.handleBootstrap)
	s.mux.HandleFunc("/api/catalog/", s.handleCatalog)
	s.mux.HandleFunc("/api/connect", s.handleConnect)
	s.mux.HandleFunc("/api/keypair", s.handleKeypair)
}

func (s *Server) trimBase(path string) string {
	if s.opts.BasePath == "" {
		return path
	}
	if trimmed := strings.TrimPrefix(path, s.opts.BasePath); trimmed != path {
		if trimmed == "" {
			return "/"
		}
		return trimmed
	}
	return path
}

func (s *Server) tokenOK(r *http.Request) bool {
	if s.opts.Token == "" {
		return true
	}
	given := r.Header.Get("X-Installer-Token")
	if given == "" {
		given = r.URL.Query().Get("token")
	}
	return subtle.ConstantTimeCompare([]byte(given), []byte(s.opts.Token)) == 1
}

func (s *Server) handleShell(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", r.URL.Path)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	title, _ := s.opts.Catalogs.Lookup(s.opts.Language, "app.title")
	data := map[string]any{
		"Title":        title,
		"Language":     s.opts.Language,
		"Token":        s.opts.Token,
		"BasePath":     s.opts.BasePath,
		"AssetVersion": webui.AssetVersion(),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := s.tmpl.ExecuteTemplate(w, "index", data); err != nil {
		// Der Kopf steht schon; mehr als abbrechen geht hier nicht.
		return
	}
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	description := s.opts.Backend.Describe()
	writeJSON(w, http.StatusOK, Bootstrap{
		Host:            description.Host,
		EntryPoints:     description.EntryPoints,
		NeedsConnection: description.NeedsConnection,
		BundleVersion:   description.BundleVersion,
		AssetVersion:    webui.AssetVersion(),
		Language:        s.opts.Language,
		LanguageFixed:   s.opts.LanguageFixed,
		Languages:       s.opts.Catalogs.Languages(),
		BasePath:        s.opts.BasePath,
	})
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	lang := strings.TrimPrefix(r.URL.Path, "/api/catalog/")
	// Eine unbekannte Sprache bekommt den englischen Katalog, keinen Fehler:
	// eine Oberflaeche ohne Texte waere unbedienbar, eine auf Englisch nicht.
	writeJSON(w, http.StatusOK, s.opts.Catalogs.Merged(lang))
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeError schreibt die einheitliche Fehlerform. code ist sprachneutral,
// detail ist unuebersetzter Zusatz und darf leer sein.
func writeError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, map[string]string{"error": code, "detail": detail})
}

// writeBackendError bildet einen Backend-Fehler auf die Fehlerform ab.
func writeBackendError(w http.ResponseWriter, err error) {
	var typed *Error
	if errors.As(err, &typed) {
		status := typed.Status
		if status == 0 {
			status = http.StatusInternalServerError
		}
		writeError(w, status, typed.Code, typed.Detail)
		return
	}
	writeError(w, http.StatusInternalServerError, "BACKEND_ERROR", err.Error())
}
