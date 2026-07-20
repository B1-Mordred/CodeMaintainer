package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	maintainerauth "github.com/local-code-maintainer/appliance/internal/auth"
)

const sessionCookieName = "maintainer_session"

type principalContextKey struct{}

func (s *Server) authenticationMiddleware(next http.Handler) http.Handler {
	if s.auth == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicRoute(r) {
			if isStateChanging(r.Method) && !csrfRequestOriginAllowed(r) {
				writeError(w, http.StatusForbidden, "request_origin_rejected", "the cross-site authentication request was rejected")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeError(w, http.StatusUnauthorized, "authentication_required", "an authenticated session is required")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		principal, err := s.auth.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			s.clearSessionCookie(w)
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeError(w, http.StatusUnauthorized, "authentication_required", "the session is invalid or expired")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if isStateChanging(r.Method) {
			if !csrfRequestOriginAllowed(r) || !s.auth.VerifyCSRF(principal, r.Header.Get("X-CSRF-Token")) {
				writeError(w, http.StatusForbidden, "csrf_rejected", "the CSRF token or request origin is invalid")
				return
			}
		}
		permission := routePermission(r)
		if !principal.User.Role.Allows(permission) {
			writeError(w, http.StatusForbidden, "permission_denied", "the authenticated role cannot perform this action")
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isPublicRoute(r *http.Request) bool {
	if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || !strings.HasPrefix(r.URL.Path, "/api/") {
		return true
	}
	switch r.URL.Path {
	case "/api/v1/auth/status", "/api/v1/auth/bootstrap", "/api/v1/auth/login":
		return true
	default:
		return false
	}
}

func isStateChanging(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func csrfRequestOriginAllowed(r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func routePermission(r *http.Request) maintainerauth.Permission {
	path := r.URL.Path
	if r.Method == http.MethodGet {
		if path == "/api/v1/audit" {
			return maintainerauth.PermissionAdminister
		}
		return maintainerauth.PermissionRead
	}
	if strings.Contains(path, "/approve-publication") || strings.Contains(path, "/findings/") {
		return maintainerauth.PermissionReview
	}
	if strings.Contains(path, "/memory/") {
		if strings.Contains(path, "/actions/promote") || strings.Contains(path, "/actions/correct") || strings.Contains(path, "/actions/invalidate") {
			return maintainerauth.PermissionReview
		}
		return maintainerauth.PermissionAdminister
	}
	if strings.HasPrefix(path, "/api/v1/projects") || strings.HasPrefix(path, "/api/v1/config") || strings.HasPrefix(path, "/api/v1/admin") {
		return maintainerauth.PermissionAdminister
	}
	if strings.HasPrefix(path, "/api/v1/auth/") {
		return maintainerauth.PermissionRead
	}
	return maintainerauth.PermissionOperate
}

func principalFromRequest(r *http.Request) (maintainerauth.Principal, bool) {
	principal, ok := r.Context().Value(principalContextKey{}).(maintainerauth.Principal)
	return principal, ok
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeJSON(w, http.StatusOK, map[string]any{"bootstrapped": true, "authentication_enabled": false})
		return
	}
	complete, err := s.auth.BootstrapStatus(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bootstrapped": complete, "authentication_enabled": true})
}

func (s *Server) authBootstrap(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotImplemented, "authentication_disabled", "authentication is not configured")
		return
	}
	var request struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	result, err := s.auth.Bootstrap(r.Context(), maintainerauth.Credentials{
		Username: request.Username, DisplayName: request.DisplayName, Password: request.Password,
	}, remoteAddress(r))
	if err != nil {
		s.authenticationError(w, err)
		return
	}
	s.setSessionCookie(w, result.Token, result.Principal.ExpiresAt)
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotImplemented, "authentication_disabled", "authentication is not configured")
		return
	}
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	result, err := s.auth.Login(r.Context(), maintainerauth.Credentials{Username: request.Username, Password: request.Password}, remoteAddress(r))
	if err != nil {
		s.authenticationError(w, err)
		return
	}
	s.setSessionCookie(w, result.Token, result.Principal.ExpiresAt)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) authSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required", "an authenticated session is required")
		return
	}
	csrfToken, principal, err := s.auth.RotateCSRF(r.Context(), principal)
	if err != nil {
		s.authenticationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"principal": principal, "csrf_token": csrfToken})
}

func (s *Server) authReauthenticate(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required", "an authenticated session is required")
		return
	}
	var request struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	principal, err := s.auth.Reauthenticate(r.Context(), principal, request.Password, remoteAddress(r))
	if err != nil {
		s.authenticationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"principal": principal})
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required", "an authenticated session is required")
		return
	}
	if err := s.auth.Logout(r.Context(), principal, remoteAddress(r)); err != nil {
		s.authenticationError(w, err)
		return
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authenticationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, maintainerauth.ErrAlreadyBootstrapped):
		writeError(w, http.StatusConflict, "bootstrap_complete", "administrator bootstrap is already complete")
	case errors.Is(err, maintainerauth.ErrRateLimited):
		w.Header().Set("Retry-After", "300")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many authentication attempts")
	case errors.Is(err, maintainerauth.ErrInvalidCredentials), errors.Is(err, maintainerauth.ErrSessionExpired), errors.Is(err, maintainerauth.ErrNotFound):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "the credentials or session are invalid")
	case errors.Is(err, maintainerauth.ErrInvalidInput):
		writeError(w, http.StatusUnprocessableEntity, "invalid_authentication_input", "username or password does not meet the required policy")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "authentication could not be completed")
	}
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true,
		Secure: s.secureCookie, SameSite: http.SameSiteLaxMode,
		Expires: expires, MaxAge: max(1, int(time.Until(expires).Seconds())),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: s.secureCookie, SameSite: http.SameSiteLaxMode,
		Expires: time.Unix(1, 0), MaxAge: -1,
	})
}

func remoteAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	if len(r.RemoteAddr) <= 128 {
		return r.RemoteAddr
	}
	return ""
}
