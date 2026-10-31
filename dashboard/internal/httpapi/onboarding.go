package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/onboarding"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
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

// handleOnboarding serves /api/v1/onboarding and everything below it: the
// progress of the caller (GET), the setup steps of the installation (PUT step,
// POST setup/complete, admins only) and the progress of the caller's own
// account (PUT me). The middleware has already checked the session, the user
// comes from the request context. settingsStore and installed feed the setup
// items that are detected from the current configuration.
func handleOnboarding(manager *auth.Manager, store *onboarding.Store, settingsStore *settings.Store, installed map[string]bool) http.HandlerFunc {
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
			handleOnboardingGet(manager, store, settingsStore, installed, user)(w, r)
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

func handleOnboardingGet(manager *auth.Manager, store *onboarding.Store, settingsStore *settings.Store, installed map[string]bool, user auth.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		progress := store.Load()
		own := progress.Users[user.Username]
		seen, queue := own.Seen, own.Queue
		if seen == nil {
			seen = []string{}
		}
		if queue == nil {
			queue = []string{}
		}
		role := onboardingRoleOf(user)
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
			"queue": onboardingSessionQueue(role, progress.SetupDone),
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
// users never see it.
func onboardingSessionQueue(role string, setupDone bool) []string {
	if role == onboardingRoleAdmin && !setupDone {
		return []string{"setup", "explore"}
	}
	return []string{"explore"}
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
