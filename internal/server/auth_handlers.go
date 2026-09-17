package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/auth"
	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/store"
)

type setupRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	needs, err := s.auth.NeedsSetup(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"needsSetup": needs})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	needs, err := s.auth.NeedsSetup(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !needs {
		s.writeError(w, r, http.StatusConflict, codeConflict, "setup has already been completed")
		return
	}
	var req setupRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	ve := newValidationError("invalid setup request")
	if strings.TrimSpace(req.Email) == "" {
		ve.field("email", "required")
	}
	if strings.TrimSpace(req.Name) == "" {
		ve.field("name", "required")
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		ve.field("password", err.Error())
	}
	if len(ve.Fields) > 0 {
		s.fail(w, r, ve)
		return
	}
	user, err := s.auth.CreateUser(ctx, core.User{Email: req.Email, Name: req.Name, Role: core.RoleAdmin}, req.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.startSession(w, r, user); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, &auth.Principal{Type: auth.AuthSession, User: &user}, "user.create", "user", user.ID, user.Email, map[string]any{"role": string(user.Role), "setup": true})
	writeJSON(w, http.StatusCreated, map[string]any{"user": user})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ip := s.clientIP(r)
	if !s.limiter.Allow(ip, time.Now()) {
		s.writeError(w, r, http.StatusTooManyRequests, "rate_limited", "too many login attempts, try again in a minute")
		return
	}
	var req loginRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	user, err := s.auth.Login(ctx, req.Email, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			s.writeError(w, r, http.StatusUnauthorized, codeUnauthenticated, "invalid email or password")
			return
		}
		s.fail(w, r, err)
		return
	}
	s.limiter.Reset(ip)
	if err := s.startSession(w, r, user); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user core.User) error {
	secret, expires, err := s.auth.CreateSession(r.Context(), user.ID, s.clientIP(r), r.UserAgent())
	if err != nil {
		return err
	}
	if err := s.store.Users.TouchLogin(r.Context(), user.ID, time.Now().UTC()); err != nil {
		s.log.Warn("could not record the login time", "user", user.Email, "error", err)
	}
	auth.SetSessionCookie(w, secret, int(time.Until(expires).Seconds()), s.secureCookies(r.Context()))
	return nil
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.CookieName); err == nil && cookie.Value != "" {
		if err := s.auth.DestroySession(r.Context(), cookie.Value); err != nil && !errors.Is(err, store.ErrNotFound) {
			s.fail(w, r, err)
			return
		}
	}
	auth.ClearSessionCookie(w, s.secureCookies(r.Context()))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	body := map[string]any{
		"authType": string(p.Type),
		"scopes":   p.Scopes,
	}
	if p.User != nil {
		body["user"] = p.User
	}
	if p.Type == auth.AuthToken {
		body["token"] = map[string]any{"id": p.TokenID, "name": p.TokenName, "jobSlugs": p.JobSlugs}
	}
	writeJSON(w, http.StatusOK, body)
}
