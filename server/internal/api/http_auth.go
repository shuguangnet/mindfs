package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mindfs/server/auth"
)

var (
	errUnauthenticated   = errors.New("unauthenticated")
	errTooManyAttempts   = errors.New("too_many_attempts")
	errInvalidCreds      = errors.New("invalid_credentials")
	errAuthUnavailable   = errors.New("auth_unavailable")
	errAuthNotConfigured = errors.New("auth_password_not_configured")
)

// authExemptPaths are reachable without a session even when the gate is on.
// They cover the login flow plus the read-only bootstrap/status endpoints the
// frontend may probe before logging in.
var authExemptPaths = map[string]bool{
	"/health":                true,
	"/api/auth/status":       true,
	"/api/auth/login":        true,
	"/api/auth/logout":       true,
	"/api/relay/status":      true,
	"/api/e2ee/open":         true,
	"/api/replying-sessions": true,
	"/api/app/update":        true,
}

// IsAuthExemptPath reports whether path may be reached without logging in.
func IsAuthExemptPath(path string) bool {
	return authExemptPaths[path]
}

func isAuthGatedPath(path string) bool {
	return strings.HasPrefix(path, "/api/") || path == "/ws"
}

// AuthMiddleware enforces the optional login gate. When the gate is disabled it
// is a no-op. Static frontend assets are never gated so the login page can
// load; only /api/* and /ws are checked.
func (h *HTTPHandler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mgr := h.Auth
		if mgr == nil || !mgr.Enabled() {
			next.ServeHTTP(w, r)
			return
		}
		if !isAuthGatedPath(r.URL.Path) || IsAuthExemptPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if h.isLocalCLIRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		if isRelayedRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		if mgr.ValidateRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		respondError(w, http.StatusUnauthorized, errUnauthenticated)
	})
}

func (h *HTTPHandler) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	mgr := h.Auth
	if mgr == nil {
		respondJSON(w, http.StatusOK, map[string]any{
			"enabled":       false,
			"authenticated": true,
		})
		return
	}
	status := mgr.Status()
	authenticated := !status.Enabled || mgr.ValidateRequest(r)
	respondJSON(w, http.StatusOK, map[string]any{
		"enabled":       status.Enabled,
		"username":      status.Username,
		"has_password":  status.HasPassword,
		"authenticated": authenticated,
	})
}

type authLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Remember bool   `json:"remember"`
}

func (h *HTTPHandler) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	mgr := h.Auth
	if mgr == nil {
		respondError(w, http.StatusServiceUnavailable, errAuthUnavailable)
		return
	}
	if !mgr.Enabled() {
		respondJSON(w, http.StatusOK, map[string]any{"enabled": false, "authenticated": true})
		return
	}
	var req authLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest("invalid json body"))
		return
	}
	ip := auth.ClientIP(r)
	if ok, retry := mgr.AllowLogin(ip); !ok {
		seconds := int(retry.Seconds())
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		respondError(w, http.StatusTooManyRequests, errTooManyAttempts)
		return
	}
	if !mgr.VerifyPassword(req.Username, req.Password) {
		mgr.RecordFailure(ip)
		respondError(w, http.StatusUnauthorized, errInvalidCreds)
		return
	}
	mgr.ResetFailures(ip)
	token, maxAge, err := mgr.IssueSession(req.Username, req.Remember)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err)
		return
	}
	writeAuthCookie(w, r, token, maxAge)
	respondJSON(w, http.StatusOK, map[string]any{
		"enabled":       true,
		"authenticated": true,
		"username":      mgr.Status().Username,
	})
}

func (h *HTTPHandler) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	writeAuthCookie(w, r, "", -1)
	respondJSON(w, http.StatusOK, map[string]any{"authenticated": false})
}

type authSettingsRequest struct {
	Enabled         *bool  `json:"enabled"`
	Username        string `json:"username"`
	NewPassword     string `json:"new_password"`
	CurrentPassword string `json:"current_password"`
}

func (h *HTTPHandler) handleAuthSettingsGet(w http.ResponseWriter, r *http.Request) {
	mgr := h.Auth
	if mgr == nil {
		respondJSON(w, http.StatusOK, map[string]any{"enabled": false, "has_password": false})
		return
	}
	status := mgr.Status()
	respondJSON(w, http.StatusOK, map[string]any{
		"enabled":      status.Enabled,
		"username":     status.Username,
		"has_password": status.HasPassword,
	})
}

func (h *HTTPHandler) handleAuthSettingsPut(w http.ResponseWriter, r *http.Request) {
	mgr := h.Auth
	if mgr == nil {
		respondError(w, http.StatusServiceUnavailable, errAuthUnavailable)
		return
	}
	var req authSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest("invalid json body"))
		return
	}
	current := mgr.Status()
	username := strings.TrimSpace(req.Username)
	newPassword := strings.TrimSpace(req.NewPassword)

	if current.Enabled && newPassword != "" {
		if !mgr.VerifyPassword(current.Username, req.CurrentPassword) {
			respondError(w, http.StatusUnauthorized, errInvalidCreds)
			return
		}
	}

	if newPassword != "" {
		target := username
		if target == "" {
			target = current.Username
		}
		if err := mgr.SetCredentials(target, newPassword); err != nil {
			respondAuthSettingsError(w, err)
			return
		}
	} else if username != "" && username != current.Username && current.HasPassword {
		if err := mgr.SetUsername(username); err != nil {
			respondAuthSettingsError(w, err)
			return
		}
	}

	if req.Enabled != nil {
		if err := mgr.SetEnabled(*req.Enabled); err != nil {
			respondAuthSettingsError(w, err)
			return
		}
		if !*req.Enabled {
			// Disabling clears sessions (Q14-A); drop this browser's cookie too.
			writeAuthCookie(w, r, "", -1)
		}
	}

	status := mgr.Status()
	if status.Enabled {
		// Keep the current browser signed in after enabling or updating
		// credentials, so the caller does not immediately bounce to login.
		if token, maxAge, err := mgr.IssueSession(status.Username, false); err == nil {
			writeAuthCookie(w, r, token, maxAge)
		}
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"enabled":      status.Enabled,
		"username":     status.Username,
		"has_password": status.HasPassword,
	})
}

func respondAuthSettingsError(w http.ResponseWriter, err error) {
	if errors.Is(err, auth.ErrNotConfigured) {
		respondError(w, http.StatusBadRequest, errAuthNotConfigured)
		return
	}
	respondError(w, http.StatusBadRequest, err)
}

func writeAuthCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	secure := r != nil && r.TLS != nil
	cookie := &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   maxAge,
	}
	if maxAge < 0 {
		cookie.Expires = time.Unix(0, 0)
	}
	http.SetCookie(w, cookie)
}
