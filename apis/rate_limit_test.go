package apis

import (
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	limiter := newRateLimiter(2, time.Minute)
	limiter.now = func() time.Time { return now }

	if !limiter.allow("ip") || !limiter.allow("ip") {
		t.Fatal("first two hits should pass")
	}
	if limiter.allow("ip") {
		t.Fatal("third hit in window should be blocked")
	}
	now = now.Add(time.Minute + time.Second)
	if !limiter.allow("ip") {
		t.Fatal("hit after window should pass")
	}
}

func TestRateLimiterBoundsTrackedKeys(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	limiter := newRateLimiter(10, time.Minute)
	limiter.maxKeys = 2
	limiter.now = func() time.Time { return now }

	if !limiter.allow("a") || !limiter.allow("b") || !limiter.allow("c") {
		t.Fatal("new keys should pass while evicting old tracked keys")
	}
	if len(limiter.hits) > limiter.maxKeys {
		t.Fatalf("tracked keys = %d, want <= %d", len(limiter.hits), limiter.maxKeys)
	}
}
