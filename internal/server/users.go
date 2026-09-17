package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/arthurr0/backvault/internal/auth"
	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/store"
)

type userRequest struct {
	Email    string        `json:"email"`
	Name     string        `json:"name"`
	Role     core.UserRole `json:"role"`
	Password string        `json:"password"`
}

type passwordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.Users.List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeList(w, items, len(items))
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	rec, err := s.store.Users.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rec.User)
}

func validateRole(role core.UserRole) (core.UserRole, bool) {
	switch role {
	case core.RoleAdmin, core.RoleViewer:
		return role, true
	case "":
		return core.RoleViewer, true
	}
	return "", false
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req userRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	ve := newValidationError("invalid user")
	if strings.TrimSpace(req.Email) == "" {
		ve.field("email", "required")
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		ve.field("password", err.Error())
	}
	role, ok := validateRole(req.Role)
	if !ok {
		ve.field("role", "must be admin or viewer")
	}
	if !ve.empty() {
		s.fail(w, r, ve)
		return
	}
	user, err := s.auth.CreateUser(r.Context(), core.User{Email: req.Email, Name: req.Name, Role: role}, req.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "user.create", "user", user.ID, user.Email, map[string]any{"role": string(user.Role)})
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rec, err := s.store.Users.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req userRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	ve := newValidationError("invalid user")
	if strings.TrimSpace(req.Email) == "" {
		ve.field("email", "required")
	}
	role, ok := validateRole(req.Role)
	if !ok {
		ve.field("role", "must be admin or viewer")
	}
	if !ve.empty() {
		s.fail(w, r, ve)
		return
	}
	if rec.Role == core.RoleAdmin && role != core.RoleAdmin {
		admins, err := s.store.Users.CountAdmins(ctx)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if admins <= 1 {
			s.writeError(w, r, http.StatusConflict, codeConflict, "the last administrator cannot be demoted")
			return
		}
	}
	rec.Email = req.Email
	rec.Name = req.Name
	rec.Role = role
	if err := s.store.Users.Update(ctx, rec.User); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "user.update", "user", rec.ID, rec.Email, map[string]any{"role": string(role)})
	writeJSON(w, http.StatusOK, rec.User)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rec, err := s.store.Users.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if rec.Role == core.RoleAdmin {
		admins, err := s.store.Users.CountAdmins(ctx)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if admins <= 1 {
			s.writeError(w, r, http.StatusConflict, codeConflict, "the last administrator cannot be deleted")
			return
		}
	}
	if err := s.store.Users.Delete(ctx, rec.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "user.delete", "user", rec.ID, rec.Email, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	principal := auth.FromContext(ctx)
	id := chi.URLParam(r, "id")
	self := principal.User != nil && principal.User.ID == id
	if !self && !principal.IsAdmin() {
		s.writeError(w, r, http.StatusForbidden, codeForbidden, "only administrators can change another user's password")
		return
	}
	rec, err := s.store.Users.Get(ctx, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req passwordRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := auth.ValidatePassword(req.NewPassword); err != nil {
		s.fail(w, r, newValidationError("invalid password").field("newPassword", err.Error()))
		return
	}
	if self {
		ok, err := auth.VerifyPassword(rec.PasswordHash, req.CurrentPassword)
		if err != nil && !errors.Is(err, auth.ErrInvalidHash) {
			s.fail(w, r, err)
			return
		}
		if !ok {
			s.fail(w, r, newValidationError("invalid password").field("currentPassword", "does not match"))
			return
		}
	}
	if err := s.auth.SetPassword(ctx, rec.ID, req.NewPassword); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.store.Sessions.DeleteForUser(ctx, rec.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "user.password", "user", rec.ID, rec.Email, nil)
	w.WriteHeader(http.StatusNoContent)
}
