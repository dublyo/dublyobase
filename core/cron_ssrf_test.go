package core

import (
	"context"
	"net"
	"testing"
)

// A cron job URL is attacker-reachable through the admin API and MCP, so it must
// not be usable to probe the deployment's own network — cloud metadata endpoints
// and services bound to loopback are the targets that matter.
func TestValidateCronJobInputRejectsPrivateTargets(t *testing.T) {
	SetCronAllowPrivateTargets(false)
	blocked := []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://127.0.0.1:8080/admin",
		"http://localhost/internal",
		"http://10.0.0.5/",
		"http://192.168.1.1/",
		"http://172.16.0.1/",
		"http://[::1]/",
		"file:///etc/passwd",
	}
	for _, target := range blocked {
		in := CronJobInput{Name: "probe", Type: "http", Schedule: "@every 1m", Method: "GET", URL: target}
		if err := ValidateCronJobInput(&in); err == nil {
			t.Errorf("expected %s to be rejected", target)
		}
	}

	// A DNS name that resolves inward is caught at save time, not only when the
	// job runs — otherwise it saves clean and then fails on every execution.
	// localtest.me is a public name pointing at 127.0.0.1; skip rather than fail
	// when the test host has no DNS, since the check is best effort by design.
	if _, err := net.DefaultResolver.LookupIPAddr(context.Background(), "localtest.me"); err == nil {
		in := CronJobInput{Name: "by-name", Type: "http", Schedule: "@every 1m", Method: "GET", URL: "http://localtest.me/x"}
		if err := ValidateCronJobInput(&in); err == nil {
			t.Error("a hostname resolving to loopback should be rejected at save time")
		}
	} else {
		t.Log("skipping DNS-based check: no resolver available")
	}

	in := CronJobInput{Name: "ok", Type: "http", Schedule: "@every 1m", Method: "GET", URL: "https://example.com/hook"}
	if err := ValidateCronJobInput(&in); err != nil {
		t.Fatalf("public target rejected: %v", err)
	}
}

// Self-hosters who cron an internal service opt in at deploy time.
func TestCronPrivateTargetsAllowedWhenOptedIn(t *testing.T) {
	SetCronAllowPrivateTargets(true)
	t.Cleanup(func() { SetCronAllowPrivateTargets(false) })
	in := CronJobInput{Name: "internal", Type: "http", Schedule: "@every 1m", Method: "GET", URL: "http://app:8080/tick"}
	if err := ValidateCronJobInput(&in); err != nil {
		t.Fatalf("opted-in private target rejected: %v", err)
	}
}
