package core

import (
	"encoding/json"
	"errors"
	"math/big"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func decimalField() Field {
	return Field{Name: "amount", Type: "decimal", Options: map[string]any{"precision": 18, "scale": 3}}
}

func TestNormalizeDecimalInputKeepsExactText(t *testing.T) {
	f := decimalField()
	cases := map[string]string{
		`"1234.565"`:           "1234.565",
		`1234.565`:             "1234.565", // JSON number decoded via json.Number, not float64
		`"0.1"`:                "0.1",
		`"-42"`:                "-42",
		`"99999999999999.999"`: "99999999999999.999",
	}
	for raw, want := range cases {
		got, err := normalizeDecimalInput(f, json.RawMessage(raw))
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if got != want {
			t.Fatalf("%s = %v, want %v", raw, got, want)
		}
	}
}

func TestNormalizeDecimalInputRejectsUnstorableValues(t *testing.T) {
	f := decimalField()
	for _, raw := range []string{
		`"1.2345"`, // more places than scale=3
		`"1e5"`,    // exponent notation hides an inexact value
		`"NaN"`,
		`"Infinity"`,
		`"abc"`,
		`"1234567890123456.1"`, // exceeds precision-scale integer digits
	} {
		if _, err := normalizeDecimalInput(f, json.RawMessage(raw)); !errors.Is(err, ErrValidation) {
			t.Fatalf("%s: expected ErrValidation, got %v", raw, err)
		}
	}
}

func TestFormatExactNumericNeverUsesFloat(t *testing.T) {
	cases := []struct {
		int  string
		exp  int32
		want string
	}{
		{"1234565", -3, "1234.565"},
		{"150", -2, "1.50"}, // trailing zero preserved as stored
		{"1", -4, "0.0001"},
		{"-1234565", -3, "-1234.565"},
		{"42", 0, "42"},
		{"42", 2, "4200"},
		{"0", -2, "0.00"},
	}
	for _, tc := range cases {
		n := new(big.Int)
		n.SetString(tc.int, 10)
		got := formatExactNumeric(pgtype.Numeric{Int: n, Exp: tc.exp, Valid: true})
		if got != tc.want {
			t.Fatalf("Int=%s Exp=%d = %v, want %v", tc.int, tc.exp, got, tc.want)
		}
	}
	if got := formatExactNumeric(pgtype.Numeric{Valid: false}); got != nil {
		t.Fatalf("invalid numeric = %v, want nil", got)
	}
}

func TestDecimalColumnDDL(t *testing.T) {
	ddl, err := ColumnDDL(decimalField())
	if err != nil {
		t.Fatal(err)
	}
	if ddl != "numeric(18,3)" {
		t.Fatalf("ddl = %q", ddl)
	}
	defaults, err := ColumnDDL(Field{Name: "price", Type: "decimal"})
	if err != nil {
		t.Fatal(err)
	}
	if defaults != "numeric(18,2)" {
		t.Fatalf("default ddl = %q", defaults)
	}
}
