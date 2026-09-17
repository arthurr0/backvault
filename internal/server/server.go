package server

import (
	"context"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/arthurr0/backvault/internal/auth"
	"github.com/arthurr0/backvault/internal/config"
	"github.com/arthurr0/backvault/internal/engine"
	"github.com/arthurr0/backvault/internal/secrets"
	"github.com/arthurr0/backvault/internal/store"
)

type Options struct {
	Store    *store.Store
	Engine   *engine.Engine
	Auth     *auth.Manager
	Secrets  *secrets.Cipher
	Config   config.Config
	Logger   *slog.Logger
	StaticFS fs.FS
}

type Server struct {
	store    *store.Store
	engine   *engine.Engine
	auth     *auth.Manager
	secrets  *secrets.Cipher
	cfg      config.Config
	log      *slog.Logger
	static   fs.FS
	metrics  *metrics
	limiter  *auth.RateLimiter
	proxies  []*net.IPNet
	mw       *auth.Middleware
	router   chi.Router
	readyMu  sync.RWMutex
	readyErr error

	scriptHashes []string
}

func New(o Options) (*Server, error) {
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		store:   o.Store,
		engine:  o.Engine,
		auth:    o.Auth,
		secrets: o.Secrets,
		cfg:     o.Config,
		log:     log,
		static:  o.StaticFS,
		limiter: auth.NewRateLimiter(5, time.Minute),
		metrics: newMetrics(o.Store, o.Engine),
	}
	s.scriptHashes = inlineScriptHashes(o.StaticFS)
	s.proxies = parseProxies(o.Config.TrustedProxies, log)
	s.mw = &auth.Middleware{
		Manager: o.Auth,
		Secure:  strings.HasPrefix(o.Config.BaseURL, "https://"),
		WriteError: func(w http.ResponseWriter, r *http.Request, status int, code, message string) {
			s.writeError(w, r, status, code, message)
		},
	}
	s.router = s.buildRouter()
	return s, nil
}

func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) Metrics() engine.Observer { return s.metrics }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) secureCookies(ctx context.Context) bool {
	if strings.HasPrefix(s.cfg.BaseURL, "https://") {
		return true
	}
	settings, err := s.store.Settings.Get(ctx)
	if err != nil {
		return false
	}
	return strings.HasPrefix(settings.BaseURL, "https://")
}

func parseProxies(values []string, log *slog.Logger) []*net.IPNet {
	var out []*net.IPNet
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if !strings.Contains(v, "/") {
			ip := net.ParseIP(v)
			if ip == nil {
				log.Warn("ignoring invalid trusted proxy", "value", v)
				continue
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, network, err := net.ParseCIDR(v)
		if err != nil {
			log.Warn("ignoring invalid trusted proxy", "value", v, "error", err)
			continue
		}
		out = append(out, network)
	}
	return out
}

func (s *Server) clientIP(r *http.Request) string {
	remote := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	if len(s.proxies) == 0 {
		return remote
	}
	ip := net.ParseIP(remote)
	if ip == nil || !s.trusted(ip) {
		return remote
	}
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded == "" {
		if real := strings.TrimSpace(r.Header.Get("X-Real-Ip")); real != "" {
			return real
		}
		return remote
	}
	parts := strings.Split(forwarded, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(parts[i])
		parsed := net.ParseIP(candidate)
		if parsed == nil {
			continue
		}
		if s.trusted(parsed) {
			continue
		}
		return candidate
	}
	return remote
}

func (s *Server) trusted(ip net.IP) bool {
	for _, network := range s.proxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
