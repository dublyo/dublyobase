package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/dublyo/dublyobase/core"
)

func TestAdminInsightsEndpoints(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	adminToken := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, adminToken)
	serviceKey := createAPIKeyForRecords(t, srv.Handler, adminToken, slug, "service")

	createBody := `{
		"name":"orders",
		"type":"base",
		"fields":[
			{"name":"title","type":"text","searchable":true},
			{"name":"status","type":"select","options":{"values":["new","paid"]}},
			{"name":"amount","type":"number"}
		]
	}`
	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), adminToken, createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create collection: want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	createRecordInCollectionForTest(t, srv.Handler, slug, "orders", serviceKey, `{"title":"First","status":"new","amount":19.5}`)
	createRecordInCollectionForTest(t, srv.Handler, slug, "orders", serviceKey, `{"title":"Second","status":"paid","amount":41}`)

	rec = getJSON(srv.Handler, fmt.Sprintf("/admin/api/projects/%s/insights?hours=24", slug), adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("project insights: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var projectInsights core.ProjectInsights
	if err := json.Unmarshal(rec.Body.Bytes(), &projectInsights); err != nil {
		t.Fatal(err)
	}
	if projectInsights.Range.Hours != 24 || len(projectInsights.Requests) == 0 {
		t.Fatalf("unexpected project range: %+v", projectInsights.Range)
	}
	var ordersSummary *core.CollectionInsightSummary
	for i := range projectInsights.Collections {
		if projectInsights.Collections[i].Name == "orders" {
			ordersSummary = &projectInsights.Collections[i]
			break
		}
	}
	if ordersSummary == nil || ordersSummary.Records != 2 || ordersSummary.NewRecords != 2 {
		t.Fatalf("orders summary mismatch: %+v", projectInsights.Collections)
	}

	rec = getJSON(srv.Handler, fmt.Sprintf("/admin/api/projects/%s/collections/orders/insights?hours=24", slug), adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("collection insights: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var collectionInsights core.CollectionInsights
	if err := json.Unmarshal(rec.Body.Bytes(), &collectionInsights); err != nil {
		t.Fatal(err)
	}
	if collectionInsights.Records != 2 || collectionInsights.NewRecords != 2 || len(collectionInsights.Created) == 0 {
		t.Fatalf("unexpected collection insights counts: %+v", collectionInsights)
	}
	var amountField *core.CollectionFieldInsight
	for i := range collectionInsights.Fields {
		if collectionInsights.Fields[i].Name == "amount" {
			amountField = &collectionInsights.Fields[i]
			break
		}
	}
	if amountField == nil || amountField.Numeric == nil || amountField.Numeric.Sum != 60.5 {
		t.Fatalf("amount field numeric stats mismatch: %+v", collectionInsights.Fields)
	}

	rec = getJSON(srv.Handler, fmt.Sprintf("/admin/api/projects/%s/insights?hours=2", slug), adminToken)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid range: want 422, got %d: %s", rec.Code, rec.Body.String())
	}
}
