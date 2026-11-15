package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/galq/blackflame/internal/auth"
)

const sessionCookieName = "blackflame_session"

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func readJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body: "+err.Error())
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must contain a single JSON object")
		return false
	}
	return true
}

func (s *Server) sessionFromRequest(ctx context.Context, r *http.Request) *auth.Session {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	session, err := s.app.Auth.Authenticate(ctx, cookie.Value)
	if err != nil {
		return nil
	}
	return session
}

func (s *Server) authState(ctx context.Context, r *http.Request) (*auth.Session, bool, bool) {
	session := s.sessionFromRequest(ctx, r)
	configured, err := s.app.Auth.PasswordConfigured(ctx)
	if err != nil {
		s.logger.Warn("check password configuration", "error", err)
	}
	return session, session != nil, !configured
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) *auth.Session {
	session := s.sessionFromRequest(r.Context(), r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "edit mode is locked")
		return nil
	}
	return session
}

func (s *Server) requireCSRF(w http.ResponseWriter, r *http.Request, session *auth.Session) bool {
	if session == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "edit mode is locked")
		return false
	}
	token := r.Header.Get("X-CSRF-Token")
	if token == "" || token != session.CSRF {
		writeError(w, http.StatusForbidden, "invalid_csrf", "missing or invalid CSRF token")
		return false
	}
	return true
}

func (s *Server) requireAdminMutation(w http.ResponseWriter, r *http.Request) *auth.Session {
	session := s.requireAdmin(w, r)
	if session == nil {
		return nil
	}
	if !s.requireCSRF(w, r, session) {
		return nil
	}
	return session
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, session *auth.Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.ID,
		Path:     s.cookiePath(),
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.SessionTTL().Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     s.cookiePath(),
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (s *Server) cookiePath() string {
	if s.basePath == "" {
		return "/"
	}
	return s.basePath
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		if first := strings.TrimSpace(strings.Split(forwarded, ",")[0]); first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func parseID(r *http.Request, name string) (int64, error) {
	raw := r.PathValue(name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid id")
	}
	return id, nil
}

func writeNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func handleStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidPassword):
		writeError(w, http.StatusUnauthorized, "invalid_password", "invalid password")
	case errors.Is(err, auth.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts; try again later")
	case errors.Is(err, auth.ErrAlreadyConfigured):
		writeError(w, http.StatusConflict, "already_configured", "password is already configured")
	default:
		writeError(w, http.StatusBadRequest, "request_failed", err.Error())
	}
}
