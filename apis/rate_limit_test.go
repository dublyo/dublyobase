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
