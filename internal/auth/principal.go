package auth

import (
	"context"

	"github.com/arthurr0/backvault/internal/core"
)

type AuthType string

const (
	AuthSession AuthType = "session"
	AuthToken   AuthType = "token"
)

type Principal struct {
	Type      AuthType
	User      *core.User
	TokenID   string
	TokenName string
	Scopes    []string
	JobSlugs  []string
	SessionID string
}

func (p *Principal) Label() string {
	if p == nil {
		return "anonymous"
	}
	if p.User != nil {
		if p.User.Email != "" {
			return p.User.Email
		}
		return p.User.Name
	}
	if p.TokenName != "" {
		return "token:" + p.TokenName
	}
	return "token"
}

func (p *Principal) ID() string {
	if p == nil {
		return ""
	}
	if p.User != nil {
		return p.User.ID
	}
	return p.TokenID
}

func (p *Principal) IsAdmin() bool {
	return p.HasScope(core.ScopeAdmin)
}

func (p *Principal) HasScope(scope string) bool {
	if p == nil {
		return false
	}
	for _, s := range p.Scopes {
		if s == scope {
			return true
		}
		if s == core.ScopeAdmin {
			return true
		}
		if s == core.ScopeRun && scope == core.ScopeRead {
			return true
		}
	}
	return false
}

func (p *Principal) AllowsJob(slug string) bool {
	if p == nil {
		return false
	}
	if len(p.JobSlugs) == 0 {
		return true
	}
	for _, s := range p.JobSlugs {
		if s == slug {
			return true
		}
	}
	return false
}

func ScopesForRole(role core.UserRole) []string {
	if role == core.RoleAdmin {
		return []string{core.ScopeAdmin, core.ScopeRead, core.ScopeRun, core.ScopeIngest}
	}
	return []string{core.ScopeRead}
}

type contextKey struct{}

var principalKey contextKey

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey).(*Principal)
	return p
}
