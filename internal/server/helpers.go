package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/galq/blackflame/internal/auth"
	"github.com/galq/blackflame/internal/store"
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
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(session.CSRF)) != 1 {
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

// clientIP returns the address rate limiting and audit logging key on.
// X-Forwarded-For is only honored when the operator explicitly opts in via
// BLACKFLAME_TRUST_FORWARDED_FOR: without a configured trusted proxy the
// header is trivially spoofable and would defeat the login limiter. The
// rightmost entry is used because it is appended by the nearest trusted
// proxy, unlike client-supplied prefix entries.
func clientIP(r *http.Request) string {
	if os.Getenv("BLACKFLAME_TRUST_FORWARDED_FOR") != "" {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			parts := strings.Split(forwarded, ",")
			if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
				return last
			}
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

// reloadAfterMutation refreshes the config snapshot after a committed write.
// The request context is not used: it is canceled as soon as the client
// disconnects, and the already-committed mutation must still be reflected in
// the served config regardless of what happens to the connection.
func (s *Server) reloadAfterMutation(r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.app.Reload(ctx); err != nil {
		s.logger.Warn("reload after mutation", "error", err, "path", r.URL.Path)
	}
}

func (s *Server) handleStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidPassword):
		writeError(w, http.StatusUnauthorized, "invalid_password", "invalid password")
		return
	case errors.Is(err, auth.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts; try again later")
		return
	case errors.Is(err, auth.ErrAlreadyConfigured):
		writeError(w, http.StatusConflict, "already_configured", "password is already configured")
		return
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	case errors.Is(err, store.ErrLastBoard):
		writeError(w, http.StatusConflict, "last_board", err.Error())
		return
	case isUniqueViolation(err):
		writeError(w, http.StatusConflict, "conflict", "a record with the same unique value already exists")
		return
	}
	// Unknown failures are server faults: do not echo driver or filesystem
	// details to the client, but keep them in the logs.
	s.logger.Error("store request failed", "error", err)
	writeError(w, http.StatusInternalServerError, "request_failed", "request could not be completed")
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
