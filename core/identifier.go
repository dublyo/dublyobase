package core

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

var dataIdentifierRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,58}$`)
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

var reservedDataIdentifiers = map[string]struct{}{
	"cmax":               {},
	"cmin":               {},
	"created":            {},
	"ctid":               {},
	"id":                 {},
	"information_schema": {},
	"oid":                {},
	"public":             {},
	"tableoid":           {},
	"updated":            {},
	"xmax":               {},
	"xmin":               {},
}

func NormalizeIdentifier(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func ValidateDataIdentifier(kind string, name string) error {
	if !dataIdentifierRe.MatchString(name) {
		return fmt.Errorf("%w: %s must match %s", ErrValidation, kind, dataIdentifierRe.String())
	}
	if _, reserved := reservedDataIdentifiers[name]; reserved {
		return fmt.Errorf("%w: %s %q is reserved", ErrValidation, kind, name)
	}
	if strings.HasPrefix(name, "_dbo") || strings.HasPrefix(name, "pg_") {
		return fmt.Errorf("%w: %s %q uses a reserved prefix", ErrValidation, kind, name)
	}
	return nil
}

func ValidateUUID(id string) error {
	if !uuidRe.MatchString(id) {
		return fmt.Errorf("%w: invalid UUID", ErrValidation)
	}
	return nil
}

func quoteIdent(parts ...string) string {
	return pgx.Identifier(parts).Sanitize()
}
