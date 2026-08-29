package core

import "testing"

// pgx reports a zero numeric as Int=0, Exp=0 no matter the column's scale, so a
// numeric(12,6) money column read back "0" for zero while every other value
// kept its six decimal places.
func TestPadDecimalScale(t *testing.T) {
	cases := []struct {
		in    any
		scale int
		want  any
	}{
		{"0", 6, "0.000000"},
		{"0.500000", 6, "0.500000"},
		{"12", 2, "12.00"},
		{"1.5", 6, "1.500000"},
		{"-0", 6, "-0.000000"},
		{"0.000001", 6, "0.000001"},
		// already at or beyond the declared scale, leave alone
		{"1.234567", 6, "1.234567"},
		{"1.2345678", 6, "1.2345678"},
		{"0", 0, "0"},
		{nil, 6, nil},
		{"", 6, ""},
	}
	for _, tc := range cases {
		if got := padDecimalScale(tc.in, tc.scale); got != tc.want {
			t.Errorf("padDecimalScale(%#v, %d) = %#v, want %#v", tc.in, tc.scale, got, tc.want)
		}
	}
}

func TestColumnFormatsReadsFieldOptions(t *testing.T) {
	c := &Collection{Fields: []Field{
		{Name: "cost", Type: "decimal", Options: map[string]any{"precision": float64(12), "scale": float64(6)}},
		{Name: "qty", Type: "number"},
		{Name: "note", Type: "text"},
		{Name: "plain", Type: "decimal"},
	}}
	got := columnFormats(c)
	if got["cost"].scale != 6 {
		t.Errorf("cost scale = %d, want 6", got["cost"].scale)
	}
	if _, ok := got["qty"]; ok {
		t.Error("number field should not get a decimal scale")
	}
	if _, ok := got["note"]; ok {
		t.Error("text field should not get a decimal scale")
	}
	if got["plain"].scale != defaultDecimalScale {
		t.Errorf("decimal without options = %d, want the default %d", got["plain"].scale, defaultDecimalScale)
	}
	if columnFormats(nil) != nil {
		t.Error("nil collection should give no scales")
	}
}
