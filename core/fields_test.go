package core

import (
	"errors"
	"strings"
	"testing"
)

func TestDataIdentifierValidation(t *testing.T) {
	for _, name := range []string{"posts", "post_123", "a"} {
		if err := ValidateDataIdentifier("collection name", name); err != nil {
			t.Fatalf("valid identifier %q rejected: %v", name, err)
		}
	}
	for _, name := range []string{"Posts", "1post", "post-name", "id", "created", "pg_class", "_dbo_table", strings.Repeat("a", 60)} {
		if err := ValidateDataIdentifier("collection name", name); err == nil {
			t.Fatalf("invalid identifier %q accepted", name)
		}
	}
}

func TestValidateFields(t *testing.T) {
	valid := []Field{
		{Name: "title", Type: "text", Required: true},
		{Name: "published", Type: "bool"},
		{Name: "status", Type: "select", Options: map[string]any{"values": []string{"draft", "live"}}},
		{Name: "author", Type: "relation", Options: map[string]any{"collection": "users"}},
		{Name: "avatar", Type: "file"},
		{Name: "gallery", Type: "file", Options: map[string]any{"multiple": true}},
	}
	if err := ValidateFields(valid); err != nil {
		t.Fatalf("valid fields rejected: %v", err)
	}

	cases := map[string][]Field{
		"duplicate":        {{Name: "title", Type: "text"}, {Name: "title", Type: "number"}},
		"reserved":         {{Name: "id", Type: "text"}},
		"unsupported":      {{Name: "title", Type: "blob"}},
		"bad file option":  {{Name: "avatar", Type: "file", Options: map[string]any{"multiple": "yes"}}},
		"select values":    {{Name: "status", Type: "select", Options: map[string]any{"values": []string{}}}},
		"relation target":  {{Name: "author", Type: "relation", Options: map[string]any{"collection": "pg_class"}}},
		"missing relation": {{Name: "author", Type: "relation"}},
	}
	for name, fields := range cases {
		if err := ValidateFields(fields); err == nil {
			t.Fatalf("%s: invalid fields accepted", name)
		}
	}
}

func TestColumnDDL(t *testing.T) {
	cases := map[string]struct {
		field Field
		want  string
	}{
		"text required":  {Field{Name: "title", Type: "text", Required: true}, "text not null"},
		"number":         {Field{Name: "score", Type: "number"}, "double precision"},
		"bool default":   {Field{Name: "published", Type: "bool"}, "boolean not null default false"},
		"bool required":  {Field{Name: "published", Type: "bool", Required: true}, "boolean not null"},
		"json default":   {Field{Name: "meta", Type: "json"}, "jsonb not null default '{}'::jsonb"},
		"file":           {Field{Name: "avatar", Type: "file"}, "jsonb"},
		"file required":  {Field{Name: "avatar", Type: "file", Required: true}, "jsonb"},
		"select multi":   {Field{Name: "tags", Type: "select", Options: map[string]any{"multi": true}}, "text[]"},
		"relation multi": {Field{Name: "owners", Type: "relation", Options: map[string]any{"multi": true}}, "uuid[]"},
	}
	for name, tc := range cases {
		got, err := ColumnDDL(tc.field)
		if err != nil {
			t.Fatalf("%s: ColumnDDL error: %v", name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: DDL = %q, want %q", name, got, tc.want)
		}
	}

	got, err := ColumnDDL(Field{Name: "file", Type: "blob"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("unsupported field error = %v, DDL = %q", err, got)
	}
}
