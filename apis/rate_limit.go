package apis

import (
	"net/http"
	"sync"
	"time"
)

type rateLimiter struct {
	mu        sync.Mutex
	hits      map[string][]time.Time
	limit     int
	window    time.Duration
	maxKeys   int
	lastSweep time.Time
	now       func() time.Time
}

const defaultRateLimiterMaxKeys = 4096

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		hits:    make(map[string][]time.Time),
		limit:   limit,
		window:  window,
		maxKeys: defaultRateLimiterMaxKeys,
		now:     time.Now,
	}
}

func (l *rateLimiter) allow(key string) bool {
	if l == nil {
		return true
	}
	return l.allowLimit(key, l.limit)
}

func (l *rateLimiter) allowLimit(key string, limit int) bool {
	if l == nil {
		return true
	}
	if limit <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if l.lastSweep.IsZero() || now.Sub(l.lastSweep) >= l.window {
		l.sweep(now)
		l.lastSweep = now
	}
	if _, exists := l.hits[key]; !exists && len(l.hits) >= l.maxKeys {
		l.sweep(now)
		if len(l.hits) >= l.maxKeys {
			l.evictOldest()
		}
	}
	cutoff := now.Add(-l.window)
	hits := l.hits[key]
	keep := hits[:0]
	for _, hit := range hits {
		if hit.After(cutoff) {
			keep = append(keep, hit)
		}
	}
	if len(keep) >= limit {
		l.hits[key] = keep
		return false
	}
	l.hits[key] = append(keep, now)
	return true
}

func (l *rateLimiter) sweep(now time.Time) {
	cutoff := now.Add(-l.window)
	for key, hits := range l.hits {
		keep := hits[:0]
		for _, hit := range hits {
			if hit.After(cutoff) {
				keep = append(keep, hit)
			}
		}
		if len(keep) == 0 {
			delete(l.hits, key)
			continue
		}
		l.hits[key] = keep
	}
}

func (l *rateLimiter) evictOldest() {
	var oldestKey string
	var oldest time.Time
	for key, hits := range l.hits {
		latest := time.Time{}
		for _, hit := range hits {
			if hit.After(latest) {
				latest = hit
			}
		}
		if oldestKey == "" || latest.Before(oldest) {
			oldestKey = key
			oldest = latest
		}
	}
	if oldestKey != "" {
		delete(l.hits, oldestKey)
	}
}

func (s *server) limitByIP(scope string, limiter *rateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := scope + ":" + s.clientIP(r)
		if !limiter.allow(key) {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}
