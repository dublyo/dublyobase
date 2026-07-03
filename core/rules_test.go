package core

import (
	"errors"
	"strings"
	"testing"
)

func testRuleCollection() *Collection {
	return &Collection{
		Name: "posts",
		Type: CollectionBase,
		Fields: []Field{
			{Name: "title", Type: "text", Required: true},
			{Name: "published", Type: "bool"},
			{Name: "owner", Type: "relation", Options: map[string]any{"collection": "users"}},
		},
	}
}

func TestCompileFilter(t *testing.T) {
	expr, err := CompileFilter(`published = true && owner = @request.auth.id`, testRuleCollection())
	if err != nil {
		t.Fatal(err)
	}
	want := `((("published" = $1)) and (("owner" = (select _dbo.request_auth_id()))))`
	if expr.SQL != want {
		t.Fatalf("SQL = %q, want %q", expr.SQL, want)
	}
	if len(expr.Args) != 1 || expr.Args[0] != true {
		t.Fatalf("args = %#v", expr.Args)
	}
}

func TestCompilePolicyRule(t *testing.T) {
	c := testRuleCollection()
	publicRule := ""
	got, err := compilePolicyRule(&publicRule, c)
	if err != nil {
		t.Fatal(err)
	}
	if got != "true" {
		t.Fatalf("public rule = %q", got)
	}
	if got, err := compilePolicyRule(nil, c); err != nil || got != "false" {
		t.Fatalf("nil rule = %q, err = %v", got, err)
	}
	ownerRule := `owner = @request.auth.id`
	got, err = compilePolicyRule(&ownerRule, c)
	if err != nil {
		t.Fatal(err)
	}
	if got != `("owner" = (select _dbo.request_auth_id()))` {
		t.Fatalf("owner rule = %q", got)
	}
}

func TestRuleValidationRejectsUnsafeExpressions(t *testing.T) {
	c := testRuleCollection()
	for _, rule := range []string{
		`missing = true`,
		`title = "x"; title = "y"`,
		`@request.auth.email = "a@example.com"`,
		`lower(title) = "x"`,
	} {
		c.ListRule = &rule
		if err := ValidateCollectionRules(c); !errors.Is(err, ErrInvalidRule) {
			t.Fatalf("rule %q error = %v, want ErrInvalidRule", rule, err)
		}
	}
}

func TestAPIKeyGeneration(t *testing.T) {
	key, prefix, err := GenerateAPIKey(APIKeyService)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "dbo_service_") {
		t.Fatalf("key prefix = %q", key)
	}
	if !strings.HasPrefix(key, prefix) || len(prefix) <= len("dbo_service_") {
		t.Fatalf("stored prefix %q not useful for key %q", prefix, key)
	}
	if HashToken(key) == key {
		t.Fatal("API key hash must not equal plaintext")
	}
}
