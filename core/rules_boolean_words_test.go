package core

import "testing"

// A check expression becomes a SQL CHECK constraint and reads like SQL, so
// people write `and` rather than `&&`. Both spellings now parse to the same
// thing; before, the SQL one failed with "unexpected token".
func TestRuleParserAcceptsSQLBooleanWords(t *testing.T) {
	collection := &Collection{Name: "reviews", Fields: []Field{
		{Name: "rating", Type: "number"},
		{Name: "score", Type: "number"},
	}}

	pairs := []struct{ symbol, word string }{
		{"rating >= 1 && rating <= 5", "rating >= 1 and rating <= 5"},
		{"rating = 1 || rating = 5", "rating = 1 or rating = 5"},
		{"rating >= 1 && rating <= 5", "rating >= 1 AND rating <= 5"},
		{"rating = 1 || rating = 5", "rating = 1 OR rating = 5"},
	}
	for _, pair := range pairs {
		wantSQL, err := compileImmutableExpr(pair.symbol, collection, "check")
		if err != nil {
			t.Fatalf("%q: %v", pair.symbol, err)
		}
		gotSQL, err := compileImmutableExpr(pair.word, collection, "check")
		if err != nil {
			t.Errorf("%q: %v", pair.word, err)
			continue
		}
		if gotSQL != wantSQL {
			t.Errorf("%q compiled to %q, want the same as %q which gave %q",
				pair.word, gotSQL, pair.symbol, wantSQL)
		}
	}

	// A field that happens to be spelled like one of the words must still be
	// usable, so the alias cannot swallow an identifier outright.
	withField := &Collection{Name: "t", Fields: []Field{{Name: "android", Type: "number"}}}
	if _, err := compileImmutableExpr("android > 0", withField, "check"); err != nil {
		t.Errorf("identifier starting with a boolean word broke: %v", err)
	}
}
