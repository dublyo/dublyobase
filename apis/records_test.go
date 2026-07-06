package apis

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestDirectusFilterQueryLogicalArrays(t *testing.T) {
	req := httptest.NewRequest("GET", "/?filter[_or][0][title][_icontains]=hello&filter[_or][1][score][_gte]=7&filter[status][_eq]=live", nil)
	filter := directusFilterQuery(req)
	var got map[string]any
	if err := json.Unmarshal([]byte(filter), &got); err != nil {
		t.Fatalf("filter JSON = %q, err = %v", filter, err)
	}
	want := map[string]any{
		"_or": []any{
			map[string]any{"title": map[string]any{"_icontains": "hello"}},
			map[string]any{"score": map[string]any{"_gte": "7"}},
		},
		"status": map[string]any{"_eq": "live"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filter = %#v, want %#v", got, want)
	}
}
