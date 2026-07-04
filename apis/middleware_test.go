package apis

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dublyo/dublyobase/core"
)

func testServer(origins []string) *server {
	cfg := &core.Config{CORSOrigins: origins, TrustProxyHeaders: true}
	app := core.NewApp(cfg, nil, testLogger())
	return &server{app: app}
}

// hijackableRecorder simulates a writer that supports hijacking (as the real
// net/http one does for HTTP/1.1).
type hijackableRecorder struct{ *httptest.ResponseRecorder }

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c1, _ := net.Pipe()
	return c1, bufio.NewReadWriter(bufio.NewReader(c1), bufio.NewWriter(c1)), nil
}

// TestWrapperSupportsHijackAndFlush locks the WebSocket/SSE compatibility of
// the logging middleware: both direct type-asserts and http.ResponseController
// must reach the underlying writer through statusRecorder.
func TestWrapperSupportsHijackAndFlush(t *testing.T) {
	s := testServer([]string{"*"})

	var sawHijacker, sawController bool
	h := s.withMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				sawHijacker = true
				conn.Close()
			}
		}
	}))
	req := httptest.NewRequest("GET", "/ws", nil)
	h.ServeHTTP(&hijackableRecorder{httptest.NewRecorder()}, req)
	if !sawHijacker {
		t.Fatal("http.Hijacker type-assert must survive the middleware wrapper")
	}

	h2 := s.withMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		if err := rc.Flush(); err == nil {
			sawController = true
		}
	}))
	h2.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/sse", nil))
	if !sawController {
		t.Fatal("http.ResponseController.Flush must work through the wrapper (Unwrap)")
	}
}

func TestCORS(t *testing.T) {
	t.Run("allowed origin echoed with Vary", func(t *testing.T) {
		s := testServer([]string{"https://a.com"})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/x", nil)
		req.Header.Set("Origin", "https://a.com")
		s.withMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://a.com" {
			t.Fatalf("ACAO = %q", got)
		}
		if rec.Header().Get("Vary") != "Origin" {
			t.Fatal("Vary: Origin missing on allowed request")
		}
		if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Fatal("credentials must be allowed for a specific origin")
		}
	})

	t.Run("disallowed origin gets Vary but no ACAO", func(t *testing.T) {
		s := testServer([]string{"https://a.com"})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/x", nil)
		req.Header.Set("Origin", "https://evil.com")
		s.withMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("disallowed origin must not get ACAO")
		}
		if rec.Header().Get("Vary") != "Origin" {
			t.Fatal("Vary: Origin must be sent even without a match (cache poisoning)")
		}
	})

	t.Run("wildcard never allows credentials", func(t *testing.T) {
		s := testServer([]string{"*"})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/x", nil)
		req.Header.Set("Origin", "https://any.com")
		s.withMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Fatal("wildcard must emit *")
		}
		if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Fatal("* + credentials is forbidden")
		}
	})

	t.Run("preflight short-circuits 204", func(t *testing.T) {
		s := testServer([]string{"https://a.com"})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("OPTIONS", "/x", nil)
		req.Header.Set("Origin", "https://a.com")
		called := false
		s.withMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })).ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent || called {
			t.Fatalf("preflight must 204 without hitting handlers (code=%d called=%v)", rec.Code, called)
		}
		if rec.Header().Get("Access-Control-Max-Age") == "" {
			t.Fatal("preflight should be cacheable (Max-Age)")
		}
	})
}

func TestSecurityHeaders(t *testing.T) {
	s := testServer([]string{"*"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	s.withMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

	for key, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := rec.Header().Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'none'") {
		t.Fatalf("CSP missing frame-ancestors: %q", got)
	}
}

func TestClientIP(t *testing.T) {
	s := testServer([]string{"*"})

	req := httptest.NewRequest("GET", "/x", nil)
	req.RemoteAddr = "10.0.0.9:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 100.64.0.7") // left = client-supplied
	if ip := s.clientIP(req); ip != "100.64.0.7" {
		t.Fatalf("must take right-most (trusted-hop) XFF entry, got %q", ip)
	}

	req.Header.Set("CF-Connecting-IP", "203.0.113.9")
	if ip := s.clientIP(req); ip != "203.0.113.9" {
		t.Fatalf("CF-Connecting-IP must win, got %q", ip)
	}

	s.app.Config.TrustProxyHeaders = false
	if ip := s.clientIP(req); ip != "10.0.0.9" {
		t.Fatalf("without trust, RemoteAddr host must win, got %q", ip)
	}
}
