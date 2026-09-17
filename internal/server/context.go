package server

import (
	"context"
	"net/http"
	"time"
)

func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

func detachedContext(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(r.Context()), d)
}
