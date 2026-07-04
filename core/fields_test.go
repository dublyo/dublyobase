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
		{Name: "title", Type: "text", Required: true, Help: "Shown below the input", Presentable: true, Options: map[string]any{"min": 2, "max": 120, "pattern": "^[a-z].*"}},
		{Name: "body", Type: "editor", Options: map[string]any{"maxSize": 4096}},
		{Name: "secret", Type: "password", Options: map[string]any{"min": 8, "max": 32, "cost": 4}},
		{Name: "starts", Type: "autodate", Options: map[string]any{"onCreate": true}},
		{Name: "published", Type: "bool"},
		{Name: "status", Type: "select", Options: map[string]any{"values": []string{"draft", "live"}, "maxSelect": 1}},
		{Name: "author", Type: "relation", Options: map[string]any{"collection": "users", "minSelect": 0, "maxSelect": 1}},
		{Name: "avatar", Type: "file"},
		{Name: "gallery", Type: "file", Options: map[string]any{"multiple": true, "maxSelect": 4, "maxSize": 1024, "mimeTypes": []string{"image/png", "text/*"}}},
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
		"select duplicate": {{Name: "status", Type: "select", Options: map[string]any{"values": []string{"a", "a"}}}},
		"bad pattern":      {{Name: "title", Type: "text", Options: map[string]any{"pattern": "["}}},
		"hidden present":   {{Name: "title", Type: "text", Hidden: true, Presentable: true}},
		"bad autodate":     {{Name: "stamp", Type: "autodate"}},
		"bad file mime":    {{Name: "asset", Type: "file", Options: map[string]any{"mimeTypes": []string{"plain"}}}},
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
		"editor":         {Field{Name: "body", Type: "editor"}, "text"},
		"password":       {Field{Name: "secret", Type: "password"}, "text"},
		"autodate":       {Field{Name: "starts", Type: "autodate"}, "timestamptz"},
		"select multi":   {Field{Name: "tags", Type: "select", Options: map[string]any{"maxSelect": 3}}, "text[]"},
		"relation multi": {Field{Name: "owners", Type: "relation", Options: map[string]any{"maxSelect": 2}}, "uuid[]"},
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

func TestValidateFileFieldSelectionMIMEOptions(t *testing.T) {
	field := Field{Name: "attachments", Type: "file", Options: map[string]any{"mimeTypes": []string{"text/plain", "image/*"}}}
	files := []FileMeta{
		{ID: "11111111-1111-4111-8111-111111111111", Path: "one", Name: "note.txt", Mime: "text/plain; charset=utf-8"},
		{ID: "22222222-2222-4222-8222-222222222222", Path: "two", Name: "photo.png", Mime: "image/png"},
	}
	if err := validateFileFieldSelection(field, files); err != nil {
		t.Fatalf("valid MIME selections rejected: %v", err)
	}

	err := validateFileFieldSelection(field, []FileMeta{{ID: "33333333-3333-4333-8333-333333333333", Path: "three", Name: "data.json", Mime: "application/json"}})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid MIME selection error = %v, want ErrValidation", err)
	}
}
