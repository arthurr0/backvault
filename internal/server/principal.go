package server

import (
	"net/http"

	"github.com/arthurr0/backvault/internal/auth"
)

func actorLabel(r *http.Request) string {
	return auth.FromContext(r.Context()).Label()
}

func (s *Server) requireJobAccess(r *http.Request, slug string) error {
	p := auth.FromContext(r.Context())
	if p == nil || p.Type != auth.AuthToken {
		return nil
	}
	return auth.RequireJobAccess(p, slug)
}
