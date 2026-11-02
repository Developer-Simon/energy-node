package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/onboarding"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/versions"
)

const onboardingPathPrefix = "/api/v1/onboarding"

// Input limits. They only protect the size of onboarding.json against callers
// with any session, guests included. They are no access control.
const (
	onboardingMaxIDBytes      = 64
	onboardingMaxSeenAdd      = 100
	onboardingMaxSeen         = 500
	onboardingMaxQueue        = 5
	onboardingMaxRunModeBytes = 32
	onboardingMaxRunIndex     = 1000
	onboardingMaxSetupSteps   = 200
)

// validOnboardingID reports whether id is a usable identifier: non-empty and
// at most onboardingMaxIDBytes long.
func validOnboardingID(id string) bool {
	return id != "" && len(id) <= onboardingMaxIDBytes
}

// writeOnboardingInvalid answers a request that breaks an input limit or names
// an invalid identifier.
func writeOnboardingInvalid(w http.ResponseWriter) {
	writeErrorKey(w, http.StatusBadRequest, "onboarding_invalid", "error.onboarding_invalid", nil, "Die Angabe zur Einrichtungsführung ist ungültig")
}

// Roles of the caller as the onboarding API reports them. A guest is never an
// admin, even when a guest role somehow carries user_management.
const (
	onboardingRoleAdmin = "admin"
	onboardingRoleGuest = "guest"
	onboardingRoleUser  = "user"
)

// onboardingItem is one setup step as GET /api/v1/onboarding reports it. State
// is empty while the step is still open.
type onboardingItem struct {
	ID        string `json:"id"`
	Area      string `json:"area"`
	Installer bool   `json:"installer"`
	State     string `json:"state"`
}

// onboardingInstalled resolves the installed services for the step catalog.
// A missing key counts as installed, as it does for the dashboard tabs.
func onboardingInstalled(services map[string]bool) map[string]bool {
	installed := map[string]bool{"tailscale": true}
	for key, value := range services {
		installed[key] = value
	}
	return installed
}

// onboardingExploreMinors is how many minor releases "Neu erkunden" lists per
// component: the installed one and the two before it.
const onboardingExploreMinors = 3

// onboardingExploreItem is one highlight of the last releases for "Neu
// erkunden". Version is the dashboard release the item is shown under, Minor
// its major.minor. Component is the component the highlight comes from
// ("dashboard" for dashboard entries), ComponentLabel its changelog label and
// ComponentVersion the release it came from. Tour holds only the steps the
// caller has the rights for and is null when none is left, the highlight then
// counts as text only.
type onboardingExploreItem struct {
	ID               string                     `json:"id"`
	Version          string                     `json:"version"`
	Minor            string                     `json:"minor"`
	Component        string                     `json:"component"`
	ComponentLabel   string                     `json:"component_label"`
	ComponentVersion string                     `json:"component_version,omitempty"`
	PR               int                        `json:"pr,omitempty"`
	Text             string                     `json:"text"`
	I18n             map[string]string          `json:"i18n,omitempty"`
	Details          *versions.HighlightDetails `json:"details,omitempty"`
	Tour             *onboardingTour            `json:"tour"`
}

// onboardingTour is the tour of a highlight as the caller may walk it. Stale
// is set when a step points at a data-tour target the web UI no longer has,
// so the tour would find nothing on the page.
type onboardingTour struct {
	Steps []versions.TourStep `json:"steps"`
	Stale bool                `json:"stale,omitempty"`
}

// onboardingExplore lists the highlights of the last onboardingExploreMinors
// dashboard minor releases, each ID once: the dashboard releases newest first,
// inside a release the dashboard entries first, then the other components.
// targets are the data-tour targets of the web UI, nil or empty marks no tour
// as stale. A missing or unreadable changelog gives an empty list and false.
func onboardingExplore(stateDir string, user auth.User, targets map[string]bool) (string, []onboardingExploreItem, bool) {
	items := []onboardingExploreItem{}
	view, err := versions.Releases(stateDir, onboardingExploreMinors)
	if err != nil {
		return "", items, false
	}
	known := map[string]bool{}
	for _, release := range view.Releases {
		for _, entry := range release.Entries {
			if entry.ID == "" || known[entry.ID] {
				continue
			}
			known[entry.ID] = true
			items = append(items, onboardingExploreItem{
				ID:               entry.ID,
				Version:          release.Version,
				Minor:            versions.MinorOf(release.Version),
				Component:        entry.Component,
				ComponentLabel:   entry.ComponentLabel,
				ComponentVersion: entry.ComponentVersion,
				PR:               entry.PR,
				Text:             entry.Text,
				I18n:             entry.I18n,
				Details:          entry.Details,
				Tour:             onboardingTourFor(user, entry.Tour, targets),
			})
		}
	}
	return view.To, items, true
}

// onboardingTourFor keeps the steps whose permission the user holds. An
// exemption or a tour without any allowed step gives nil. The tour is stale
// when one of the kept steps names a target outside targets; without targets
// nothing is stale.
func onboardingTourFor(user auth.User, tour *versions.HighlightTour, targets map[string]bool) *onboardingTour {
	if tour == nil {
		return nil
	}
	steps := []versions.TourStep{}
	for _, step := range tour.Steps {
		if step.Need == "" || auth.HasRole(user, step.Need) {
			steps = append(steps, step)
		}
	}
	if len(steps) == 0 {
		return nil
	}
	stale := false
	if len(targets) > 0 {
		for _, step := range steps {
			if !targets[step.Target] {
				stale = true
			}
		}
	}
	return &onboardingTour{Steps: steps, Stale: stale}
}

// onboardingSeed is the first progress of an account without an entry.
// Accounts created after the onboarding started get the welcome and every
// current highlight as seen. On a fresh install every account starts with the
// highlights of older minors seen (olderIDs), the current minor stays open to
// explore, and everyone but admins (they run the setup) gets the
// welcome. Older accounts of an updated install start with nothing seen, so
// "Neu erkunden" begins with this update.
func onboardingSeed(user auth.User, role string, fresh bool, ids, olderIDs []string) func(time.Time) onboarding.UserProgress {
	return func(since time.Time) onboarding.UserProgress {
		switch {
		case user.CreatedAt.After(since):
			return onboarding.UserProgress{Seen: ids, Welcome: true, Baseline: true}
		case fresh:
			return onboarding.UserProgress{Seen: olderIDs, Welcome: role != onboardingRoleAdmin, Baseline: true}
		default:
			return onboarding.UserProgress{Seen: []string{}}
		}
	}
}

// handleOnboarding serves /api/v1/onboarding and everything below it: the
// progress of the caller (GET), the setup steps of the installation (PUT step,
// POST setup/complete, admins only) and the progress of the caller's own
// account (PUT me). The middleware has already checked the session, the user
// comes from the request context. settingsStore and installed feed the setup
// items that are detected from the current configuration, stateDir holds the
// changelog for "Neu erkunden", tourTargets the data-tour targets of the web UI
// that mark a tour as stale.
func handleOnboarding(manager *auth.Manager, store *onboarding.Store, settingsStore *settings.Store, installed map[string]bool, stateDir string, tourTargets map[string]bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
			return
		}
		rest := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, onboardingPathPrefix), "/")
		switch rest {
		case "":
			if r.Method != http.MethodGet {
				methodNotAllowed(w, http.MethodGet)
				return
			}
			handleOnboardingGet(manager, store, settingsStore, installed, stateDir, tourTargets, user)(w, r)
		case "step":
			if r.Method != http.MethodPut {
				methodNotAllowed(w, http.MethodPut)
				return
			}
			if !requireOnboardingAdmin(w, r, manager, user) {
				return
			}
			handleOnboardingStep(store)(w, r)
		case "setup/complete":
			if r.Method != http.MethodPost {
				methodNotAllowed(w, http.MethodPost)
				return
			}
			if !requireOnboardingAdmin(w, r, manager, user) {
				return
			}
			if err := store.CompleteSetup(); err != nil {
				writeError(w, http.StatusInternalServerError, "onboarding_save_failed", "Die Einrichtungsführung konnte nicht gespeichert werden")
				return
			}
			writeJSON(w, map[string]string{"status": "completed"})
		case "me":
			if r.Method != http.MethodPut {
				methodNotAllowed(w, http.MethodPut)
				return
			}
			if !requireCSRF(w, r, manager) {
				return
			}
			handleOnboardingMe(store, user)(w, r)
		default:
			http.NotFound(w, r)
		}
	}
}

// onboardingRoleOf maps the account to the role the frontend shows.
func onboardingRoleOf(user auth.User) string {
	switch {
	case user.Guest:
		return onboardingRoleGuest
	case auth.HasRole(user, auth.RoleUserManagement):
		return onboardingRoleAdmin
	default:
		return onboardingRoleUser
	}
}

// requireOnboardingAdmin checks the CSRF token first, as the user management
// does, and then the admin role.
func requireOnboardingAdmin(w http.ResponseWriter, r *http.Request, manager *auth.Manager, user auth.User) bool {
	if !requireCSRF(w, r, manager) {
		return false
	}
	if onboardingRoleOf(user) != onboardingRoleAdmin {
		writeErrorKey(w, http.StatusForbidden, "onboarding_forbidden", "error.onboarding_forbidden", nil, "Diese Änderung der Einrichtungsführung ist nur für Administratoren möglich")
		return false
	}
	return true
}

func handleOnboardingGet(manager *auth.Manager, store *onboarding.Store, settingsStore *settings.Store, installed map[string]bool, stateDir string, tourTargets map[string]bool, user auth.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := onboardingRoleOf(user)
		currentVersion, exploreItems, readable := onboardingExplore(stateDir, user, tourTargets)
		ids := make([]string, 0, len(exploreItems))
		olderIDs := []string{}
		for _, item := range exploreItems {
			ids = append(ids, item.ID)
			// The items come newest first, the first one names the current minor.
			if item.Minor != exploreItems[0].Minor {
				olderIDs = append(olderIDs, item.ID)
			}
		}
		var progress onboarding.Progress
		var own onboarding.UserProgress
		if user.Guest {
			// Guests come and go with their session, so their start is never
			// stored. Every guest gets the welcome and nothing to explore.
			progress = store.Load()
			own = progress.Users[user.Username]
			own.Seen, own.Welcome, own.Baseline = ids, true, true
		} else {
			seed := onboardingSeed(user, role, versions.FreshInstall(stateDir), ids, olderIDs)
			if readable {
				progress = store.EnsureUser(user.Username, time.Now().UTC(), seed)
			} else {
				// Without the changelog the seed would store the baseline with an
				// empty list for good. The answer is computed here, nothing is written.
				progress = store.Preview(user.Username, time.Now().UTC(), seed)
			}
			own = progress.Users[user.Username]
		}
		seen, queue := own.Seen, own.Queue
		if seen == nil {
			seen = []string{}
		}
		if queue == nil {
			queue = []string{}
		}
		states := onboarding.Merge(onboardingDetected(settingsStore, manager), progress.SetupSteps)
		catalog := onboarding.Catalog(installed)
		items := make([]onboardingItem, 0, len(catalog))
		for _, step := range catalog {
			items = append(items, onboardingItem{
				ID:        step.ID,
				Area:      step.Area,
				Installer: step.Installer,
				State:     string(states[step.ID]),
			})
		}
		writeJSON(w, map[string]any{
			"role": role,
			"setup": map[string]any{
				"done":  progress.SetupDone,
				"steps": progress.SetupSteps,
				"items": items,
			},
			"explore": map[string]any{
				"current_version": currentVersion,
				"items":           exploreItems,
				"baseline_seen":   own.Baseline,
			},
			"queue": onboardingSessionQueue(role, progress.SetupDone, own.Welcome && !own.Welcomed),
			"user": map[string]any{
				"seen":     seen,
				"welcomed": own.Welcomed,
				"run":      own.Run,
				"queue":    queue,
			},
		})
	}
}

// onboardingSessionQueue lists the guides this session should run next. Only
// an admin gets the setup while the installation is not set up yet. Guests and
// users never see it. A pending welcome replaces explore, everything counts as
// seen for whoever gets it.
func onboardingSessionQueue(role string, setupDone, welcome bool) []string {
	tour := "explore"
	if welcome {
		tour = "welcome"
	}
	if role == onboardingRoleAdmin && !setupDone {
		return []string{"setup", tour}
	}
	return []string{tour}
}

// onboardingDetected reads the current settings and accounts for the steps
// that are detected rather than marked. Missing settings count as defaults.
func onboardingDetected(settingsStore *settings.Store, manager *auth.Manager) map[string]onboarding.StepState {
	set := settings.Default()
	var energy settings.EnergyConfig
	if settingsStore != nil {
		if value, err := settingsStore.LoadSettings(); err == nil {
			set = value
		}
		if value, err := settingsStore.LoadEnergy(); err == nil {
			energy = value
		}
	}
	var users []auth.User
	if manager != nil {
		users = manager.Users()
	}
	return onboarding.Detect(set, energy, users)
}

func handleOnboardingStep(store *onboarding.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID    string `json:"id"`
			State string `json:"state"`
		}
		if err := decodeBody(r, &request); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
			return
		}
		state := onboarding.StepState(request.State)
		if !validOnboardingID(request.ID) || (state != onboarding.StepDone && state != onboarding.StepLater) {
			writeOnboardingInvalid(w)
			return
		}
		if progress := store.Load(); len(progress.SetupSteps) >= onboardingMaxSetupSteps {
			if _, known := progress.SetupSteps[request.ID]; !known {
				writeOnboardingInvalid(w)
				return
			}
		}
		if err := store.SetStep(request.ID, state); err != nil {
			writeError(w, http.StatusInternalServerError, "onboarding_save_failed", "Die Einrichtungsführung konnte nicht gespeichert werden")
			return
		}
		writeJSON(w, map[string]string{"status": "saved"})
	}
}

// handleOnboardingMe changes only the progress of the caller. seen_add is
// added to the seen list, run, queue and welcomed replace their value when
// they are present in the body. A run of null ends the run.
func handleOnboardingMe(store *onboarding.Store, user auth.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			SeenAdd  []string        `json:"seen_add"`
			Run      json.RawMessage `json:"run"`
			Queue    *[]string       `json:"queue"`
			Welcomed *bool           `json:"welcomed"`
		}
		if err := decodeBody(r, &request); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
			return
		}
		runGiven := len(request.Run) > 0
		var run *onboarding.Run
		if runGiven && !bytes.Equal(bytes.TrimSpace(request.Run), []byte("null")) {
			run = &onboarding.Run{}
			if err := json.Unmarshal(request.Run, run); err != nil {
				writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
				return
			}
			if len(run.Mode) > onboardingMaxRunModeBytes || len(run.From) > onboardingMaxIDBytes || run.Index < 0 || run.Index > onboardingMaxRunIndex {
				writeOnboardingInvalid(w)
				return
			}
		}
		if len(request.SeenAdd) > onboardingMaxSeenAdd {
			writeOnboardingInvalid(w)
			return
		}
		for _, id := range request.SeenAdd {
			if !validOnboardingID(id) {
				writeOnboardingInvalid(w)
				return
			}
		}
		if request.Queue != nil {
			if len(*request.Queue) > onboardingMaxQueue {
				writeOnboardingInvalid(w)
				return
			}
			for _, id := range *request.Queue {
				if !validOnboardingID(id) {
					writeOnboardingInvalid(w)
					return
				}
			}
		}
		if user.Guest {
			// Gäste speichern nichts, ihr Stand endet mit der Sitzung.
			writeJSON(w, map[string]string{"status": "saved"})
			return
		}
		// Check the resulting seen list before anything is written.
		if len(request.SeenAdd) > 0 {
			current := store.Load().Users[user.Username].Seen
			added := map[string]bool{}
			for _, id := range request.SeenAdd {
				if !slices.Contains(current, id) {
					added[id] = true
				}
			}
			if len(current)+len(added) > onboardingMaxSeen {
				writeOnboardingInvalid(w)
				return
			}
		}
		err := store.UpdateUser(user.Username, func(own *onboarding.UserProgress) {
			for _, id := range request.SeenAdd {
				if !slices.Contains(own.Seen, id) {
					own.Seen = append(own.Seen, id)
				}
			}
			if runGiven {
				own.Run = run
			}
			if request.Queue != nil {
				own.Queue = *request.Queue
			}
			if request.Welcomed != nil {
				own.Welcomed = *request.Welcomed
			}
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "onboarding_save_failed", "Die Einrichtungsführung konnte nicht gespeichert werden")
			return
		}
		writeJSON(w, map[string]string{"status": "saved"})
	}
}
