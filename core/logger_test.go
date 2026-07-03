package core

import (
	"strings"
	"testing"
)

func TestRedactURL(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		secret string // must never appear in output
	}{
		{"userinfo password", "postgres://user:s3cret@db:5432/app?sslmode=disable", "s3cret"},
		{"password with special chars", "postgres://user:p%40ss%3Aw@db/app", "p%40ss%3Aw"},
		{"query param password", "postgres://user@db/app?password=s3cret", "s3cret"},
		{"query param only", "postgres://db/app?password=s3cret&sslmode=disable", "s3cret"},
		{"sslpassword param", "postgres://db/app?sslpassword=s3cret", "s3cret"},
		{"kv dsn form", "host=db user=app password=s3cret dbname=app", "s3cret"},
		{"kv dsn sslpassword", "host=db sslpassword=s3cret", "s3cret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := RedactURL(tc.in)
			if strings.Contains(out, tc.secret) {
				t.Fatalf("secret leaked: %q -> %q", tc.in, out)
			}
		})
	}

	t.Run("no password passes through", func(t *testing.T) {
		in := "postgres://user@db:5432/app?sslmode=disable"
		if out := RedactURL(in); out != in {
			t.Fatalf("mangled password-less url: %q -> %q", in, out)
		}
	})
}
