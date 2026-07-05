package core

import (
	"errors"
	"testing"
)

func TestUpgradeSSLMode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{
			name: "disable is upgraded to require",
			in:   "postgres://u:p@h:5432/db?sslmode=disable",
			want: "postgres://u:p@h:5432/db?sslmode=require",
			ok:   true,
		},
		{
			name: "allow is upgraded to require",
			in:   "postgres://u:p@h:5432/db?sslmode=allow",
			want: "postgres://u:p@h:5432/db?sslmode=require",
			ok:   true,
		},
		{
			name: "prefer is upgraded to require",
			in:   "postgres://u:p@h:5432/db?sslmode=prefer",
			want: "postgres://u:p@h:5432/db?sslmode=require",
			ok:   true,
		},
		{
			name: "no sslmode is upgraded to require",
			in:   "postgres://u:p@h:5432/db",
			want: "postgres://u:p@h:5432/db?sslmode=require",
			ok:   true,
		},
		{
			name: "already require: no change",
			in:   "postgres://u:p@h:5432/db?sslmode=require",
			want: "postgres://u:p@h:5432/db?sslmode=require",
			ok:   false,
		},
		{
			name: "verify-ca is left alone (operator picked it)",
			in:   "postgres://u:p@h:5432/db?sslmode=verify-ca",
			want: "postgres://u:p@h:5432/db?sslmode=verify-ca",
			ok:   false,
		},
		{
			name: "verify-full is left alone",
			in:   "postgres://u:p@h:5432/db?sslmode=verify-full",
			want: "postgres://u:p@h:5432/db?sslmode=verify-full",
			ok:   false,
		},
		{
			name: "non-postgres URL: no change",
			in:   "mysql://u:p@h:3306/db",
			want: "mysql://u:p@h:3306/db",
			ok:   false,
		},
		{
			name: "junk URL: no change",
			in:   "not a url",
			want: "not a url",
			ok:   false,
		},
		{
			name: "empty: no change",
			in:   "",
			want: "",
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := upgradeSSLMode(tc.in)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("upgradeSSLMode(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestIsSSLRequiredError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unrelated", errors.New("connection reset by peer"), false},
		{
			"pg_hba no encryption",
			errors.New(`server error: FATAL: pg_hba.conf rejects connection for host "172.18.0.6", user "postgres", database "app", no encryption (SQLSTATE 28000)`),
			true,
		},
		{
			"case-insensitive",
			errors.New("NO ENCRYPTION"),
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSSLRequiredError(tc.err); got != tc.want {
				t.Fatalf("isSSLRequiredError = %v, want %v", got, tc.want)
			}
		})
	}
}
