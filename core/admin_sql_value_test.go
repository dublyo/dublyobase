package core

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// The SQL console rendered anything that was not a plain scalar with
// fmt.Sprint, so a uuid came back as "[143 164 ...]", a numeric as
// "{2885000 -3 false finite true}" and jsonb as "map[a:1]" — none of which a
// person can read or paste back into a query.
func TestSQLConsoleValueRendersDriverTypes(t *testing.T) {
	num := pgtype.Numeric{Valid: true, Exp: -3, Int: big.NewInt(2885000)}

	id := [16]byte{0x8f, 0xa4, 0xe0, 0x19, 0x35, 0xfe, 0x4b, 0xc4, 0x87, 0x43, 0xde, 0x70, 0x7c, 0x53, 0xb5, 0x9c}

	cases := []struct {
		name string
		in   any
		want any
	}{
		{"uuid", id, "8fa4e019-35fe-4bc4-8743-de707c53b59c"},
		{"numeric", num, "2885.000"},
		{"numeric pointer", &num, "2885.000"},
		{"null numeric", pgtype.Numeric{}, nil},
		{"interval days", pgtype.Interval{Days: 1, Valid: true}, "1 day"},
		{"interval time", pgtype.Interval{Microseconds: int64(90 * time.Minute / time.Microsecond), Valid: true}, "01:30:00"},
		{"interval zero", pgtype.Interval{Valid: true}, "00:00:00"},
		{"null interval", pgtype.Interval{}, nil},
		{"string passthrough", "hello", "hello"},
		{"int passthrough", int64(7), int64(7)},
	}
	for _, tc := range cases {
		if got := sqlConsoleValue(tc.in); got != tc.want {
			t.Errorf("%s: got %#v, want %#v", tc.name, got, tc.want)
		}
	}
}

// Containers must survive as containers so they encode as real JSON rather
// than as Go's map/slice debug syntax.
func TestSQLConsoleValueWalksContainers(t *testing.T) {
	got := sqlConsoleValue(map[string]any{"a": int64(1), "b": []any{int64(2), int64(3)}})
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"a":1,"b":[2,3]}` {
		t.Errorf("jsonb: got %s", encoded)
	}

	num := pgtype.Numeric{Valid: true, Exp: -1, Int: big.NewInt(105)}
	arr := sqlConsoleValue([]any{num, num})
	encoded, err = json.Marshal(arr)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `["10.5","10.5"]` {
		t.Errorf("numeric array: got %s", encoded)
	}

	if got := sqlConsoleValue([]byte("raw")); got != "raw" {
		t.Errorf("bytea should stay a string, got %#v", got)
	}
}
