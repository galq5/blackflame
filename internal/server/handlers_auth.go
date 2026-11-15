package server

import (
	"errors"
	"net/http"

	"github.com/galq/blackflame/internal/auth"
)

type credentials struct {
	Password string `json:"password"`
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	session, authenticated, setupRequired := s.authState(r.Context(), r)
	response := map[string]any{
		"authenticated": authenticated,
		"setupRequired": setupRequired,
	}
	if session != nil {
		response["csrf"] = session.CSRF
		response["role"] = session.Role
		response["expiresAt"] = session.ExpiresAt
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	configured, err := s.app.Auth.PasswordConfigured(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "auth_unavailable", err.Error())
		return
	}
	if configured {
		writeError(w, http.StatusConflict, "already_configured", "password is already configured")
		return
	}
	var payload credentials
	if !readJSON(w, r, &payload) {
		return
	}
	if len(payload.Password) < 8 {
		writeError(w, http.StatusBadRequest, "weak_password", "password must be at least 8 characters")
		return
	}
	if err := s.app.Auth.SetPassword(r.Context(), payload.Password); err != nil {
		handleStoreError(w, err)
		return
	}
	session, err := s.app.Auth.Login(r.Context(), payload.Password, clientIP(r))
	if err != nil {
		handleStoreError(w, err)
		return
	}
	s.setSessionCookie(w, r, session)
	writeJSON(w, http.StatusCreated, map[string]any{
		"authenticated": true,
		"csrf":          session.CSRF,
		"role":          session.Role,
		"expiresAt":     session.ExpiresAt,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	configured, err := s.app.Auth.PasswordConfigured(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "auth_unavailable", err.Error())
		return
	}
	if !configured {
		writeError(w, http.StatusConflict, "setup_required", "set an edit password before logging in")
		return
	}
	var payload credentials
	if !readJSON(w, r, &payload) {
		return
	}
	session, err := s.app.Auth.Login(r.Context(), payload.Password, clientIP(r))
	if err != nil {
		if errors.Is(err, auth.ErrInvalidPassword) {
			writeError(w, http.StatusUnauthorized, "invalid_password", "invalid password")
			return
		}
		handleStoreError(w, err)
		return
	}
	s.setSessionCookie(w, r, session)
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"csrf":          session.CSRF,
		"role":          session.Role,
		"expiresAt":     session.ExpiresAt,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		session, authErr := s.app.Auth.Authenticate(r.Context(), cookie.Value)
		if authErr == nil && session != nil {
			if !s.requireCSRF(w, r, session) {
				return
			}
		}
		_ = s.app.Auth.Logout(r.Context(), cookie.Value)
	}
	s.clearSessionCookie(w, r)
	writeNoContent(w)
}
