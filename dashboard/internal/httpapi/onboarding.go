package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/onboarding"
)

const onboardingPathPrefix = "/api/v1/onboarding"

// Roles of the caller as the onboarding API reports them. A guest is never an
// admin, even when a guest role somehow carries user_management.
const (
	onboardingRoleAdmin = "admin"
	onboardingRoleGuest = "guest"
	onboardingRoleUser  = "user"
)

// handleOnboarding serves /api/v1/onboarding and everything below it: the
// progress of the caller (GET), the setup steps of the installation (PUT step,
// POST setup/complete, admins only) and the progress of the caller's own
// account (PUT me). The middleware has already checked the session, the user
// comes from the request context.
func handleOnboarding(manager *auth.Manager, store *onboarding.Store) http.HandlerFunc {
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
			handleOnboardingGet(store, user)(w, r)
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

func handleOnboardingGet(store *onboarding.Store, user auth.User) http.HandlerFunc {
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
		writeJSON(w, map[string]any{
			"role": onboardingRoleOf(user),
			"setup": map[string]any{
				"done":  progress.SetupDone,
				"steps": progress.SetupSteps,
			},
			"user": map[string]any{
				"seen":     seen,
				"welcomed": own.Welcomed,
				"run":      own.Run,
				"queue":    queue,
			},
		})
	}
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
		if request.ID == "" || (state != onboarding.StepDone && state != onboarding.StepLater) {
			writeErrorKey(w, http.StatusBadRequest, "onboarding_invalid", "error.onboarding_invalid", nil, "Die Angabe zur Einrichtungsführung ist ungültig")
			return
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
