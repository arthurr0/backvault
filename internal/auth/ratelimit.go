package auth

import (
	"sync"
	"time"
)

type RateLimiter struct {
	mu       sync.Mutex
	hits     map[string][]time.Time
	limit    int
	window   time.Duration
	lastScan time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{hits: map[string][]time.Time{}, limit: limit, window: window}
}

func (l *RateLimiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastScan) > l.window {
		for k, times := range l.hits {
			if len(times) == 0 || now.Sub(times[len(times)-1]) > l.window {
				delete(l.hits, k)
			}
		}
		l.lastScan = now
	}
	cutoff := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

func (l *RateLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}
