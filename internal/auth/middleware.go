package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/store"
)

type ErrorWriter func(w http.ResponseWriter, r *http.Request, status int, code, message string)

type Middleware struct {
	Manager    *Manager
	Secure     bool
	WriteError ErrorWriter
}

const CSRFHeader = "X-Requested-With"
const CSRFValue = "backvault"

func (mw *Middleware) fail(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	if mw.WriteError != nil {
		mw.WriteError(w, r, status, code, message)
		return
	}
	http.Error(w, message, status)
}

func (mw *Middleware) Resolve(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if header := strings.TrimSpace(r.Header.Get("Authorization")); header != "" {
			if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
				mw.fail(w, r, http.StatusUnauthorized, "unauthenticated", "unsupported authorization scheme")
				return
			}
			secret := strings.TrimSpace(header[len("bearer "):])
			p, err := mw.Manager.ResolveToken(ctx, secret)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) || errors.Is(err, ErrTokenExpired) {
					mw.fail(w, r, http.StatusUnauthorized, "unauthenticated", "invalid or expired API token")
					return
				}
				mw.fail(w, r, http.StatusInternalServerError, "internal", "could not verify API token")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(ctx, p)))
			return
		}
		if cookie, err := r.Cookie(CookieName); err == nil && cookie.Value != "" {
			p, err := mw.Manager.ResolveSession(ctx, cookie.Value)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) || errors.Is(err, ErrSessionExpired) {
					ClearSessionCookie(w, mw.Secure)
					next.ServeHTTP(w, r)
					return
				}
				mw.fail(w, r, http.StatusInternalServerError, "internal", "could not verify session")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(ctx, p)))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (mw *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if FromContext(r.Context()) == nil {
			mw.fail(w, r, http.StatusUnauthorized, "unauthenticated", "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (mw *Middleware) RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := FromContext(r.Context())
			if p == nil {
				mw.fail(w, r, http.StatusUnauthorized, "unauthenticated", "authentication required")
				return
			}
			if !p.HasScope(scope) {
				mw.fail(w, r, http.StatusForbidden, "forbidden", "missing scope: "+scope)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (mw *Middleware) CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !mutating(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		p := FromContext(r.Context())
		if p != nil && p.Type == AuthSession && !strings.EqualFold(r.Header.Get(CSRFHeader), CSRFValue) {
			mw.fail(w, r, http.StatusForbidden, "forbidden", "missing "+CSRFHeader+": "+CSRFValue+" header")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func mutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func SetSessionCookie(w http.ResponseWriter, secret string, maxAgeSeconds int, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    secret,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAgeSeconds,
	})
}

func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func RequireJobAccess(p *Principal, slug string) error {
	if p == nil {
		return errors.New("authentication required")
	}
	if !p.AllowsJob(slug) {
		return errors.New("token is not allowed to use job " + slug)
	}
	return nil
}

func ValidateScopes(scopes []string) error {
	if len(scopes) == 0 {
		return errors.New("at least one scope is required")
	}
	for _, s := range scopes {
		switch s {
		case core.ScopeAdmin, core.ScopeRead, core.ScopeRun, core.ScopeIngest:
		default:
			return errors.New("unknown scope: " + s)
		}
	}
	return nil
}
