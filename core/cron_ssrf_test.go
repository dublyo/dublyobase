package core

import "testing"

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
