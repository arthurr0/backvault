package server

import (
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		level := "info"
		if sw.status >= 500 {
			level = "error"
		}
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"bytes", sw.bytes,
			"durationMs", time.Since(started).Milliseconds(),
			"ip", s.clientIP(r),
		}
		if level == "error" {
			s.log.Error("request", attrs...)
			return
		}
		s.log.Debug("request", attrs...)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				s.log.Error("panic in handler", "path", r.URL.Path, "panic", rec, "stack", string(debug.Stack()))
				s.writeError(w, r, http.StatusInternalServerError, codeInternal, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	policy := contentSecurityPolicy(s.scriptHashes)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Content-Security-Policy", policy)
		}
		next.ServeHTTP(w, r)
	})
}

func contentSecurityPolicy(scriptHashes []string) string {
	scriptSrc := "'self'"
	for _, hash := range scriptHashes {
		scriptSrc += " '" + hash + "'"
	}
	return strings.Join([]string{
		"default-src 'self'",
		"script-src " + scriptSrc,
		"img-src 'self' data:",
		"style-src 'self' 'unsafe-inline'",
		"font-src 'self' data:",
		"connect-src 'self'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
