package core

import "testing"

func TestProjectSlugValidation(t *testing.T) {
	for _, slug := range []string{"demo", "demo_123", "a12"} {
		if err := ValidateProjectSlug(slug); err != nil {
			t.Fatalf("valid slug %q rejected: %v", slug, err)
		}
	}
	for _, slug := range []string{"ab", "Demo", "_dbo", "pg_demo", "public", "information_schema", "demo-name"} {
		if err := ValidateProjectSlug(slug); err == nil {
			t.Fatalf("invalid slug %q accepted", slug)
		}
	}
}

func TestProjectNames(t *testing.T) {
	schema, roles := ProjectNames("demo")
	if schema != "proj_demo" {
		t.Fatalf("schema = %q", schema)
	}
	if roles.Anon != "demo_anon" || roles.Authenticated != "demo_authenticated" || roles.Service != "demo_service" {
		t.Fatalf("roles = %+v", roles)
	}
}
