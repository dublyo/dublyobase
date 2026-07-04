package apis

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/dublyo/dublyobase/core"
)

// statusRecorder captures the response status code for access logging while
// staying transparent to streaming and upgrades: it exposes Unwrap for
// http.ResponseController (Flush/Hijack/SetDeadline) and passes Hijack through
// for libraries that type-assert http.Hijacker directly (WebSocket upgrades).
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("underlying ResponseWriter does not support hijacking")
}

// withMiddleware wraps a handler with CORS, access logging and (optionally)
// proxy-aware client-IP extraction.
func (s *server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applySecurityHeaders(w)
		s.applyCORS(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)

		s.app.Log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"dur_ms", time.Since(start).Milliseconds(),
			"ip", s.clientIP(r),
		)
	})
}

func applySecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'self'",
		"base-uri 'self'",
		"frame-ancestors 'none'",
		"form-action 'self'",
		"img-src 'self' data: blob:",
		"font-src 'self' data:",
		"script-src 'self' 'unsafe-inline'",
		"style-src 'self' 'unsafe-inline'",
		"connect-src 'self'",
	}, "; "))
}

// applyCORS honors CORS_ORIGINS (a comma-separated list, or "*").
func (s *server) applyCORS(w http.ResponseWriter, r *http.Request) {
	wildcard := len(s.app.Config.CORSOrigins) == 1 && s.app.Config.CORSOrigins[0] == "*"
	if !wildcard {
		// The response depends on the Origin header even when it doesn't
		// match — shared caches must always be told.
		w.Header().Add("Vary", "Origin")
	}

	origin := r.Header.Get("Origin")
	allowed := ""
	for _, o := range s.app.Config.CORSOrigins {
		if o == "*" {
			allowed = "*"
			break
		}
		if o == origin {
			allowed = origin
			break
		}
	}
	if allowed == "" {
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", allowed)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Checksum-SHA256")
	w.Header().Set("Access-Control-Max-Age", "300")
	if allowed != "*" {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
}

// clientIP returns the real client IP. Behind cloudflared → Traefik the
// left-most X-Forwarded-For entry is attacker-controlled (clients can send the
// header), so prefer Cloudflare's CF-Connecting-IP, then the RIGHT-most XFF
// entry (appended by our trusted hop), and only then RemoteAddr.
func (s *server) clientIP(r *http.Request) string {
	remoteIP := s.remoteIP(r)
	if s.app.Config.TrustProxyHeaders && s.trustsProxy(remoteIP) {
		if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cf != "" {
			return cf
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	if remoteIP != "" {
		return remoteIP
	}
	return r.RemoteAddr
}

func (s *server) remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *server) trustsProxy(ipText string) bool {
	ip := net.ParseIP(strings.TrimSpace(ipText))
	if ip == nil {
		return false
	}
	cidrs := s.app.Config.TrustedProxyCIDRs
	if len(cidrs) == 0 {
		cidrs = core.DefaultTrustedProxyCIDRs()
	}
	for _, raw := range cidrs {
		_, network, err := net.ParseCIDR(raw)
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

// setSSEHeaders configures a response for Server-Sent Events in a way that
// survives the Cloudflare tunnel + Traefik: no buffering, no transform, flush
// per event. (Used by realtime handlers in a later milestone.)
func setSSEHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
}
