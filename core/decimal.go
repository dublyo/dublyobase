package core

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// Decimal fields exist because `number` is double precision, and IEEE 754 is
// the wrong representation for money: individual values round-trip fine but
// totals drift (0.1 + 0.2 + 99999999.99 + 1234.565 sums to ...855 as numeric
// and ...8549999 as float8). A decimal field is a real numeric(precision,scale)
// column, is carried over the wire as a JSON *string* so no JSON parser can
// silently turn it back into a float, and is compared exactly.
const (
	defaultDecimalPrecision = 18
	defaultDecimalScale     = 2
	maxDecimalPrecision     = 38
)

// decimalOptions reads options.precision / options.scale, falling back to a
// money-shaped default.
func decimalOptions(field Field) (precision int, scale int) {
	precision, scale = defaultDecimalPrecision, defaultDecimalScale
	if v, ok := intOption(field.Options, "precision"); ok {
		precision = v
	}
	if v, ok := intOption(field.Options, "scale"); ok {
		scale = v
	}
	return precision, scale
}

func validateDecimalOptions(field Field) error {
	precision, scale := decimalOptions(field)
	if precision < 1 || precision > maxDecimalPrecision {
		return fmt.Errorf("%w: decimal field %q options.precision must be between 1 and %d", ErrValidation, field.Name, maxDecimalPrecision)
	}
	if scale < 0 || scale > precision {
		return fmt.Errorf("%w: decimal field %q options.scale must be between 0 and options.precision", ErrValidation, field.Name)
	}
	for _, key := range []string{"min", "max"} {
		raw, ok := field.Options[key]
		if !ok {
			continue
		}
		text, ok := raw.(string)
		if !ok {
			return fmt.Errorf("%w: decimal field %q options.%s must be a decimal string", ErrValidation, field.Name, key)
		}
		if _, err := parseDecimalText(text); err != nil {
			return fmt.Errorf("%w: decimal field %q options.%s must be a decimal string", ErrValidation, field.Name, key)
		}
	}
	if minText, okMin := field.Options["min"].(string); okMin {
		if maxText, okMax := field.Options["max"].(string); okMax {
			minVal, err1 := parseDecimalText(minText)
			maxVal, err2 := parseDecimalText(maxText)
			if err1 == nil && err2 == nil && maxVal.Cmp(minVal) < 0 {
				return fmt.Errorf("%w: decimal field %q options.max must be greater than or equal to options.min", ErrValidation, field.Name)
			}
		}
	}
	return nil
}

// parseDecimalText accepts only a plain decimal literal. Exponent notation,
// NaN and Infinity are refused: they are all ways to smuggle a value that
// cannot be stored exactly, and big.Rat would happily accept the first.
func parseDecimalText(text string) (*big.Rat, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, fmt.Errorf("%w: value is not a decimal", ErrValidation)
	}
	for _, r := range trimmed {
		if r != '-' && r != '+' && r != '.' && (r < '0' || r > '9') {
			return nil, fmt.Errorf("%w: value is not a decimal", ErrValidation)
		}
	}
	rat, ok := new(big.Rat).SetString(trimmed)
	if !ok {
		return nil, fmt.Errorf("%w: value is not a decimal", ErrValidation)
	}
	return rat, nil
}

// decimalDigits reports the integer-part digit count and the fractional digit
// count of a plain decimal literal, so a value can be rejected before Postgres
// rounds or errors on it.
func decimalDigits(text string) (intDigits int, fracDigits int) {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimPrefix(strings.TrimPrefix(trimmed, "-"), "+")
	whole, frac, _ := strings.Cut(trimmed, ".")
	whole = strings.TrimLeft(whole, "0")
	frac = strings.TrimRight(frac, "0")
	return len(whole), len(frac)
}

// normalizeDecimalInput accepts a JSON string ("12.34") or a JSON number
// (12.34) and returns the exact literal. Numbers are decoded through
// json.Number, which keeps the source text, so nothing passes through float64
// on the way in.
func normalizeDecimalInput(field Field, raw json.RawMessage) (any, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		var number json.Number
		if err := json.Unmarshal(raw, &number); err != nil {
			return nil, fmt.Errorf("%w: field %q must be a decimal string", ErrValidation, field.Name)
		}
		text = number.String()
	}
	text = strings.TrimSpace(text)
	if text == "" {
		if field.Required {
			return nil, fmt.Errorf("%w: field %q is required", ErrValidation, field.Name)
		}
		return nil, nil
	}
	value, err := parseDecimalText(text)
	if err != nil {
		return nil, fmt.Errorf("%w: field %q must be a decimal string", ErrValidation, field.Name)
	}
	precision, scale := decimalOptions(field)
	intDigits, fracDigits := decimalDigits(text)
	if fracDigits > scale {
		return nil, fmt.Errorf("%w: field %q allows at most %d decimal places", ErrValidation, field.Name, scale)
	}
	if intDigits > precision-scale {
		return nil, fmt.Errorf("%w: field %q allows at most %d digits before the decimal point", ErrValidation, field.Name, precision-scale)
	}
	if minText, ok := field.Options["min"].(string); ok {
		if minVal, err := parseDecimalText(minText); err == nil && value.Cmp(minVal) < 0 {
			return nil, fmt.Errorf("%w: field %q must be greater than or equal to %s", ErrValidation, field.Name, minText)
		}
	}
	if maxText, ok := field.Options["max"].(string); ok {
		if maxVal, err := parseDecimalText(maxText); err == nil && value.Cmp(maxVal) > 0 {
			return nil, fmt.Errorf("%w: field %q must be less than or equal to %s", ErrValidation, field.Name, maxText)
		}
	}
	return text, nil
}

// formatExactNumeric renders a Postgres numeric as its exact decimal string.
// pgx hands back Int and Exp (value = Int * 10^Exp); going through float64 here
// is exactly the bug decimal fields exist to avoid, so the digits are shifted
// by hand. Scale is preserved as stored, so numeric(18,2) holding 1.5 reads
// back as "1.50".
func formatExactNumeric(n pgtype.Numeric) any {
	if !n.Valid || n.NaN || n.InfinityModifier != pgtype.Finite || n.Int == nil {
		return nil
	}
	digits := n.Int.String()
	negative := strings.HasPrefix(digits, "-")
	digits = strings.TrimPrefix(digits, "-")

	var out string
	switch {
	case n.Exp >= 0:
		out = digits + strings.Repeat("0", int(n.Exp))
	default:
		frac := int(-n.Exp)
		if len(digits) <= frac {
			digits = strings.Repeat("0", frac-len(digits)+1) + digits
		}
		split := len(digits) - frac
		out = digits[:split] + "." + digits[split:]
	}
	if negative && strings.Trim(out, "0.") != "" {
		out = "-" + out
	}
	return out
}

// decimalScales maps a collection's decimal columns to their declared scale.
//
// pgx reports a zero numeric as Int=0, Exp=0 whatever the column's scale is —
// the binary format for zero carries no digits, so the scale is not in the
// value to recover. Every other value keeps its exponent, so without this a
// numeric(12,6) column read back "0" for zero and "0.500000" for everything
// else, breaking the fixed-scale shape a money column is supposed to have.
func decimalScales(collection *Collection) map[string]int {
	if collection == nil {
		return nil
	}
	var out map[string]int
	for _, field := range collection.Fields {
		if field.Type != "decimal" {
			continue
		}
		_, scale := decimalOptions(field)
		if scale <= 0 {
			continue
		}
		if out == nil {
			out = make(map[string]int)
		}
		out[field.Name] = scale
	}
	return out
}

// padDecimalScale restores the trailing zeros the driver dropped.
func padDecimalScale(value any, scale int) any {
	text, ok := value.(string)
	if !ok || scale <= 0 || text == "" {
		return value
	}
	dot := strings.IndexByte(text, '.')
	if dot < 0 {
		return text + "." + strings.Repeat("0", scale)
	}
	if missing := scale - (len(text) - dot - 1); missing > 0 {
		return text + strings.Repeat("0", missing)
	}
	return text
}
