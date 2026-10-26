package httpapi

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/companion"
	"github.com/Developer-Simon/energy-node-dashboard/internal/companionpair"
)

// companionPrefix is the path prefix of the companion routes. They are served
// on the companion port only (see CompanionGate); the main port answers 404.
const companionPrefix = "/api/v1/companion/"

// companionPairPath is the one route that passes the gate without a token.
const companionPairPath = companionPrefix + "pair"

// companionContextKey marks requests that passed CompanionGate.
type companionContextKey struct{}

var errInvalidNonce = errors.New("nonce must be 64 hex characters")

// CompanionGate wraps the handler of the companion port (8081). Every request
// needs the bearer token of the pairing, except the pairing itself. The
// request that reaches next is a clone without the Authorization header and
// carries the companion marker; it counts as insecure when X-Forwarded-Proto
// names a scheme other than https.
func CompanionGate(manager *auth.Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !(r.Method == http.MethodPost && r.URL.Path == companionPairPath) {
			token, ok := bearerToken(r)
			if !ok || manager == nil || !manager.CompanionTokenValid(token) {
				writeErrorKey(w, http.StatusUnauthorized, "companion_unauthorized", "error.companion_unauthorized", nil, "Der Companion ist nicht gekoppelt.")
				return
			}
		}
		clone := r.Clone(context.WithValue(r.Context(), companionContextKey{}, true))
		clone.Header.Del("Authorization")
		if proto := firstForwardedProto(r); proto != "" && !strings.EqualFold(proto, "https") {
			clone.TLS = nil
		}
		next.ServeHTTP(w, clone)
	})
}

// isCompanionRequest reports whether the request passed CompanionGate.
func isCompanionRequest(r *http.Request) bool {
	marked, _ := r.Context().Value(companionContextKey{}).(bool)
	return marked
}

// bearerToken returns the token of an "Authorization: Bearer" header.
func bearerToken(r *http.Request) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	token = strings.TrimSpace(token)
	return token, ok && token != ""
}

// firstForwardedProto returns the first value of X-Forwarded-Proto, trimmed.
func firstForwardedProto(r *http.Request) string {
	return strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
}

// handleCompanion serves the routes under /api/v1/companion/. Requests that
// did not pass CompanionGate get 404; without a hub the routes answer 501.
func handleCompanion(manager *auth.Manager, hub *companion.Hub, fingerprint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isCompanionRequest(r) {
			http.NotFound(w, r)
			return
		}
		if hub == nil {
			http.Error(w, "companion is not available", http.StatusNotImplemented)
			return
		}
		route := strings.TrimPrefix(r.URL.Path, companionPrefix)
		switch {
		case route == "pair":
			if r.Method != http.MethodPost {
				methodNotAllowed(w, http.MethodPost)
				return
			}
			companionPair(manager, hub, fingerprint, w, r)
		case route == "stream":
			if r.Method != http.MethodGet {
				methodNotAllowed(w, http.MethodGet)
				return
			}
			hub.ServeStream(w, r, func() { _ = manager.CompanionSeen() })
		case route == "session":
			if r.Method != http.MethodPost {
				methodNotAllowed(w, http.MethodPost)
				return
			}
			companionSession(manager, w, r)
		case route == "user-changed":
			if r.Method != http.MethodPost {
				methodNotAllowed(w, http.MethodPost)
				return
			}
			companionUserChanged(manager, w, r)
		case strings.HasPrefix(route, "verify/"):
			if r.Method != http.MethodPost {
				methodNotAllowed(w, http.MethodPost)
				return
			}
			companionVerify(hub, strings.TrimPrefix(route, "verify/"), w, r)
		default:
			http.NotFound(w, r)
		}
	}
}

// companionPair couples the companion. The proof of the companion must match
// the pairing code, the certificate fingerprint and the nonce.
func companionPair(manager *auth.Manager, hub *companion.Hub, fingerprint string, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Nonce string `json:"nonce"`
		Proof string `json:"proof"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
		return
	}
	if !validCompanionNonce(body.Nonce) {
		writeErrorDetail(w, http.StatusBadRequest, "invalid_body", errInvalidNonce)
		return
	}
	token, code, err := manager.Pair(func(code string) bool {
		return hmac.Equal([]byte(companionpair.ClientProof(code, fingerprint, body.Nonce)), []byte(body.Proof))
	})
	if errors.Is(err, auth.ErrPairingInvalid) {
		writeErrorKey(w, http.StatusForbidden, "pairing_invalid", "error.pairing_invalid", nil, "Der Kopplungscode ist ungültig oder abgelaufen.")
		return
	}
	if err != nil {
		writeErrorKey(w, http.StatusInternalServerError, "companion_failed", "error.companion_failed", nil, "Die Anfrage des Companion konnte nicht gespeichert werden.")
		return
	}
	// A new pairing replaces the old one, so an open stream of the old
	// companion must end.
	hub.Close()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]string{
		"token": token,
		"proof": companionpair.ServerProof(code, fingerprint, body.Nonce, token),
	})
}

// companionVerify hands the answer to a pending password check.
func companionVerify(hub *companion.Hub, id string, w http.ResponseWriter, r *http.Request) {
	var result companion.VerifyResult
	if err := decodeBody(r, &result); err != nil {
		writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
		return
	}
	if !hub.Resolve(id, result) {
		writeErrorKey(w, http.StatusNotFound, "verify_unknown", "error.verify_unknown", nil, "Diese Prüfung ist nicht mehr offen.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// companionSession starts a session for a Home Assistant user.
func companionSession(manager *auth.Manager, w http.ResponseWriter, r *http.Request) {
	identity, ok := decodeCompanionIdentity(w, r)
	if !ok {
		return
	}
	session, err := manager.CompanionSession(identity)
	if errors.Is(err, auth.ErrAccountDisabled) {
		writeErrorKey(w, http.StatusForbidden, "account_disabled", "error.account_disabled", nil, "Dieses Konto ist gesperrt.")
		return
	}
	if errors.Is(err, auth.ErrNotPaired) {
		writeErrorKey(w, http.StatusUnauthorized, "companion_unauthorized", "error.companion_unauthorized", nil, "Der Companion ist nicht gekoppelt.")
		return
	}
	if err != nil {
		writeErrorKey(w, http.StatusInternalServerError, "companion_failed", "error.companion_failed", nil, "Die Anfrage des Companion konnte nicht gespeichert werden.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{
		"token":      session.Token,
		"csrf_token": session.CSRFToken,
		"expires_at": session.ExpiresAt,
	})
}

// companionUserChanged applies a change of a Home Assistant user. An inactive
// or removed user loses all sessions.
func companionUserChanged(manager *auth.Manager, w http.ResponseWriter, r *http.Request) {
	var body struct {
		companionIdentityBody
		Active  bool `json:"active"`
		Removed bool `json:"removed"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
		return
	}
	if body.HAUserID == "" || body.Username == "" {
		writeErrorDetail(w, http.StatusBadRequest, "invalid_body", errMissingHAUser)
		return
	}
	if err := manager.HAUserChanged(body.identity(), body.Active, body.Removed); err != nil {
		writeErrorKey(w, http.StatusInternalServerError, "companion_failed", "error.companion_failed", nil, "Die Anfrage des Companion konnte nicht gespeichert werden.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var errMissingHAUser = errors.New("ha_user_id and username are required")

// companionIdentityBody is the identity part of the companion bodies.
type companionIdentityBody struct {
	HAUserID string `json:"ha_user_id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	IsAdmin  bool   `json:"is_admin"`
}

func (b companionIdentityBody) identity() auth.HAIdentity {
	return auth.HAIdentity{UserID: b.HAUserID, Username: b.Username, Name: b.Name, Admin: b.IsAdmin}
}

// decodeCompanionIdentity reads the identity of a session request. It answers
// 400 itself when the request is not usable.
func decodeCompanionIdentity(w http.ResponseWriter, r *http.Request) (auth.HAIdentity, bool) {
	var body companionIdentityBody
	if err := decodeBody(r, &body); err != nil {
		writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
		return auth.HAIdentity{}, false
	}
	if body.HAUserID == "" || body.Username == "" {
		writeErrorDetail(w, http.StatusBadRequest, "invalid_body", errMissingHAUser)
		return auth.HAIdentity{}, false
	}
	return body.identity(), true
}

// validCompanionNonce reports whether nonce is 64 hex characters.
func validCompanionNonce(nonce string) bool {
	if len(nonce) != 64 {
		return false
	}
	_, err := hex.DecodeString(nonce)
	return err == nil
}
