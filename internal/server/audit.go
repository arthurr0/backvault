package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/auth"
	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/store"
)

func (s *Server) audit(r *http.Request, p *auth.Principal, action, objectType, objectID, objectName string, details map[string]any) {
	if p == nil {
		p = auth.FromContext(r.Context())
	}
	entry := core.AuditEntry{
		Time:       time.Now().UTC(),
		ActorID:    p.ID(),
		ActorLabel: p.Label(),
		Action:     action,
		ObjectType: objectType,
		ObjectID:   objectID,
		ObjectName: objectName,
		Details:    details,
		IP:         s.clientIP(r),
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	if err := s.store.Audit.Record(ctx, entry); err != nil {
		s.log.Error("could not record audit entry", "action", action, "error", err)
	}
}

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	page, err := pageFromRequest(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	since, err := parseTimeParam(r, "since")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	filter := store.AuditFilter{
		Actor:      strings.TrimSpace(r.URL.Query().Get("actor")),
		Action:     strings.TrimSpace(r.URL.Query().Get("action")),
		ObjectType: strings.TrimSpace(r.URL.Query().Get("objectType")),
		Since:      since,
		Page:       page,
	}
	items, total, err := s.store.Audit.List(r.Context(), filter)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeList(w, items, total)
}
