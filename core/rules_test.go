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

func TestRuleParserRejectsDeepAndHugeExpressions(t *testing.T) {
	c := testRuleCollection()
	deep := strings.Repeat("(", maxRuleDepth+2) + `title = "x"` + strings.Repeat(")", maxRuleDepth+2)
	if _, err := CompileFilter(deep, c); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("deep filter error = %v, want ErrInvalidFilter", err)
	}
	c.ListRule = &deep
	if err := ValidateCollectionRules(c); !errors.Is(err, ErrInvalidRule) {
		t.Fatalf("deep rule error = %v, want ErrInvalidRule", err)
	}

	huge := strings.Repeat(`title = "x" || `, maxRuleTokens) + `title = "x"`
	if _, err := CompileFilter(huge, c); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("huge filter error = %v, want ErrInvalidFilter", err)
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

// TestCompilePolicyRuleAuthIDAgainstString covers the canonical PocketBase
// "is anyone signed in" idiom. request_auth_id() returns uuid, so comparing it
// to a string literal used to emit `uuid <> ''` and only fail later, at
// CREATE POLICY time, as a 500.
func TestCompilePolicyRuleAuthIDAgainstString(t *testing.T) {
	c := testRuleCollection()
	cases := []struct {
		rule string
		want string
	}{
		{`@request.auth.id != ""`, `((select _dbo.request_auth_id()) is not null)`},
		{`@request.auth.id = ""`, `((select _dbo.request_auth_id()) is null)`},
		{`"" != @request.auth.id`, `((select _dbo.request_auth_id()) is not null)`},
		// a column comparison must stay uuid = uuid so the index is still usable
		{`owner = @request.auth.id`, `("owner" = (select _dbo.request_auth_id()))`},
	}
	for _, tc := range cases {
		rule := tc.rule
		got, err := compilePolicyRule(&rule, c)
		if err != nil {
			t.Fatalf("rule %q: unexpected error: %v", tc.rule, err)
		}
		if got != tc.want {
			t.Fatalf("rule %q = %q, want %q", tc.rule, got, tc.want)
		}
	}
}

func TestCompilePolicyRuleRejectsNonUUIDAuthIDLiteral(t *testing.T) {
	c := testRuleCollection()
	for _, rule := range []string{
		`@request.auth.id = "not-a-uuid"`,
		`@request.auth.id != "abc"`,
	} {
		r := rule
		if _, err := compilePolicyRule(&r, c); !errors.Is(err, ErrInvalidRule) {
			t.Fatalf("rule %q: expected ErrInvalidRule, got %v", rule, err)
		}
	}
	valid := `@request.auth.id = "b7c8f0f2-3a1e-4b8e-9c2d-5f6a7b8c9d0e"`
	if _, err := compilePolicyRule(&valid, c); err != nil {
		t.Fatalf("valid uuid literal rejected: %v", err)
	}
}
