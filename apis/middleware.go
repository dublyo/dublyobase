package apis

import (
	"net"
	"net/http"
	"strings"
	"time"
)

// statusRecorder captures the response status code for access logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush exposes the underlying flusher so SSE/streaming handlers still work
// through the wrapper.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// withMiddleware wraps a handler with CORS, access logging and (optionally)
// proxy-aware client-IP extraction.
func (s *server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

// applyCORS honors CORS_ORIGINS (a comma-separated list, or "*").
func (s *server) applyCORS(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	if allowed != "*" {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
}

// clientIP returns the real client IP, honoring X-Forwarded-For only when
// TRUST_PROXY_HEADERS is set (dublyobase runs behind cloudflared -> Traefik).
func (s *server) clientIP(r *http.Request) string {
	if s.app.Config.TrustProxyHeaders {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// left-most entry is the original client
			if i := strings.IndexByte(xff, ','); i != -1 {
				return strings.TrimSpace(xff[:i])
			}
			return strings.TrimSpace(xff)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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
