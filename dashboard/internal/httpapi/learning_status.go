package httpapi

import (
	"log"
	"net/http"
	"runtime"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// LearningDeps wires GET /api/v1/learning/status and POST /api/v1/learning/reset.
// Every field is a function, so httpapi does not depend on the services behind
// them. A nil LearningDeps or a nil Settings answers 501. Nil for any other
// field reads as "nothing learned" in the status, and the reset answers 501 without Reset.
type LearningDeps struct {
	Settings         func() (settings.Settings, error)
	State            func() learnrec.State
	Usage            func() autolearn.Usage
	ChainSuggestInfo func() (since int64, devices int)
	ChainLearnInfo   func() (since int64, devices int)
	SetupDone        func() bool
	// Rules reads the automation rule document. Its site location gives
	// location_set in the status. Nil or an unreadable document reads as no location.
	Rules func() ([]byte, error)
	Reset func() error
}

// learningStatusBody is the answer of GET /api/v1/learning/status.
type learningStatusBody struct {
	Fresh        bool                 `json:"fresh"`
	SetupDone    bool                 `json:"setup_done"`
	Level        string               `json:"level"`
	LevelSetting string               `json:"level_setting"`
	Features     learningFeaturesBody `json:"features"`
	Usage        autolearn.Usage      `json:"usage"`
	Acknowledged []string             `json:"acknowledged"`
	New          []string             `json:"new"`
	LocationSet  bool                 `json:"location_set"`
}

type learningFeaturesBody struct {
	ChainSuggestions learningFeatureBody `json:"chain_suggestions"`
	ChainLearning    learningFeatureBody `json:"chain_learning"`
	AutoPatterns     learningFeatureBody `json:"auto_patterns"`
}

type learningFeatureBody struct {
	On      bool  `json:"on"`
	Since   int64 `json:"since"`
	Devices int   `json:"devices"`
}

// handleLearningStatus serves GET /api/v1/learning/status. The route is gated by
// view_automations in the route table.
func handleLearningStatus(deps *LearningDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if deps == nil || deps.Settings == nil {
			writeErrorKey(w, http.StatusNotImplemented, "learning_unavailable", "error.learning_unavailable", nil, "Die Lernfunktionen sind nicht verfügbar")
			return
		}
		value, err := deps.Settings()
		if err != nil {
			writeErrorDetail(w, http.StatusInternalServerError, "settings_invalid", err)
			return
		}
		writeJSON(w, learningStatus(value, deps, runtime.NumCPU()))
	}
}

// learningStatus builds the status body from the settings and the injected
// state. A feature is new when it is switched on and not acknowledged yet.
func learningStatus(value settings.Settings, deps *LearningDeps, cores int) learningStatusBody {
	setting := value.LearningLevel
	if setting == "" {
		setting = settings.LearningLevelAuto
	}
	acknowledged := append([]string{}, value.LearningAcknowledged...)

	var usage autolearn.Usage
	if deps.Usage != nil {
		usage = deps.Usage()
	}
	var chainSuggest, chainLearn learningFeatureBody
	chainSuggest.On = !value.ChainSuggestionsDisabled
	chainLearn.On = !value.ChainLearningDisabled
	if deps.ChainSuggestInfo != nil {
		chainSuggest.Since, chainSuggest.Devices = deps.ChainSuggestInfo()
	}
	if deps.ChainLearnInfo != nil {
		chainLearn.Since, chainLearn.Devices = deps.ChainLearnInfo()
	}
	patterns := learningFeatureBody{
		On:      !value.AutomationPatternsDisabled,
		Since:   usage.Since,
		Devices: usage.Devices,
	}

	features := map[string]bool{
		"chain_suggestions": chainSuggest.On,
		"chain_learning":    chainLearn.On,
		"auto_patterns":     patterns.On,
	}
	news := []string{}
	for _, key := range settings.LearningFeatures() {
		if features[key] && !learningAcknowledged(acknowledged, key) {
			news = append(news, key)
		}
	}

	fresh := false
	var remembered learnrec.Level
	if deps.State != nil {
		st := deps.State()
		fresh = st.Fresh
		remembered = st.AutoLevel
	}
	setupDone := deps.SetupDone != nil && deps.SetupDone()
	locationSet := ruleLocationSet(deps.Rules)

	return learningStatusBody{
		Fresh:        fresh,
		SetupDone:    setupDone,
		Level:        string(learnrec.ResolveLevel(setting, cores, remembered)),
		LevelSetting: setting,
		Features: learningFeaturesBody{
			ChainSuggestions: chainSuggest,
			ChainLearning:    chainLearn,
			AutoPatterns:     patterns,
		},
		Usage:        usage,
		Acknowledged: acknowledged,
		New:          news,
		LocationSet:  locationSet,
	}
}

// ruleLocationSet reports whether the automation rule document has a site
// location. It parses the document the way the learning engine does, so both
// see the same location.
func ruleLocationSet(read func() ([]byte, error)) bool {
	if read == nil {
		return false
	}
	raw, err := read()
	if err != nil {
		return false
	}
	doc, err := autolearn.ParseDoc(raw)
	if err != nil {
		return false
	}
	return doc.Location != nil
}

// learningAcknowledged reports whether key is in the acknowledged list.
func learningAcknowledged(acknowledged []string, key string) bool {
	for _, v := range acknowledged {
		if v == key {
			return true
		}
	}
	return false
}

// handleLearningReset serves POST /api/v1/learning/reset. It needs edit_setup and
// a CSRF token, like the energy writes, and answers {"ok":true}.
func handleLearningReset(manager *auth.Manager, deps *LearningDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if deps == nil || deps.Reset == nil {
			writeErrorKey(w, http.StatusNotImplemented, "learning_unavailable", "error.learning_unavailable", nil, "Die Lernfunktionen sind nicht verfügbar")
			return
		}
		if !requireLearningMutation(w, r, manager) {
			return
		}
		if err := deps.Reset(); err != nil {
			log.Printf("httpapi: learning reset: %v", err)
			writeErrorKey(w, http.StatusInternalServerError, "learning_reset_failed", "error.learning_reset_failed", nil, "Das Gelernte konnte nicht gelöscht werden.")
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}
}

// requireLearningMutation checks the login, edit_setup and the CSRF token. A nil
// manager means no accounts are configured and skips the checks, as in requireEnergyMutation.
func requireLearningMutation(w http.ResponseWriter, r *http.Request, manager *auth.Manager) bool {
	if manager == nil {
		return true
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
		return false
	}
	if !auth.HasRole(user, auth.RoleEditSetup) {
		writeErrorKey(w, http.StatusForbidden, "learning_forbidden", "error.learning_forbidden", nil, "Zum Zurücksetzen der Lernfunktionen fehlt die Berechtigung")
		return false
	}
	cookie, err := r.Cookie(sessionCookieName(isSecureRequest(r)))
	if err != nil || !manager.ValidateCSRF(cookie.Value, r.Header.Get("X-CSRF-Token")) {
		writeError(w, http.StatusForbidden, "csrf_failed", "Sicherheitsprüfung fehlgeschlagen")
		return false
	}
	return true
}
