package core

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type InsightsRange struct {
	Hours         int       `json:"hours"`
	BucketMinutes int       `json:"bucketMinutes"`
	StartedAt     time.Time `json:"startedAt"`
	FinishedAt    time.Time `json:"finishedAt"`
}

type ProjectInsights struct {
	ProjectID   string                     `json:"projectId"`
	ProjectSlug string                     `json:"projectSlug"`
	Range       InsightsRange              `json:"range"`
	Metrics     ProjectMetrics             `json:"metrics"`
	Requests    []RequestInsightBucket     `json:"requests"`
	Methods     []InsightNameValue         `json:"methods"`
	Statuses    []InsightNameValue         `json:"statuses"`
	TopPaths    []InsightNameValue         `json:"topPaths"`
	Collections []CollectionInsightSummary `json:"collections"`
}

type RequestInsightBucket struct {
	Timestamp     time.Time `json:"timestamp"`
	Total         int       `json:"total"`
	Errors        int       `json:"errors"`
	AvgDurationMS float64   `json:"avgDurationMs"`
	P95DurationMS float64   `json:"p95DurationMs"`
}

type InsightCountBucket struct {
	Timestamp time.Time `json:"timestamp"`
	Count     int       `json:"count"`
}

type InsightNameValue struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

type CollectionInsightSummary struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	System     bool   `json:"system"`
	Records    int    `json:"records"`
	NewRecords int    `json:"newRecords"`
	Fields     int    `json:"fields"`
}

type CollectionInsights struct {
	ProjectID   string                   `json:"projectId"`
	ProjectSlug string                   `json:"projectSlug"`
	Collection  string                   `json:"collection"`
	Range       InsightsRange            `json:"range"`
	Records     int                      `json:"records"`
	NewRecords  int                      `json:"newRecords"`
	Created     []InsightCountBucket     `json:"created"`
	Fields      []CollectionFieldInsight `json:"fields"`
}

type CollectionFieldInsight struct {
	Name     string              `json:"name"`
	Type     string              `json:"type"`
	Filled   int                 `json:"filled"`
	Empty    int                 `json:"empty"`
	Distinct int                 `json:"distinct"`
	Top      []InsightNameValue  `json:"top,omitempty"`
	Numeric  *InsightNumberStats `json:"numeric,omitempty"`
}

type InsightNumberStats struct {
	Sum float64 `json:"sum"`
	Avg float64 `json:"avg"`
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

func GetProjectInsights(ctx context.Context, pool *pgxpool.Pool, projectSlug string, hours int, now time.Time) (*ProjectInsights, error) {
	project, insightRange, err := resolveInsightsRange(ctx, pool, projectSlug, hours, now)
	if err != nil {
		return nil, err
	}
	metrics, err := GetProjectMetrics(ctx, pool, projectSlug, insightRange.Hours, insightRange.FinishedAt)
	if err != nil {
		return nil, err
	}
	collections, err := ListCollections(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	requests, err := requestInsightBuckets(ctx, pool, project.ID, insightRange)
	if err != nil {
		return nil, err
	}
	methods, err := requestInsightNameValues(ctx, pool, project.ID, insightRange, "method")
	if err != nil {
		return nil, err
	}
	statuses, err := requestInsightNameValues(ctx, pool, project.ID, insightRange, "status")
	if err != nil {
		return nil, err
	}
	topPaths, err := requestInsightNameValues(ctx, pool, project.ID, insightRange, "path")
	if err != nil {
		return nil, err
	}
	summaries, err := collectionInsightSummaries(ctx, pool, project, collections, insightRange)
	if err != nil {
		return nil, err
	}
	return &ProjectInsights{
		ProjectID:   project.ID,
		ProjectSlug: project.Slug,
		Range:       insightRange,
		Metrics:     *metrics,
		Requests:    requests,
		Methods:     methods,
		Statuses:    statuses,
		TopPaths:    topPaths,
		Collections: summaries,
	}, nil
}

func GetCollectionInsights(ctx context.Context, pool *pgxpool.Pool, projectSlug string, collectionName string, hours int, now time.Time) (*CollectionInsights, error) {
	project, insightRange, err := resolveInsightsRange(ctx, pool, projectSlug, hours, now)
	if err != nil {
		return nil, err
	}
	collection, err := GetCollection(ctx, pool, projectSlug, collectionName)
	if err != nil {
		return nil, err
	}
	schemaName, tableName, err := collectionPhysicalTable(project, collection)
	if err != nil {
		return nil, err
	}
	exists, err := tableExists(ctx, pool, schemaName, tableName)
	if err != nil {
		return nil, err
	}
	result := &CollectionInsights{
		ProjectID:   project.ID,
		ProjectSlug: project.Slug,
		Collection:  collection.Name,
		Range:       insightRange,
		Created:     filledCountBuckets(insightRange),
		Fields:      make([]CollectionFieldInsight, 0),
	}
	if !exists || collection.Type == CollectionView {
		return result, nil
	}
	table := quoteIdent(schemaName, tableName)
	if err := pool.QueryRow(ctx, fmt.Sprintf(`select count(*) from %s`, table)).Scan(&result.Records); err != nil {
		return nil, mapRecordDBError(err)
	}
	if collectionStandardSystemColumns(collection) {
		createdColumn := quoteIdent("created")
		if err := pool.QueryRow(ctx, fmt.Sprintf(`select count(*) from %s where %s >= $1 and %s <= $2`, table, createdColumn, createdColumn), insightRange.StartedAt, insightRange.FinishedAt).Scan(&result.NewRecords); err != nil {
			return nil, mapRecordDBError(err)
		}
		series, err := countBuckets(ctx, pool, table, createdColumn, insightRange)
		if err != nil {
			return nil, err
		}
		result.Created = series
	}
	for _, field := range insightFields(collection) {
		insight, err := collectionFieldInsight(ctx, pool, table, collection, field, result.Records)
		if err != nil {
			return nil, err
		}
		result.Fields = append(result.Fields, insight)
	}
	return result, nil
}

func resolveInsightsRange(ctx context.Context, pool *pgxpool.Pool, projectSlug string, hours int, now time.Time) (*Project, InsightsRange, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, InsightsRange{}, err
	}
	bucketMinutes, err := insightsBucketMinutes(hours)
	if err != nil {
		return nil, InsightsRange{}, err
	}
	finished := now.UTC()
	started := finished.Add(-time.Duration(hours) * time.Hour)
	return project, InsightsRange{Hours: hours, BucketMinutes: bucketMinutes, StartedAt: started, FinishedAt: finished}, nil
}

func insightsBucketMinutes(hours int) (int, error) {
	switch hours {
	case 1:
		return 5, nil
	case 24:
		return 60, nil
	case 168:
		return 360, nil
	case 720:
		return 1440, nil
	default:
		return 0, fmt.Errorf("%w: insights range must be 1, 24, 168 or 720 hours", ErrValidation)
	}
}

func requestInsightBuckets(ctx context.Context, pool *pgxpool.Pool, projectID string, insightRange InsightsRange) ([]RequestInsightBucket, error) {
	buckets := filledRequestBuckets(insightRange)
	index := map[int64]int{}
	for i, bucket := range buckets {
		index[bucket.Timestamp.Unix()] = i
	}
	rows, err := pool.Query(ctx, `
		select to_timestamp(floor(extract(epoch from created_at) / $4::double precision) * $4)::timestamptz as bucket,
		       count(*),
		       count(*) filter (where status >= 500),
		       coalesce(avg(duration_ms), 0),
		       coalesce(percentile_cont(0.95) within group (order by duration_ms), 0)
		from _dbo.request_logs
		where project_id = $1 and created_at >= $2 and created_at <= $3
		group by bucket
		order by bucket`,
		projectID,
		insightRange.StartedAt,
		insightRange.FinishedAt,
		insightRange.BucketMinutes*60,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var bucket time.Time
		var total, errors int
		var avg, p95 float64
		if err := rows.Scan(&bucket, &total, &errors, &avg, &p95); err != nil {
			return nil, err
		}
		key := normalizeBucketTime(bucket, insightRange).Unix()
		if i, ok := index[key]; ok {
			buckets[i].Total = total
			buckets[i].Errors = errors
			buckets[i].AvgDurationMS = roundFloat(avg)
			buckets[i].P95DurationMS = roundFloat(p95)
		}
	}
	return buckets, rows.Err()
}

func requestInsightNameValues(ctx context.Context, pool *pgxpool.Pool, projectID string, insightRange InsightsRange, mode string) ([]InsightNameValue, error) {
	expr := "method"
	switch mode {
	case "method":
		expr = "method"
	case "status":
		expr = "((status / 100) * 100)::text || 'xx'"
	case "path":
		expr = "path"
	default:
		return nil, fmt.Errorf("%w: unsupported insights dimension", ErrValidation)
	}
	rows, err := pool.Query(ctx, fmt.Sprintf(`
		select %s as name, count(*)::double precision as value
		from _dbo.request_logs
		where project_id = $1 and created_at >= $2 and created_at <= $3
		group by name
		order by value desc, name asc
		limit 10`, expr),
		projectID,
		insightRange.StartedAt,
		insightRange.FinishedAt,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNameValues(rows)
}

func collectionInsightSummaries(ctx context.Context, pool *pgxpool.Pool, project *Project, collections []Collection, insightRange InsightsRange) ([]CollectionInsightSummary, error) {
	summaries := make([]CollectionInsightSummary, 0, len(collections))
	for i := range collections {
		collection := &collections[i]
		summary := CollectionInsightSummary{
			Name:   collection.Name,
			Type:   string(collection.Type),
			System: collection.System,
			Fields: len(insightFields(collection)),
		}
		if collection.Type == CollectionView {
			summaries = append(summaries, summary)
			continue
		}
		schemaName, tableName, err := collectionPhysicalTable(project, collection)
		if err != nil {
			return nil, err
		}
		exists, err := tableExists(ctx, pool, schemaName, tableName)
		if err != nil {
			return nil, err
		}
		if !exists {
			summaries = append(summaries, summary)
			continue
		}
		table := quoteIdent(schemaName, tableName)
		if err := pool.QueryRow(ctx, fmt.Sprintf(`select count(*) from %s`, table)).Scan(&summary.Records); err != nil {
			return nil, mapRecordDBError(err)
		}
		if collectionStandardSystemColumns(collection) {
			createdColumn := quoteIdent("created")
			if err := pool.QueryRow(ctx, fmt.Sprintf(`select count(*) from %s where %s >= $1 and %s <= $2`, table, createdColumn, createdColumn), insightRange.StartedAt, insightRange.FinishedAt).Scan(&summary.NewRecords); err != nil {
				return nil, mapRecordDBError(err)
			}
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func countBuckets(ctx context.Context, pool *pgxpool.Pool, table string, createdColumn string, insightRange InsightsRange) ([]InsightCountBucket, error) {
	buckets := filledCountBuckets(insightRange)
	index := map[int64]int{}
	for i, bucket := range buckets {
		index[bucket.Timestamp.Unix()] = i
	}
	rows, err := pool.Query(ctx, fmt.Sprintf(`
		select to_timestamp(floor(extract(epoch from %s) / $3::double precision) * $3)::timestamptz as bucket,
		       count(*)
		from %s
		where %s >= $1 and %s <= $2
		group by bucket
		order by bucket`,
		createdColumn,
		table,
		createdColumn,
		createdColumn,
	), insightRange.StartedAt, insightRange.FinishedAt, insightRange.BucketMinutes*60)
	if err != nil {
		return nil, mapRecordDBError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var bucket time.Time
		var count int
		if err := rows.Scan(&bucket, &count); err != nil {
			return nil, err
		}
		key := normalizeBucketTime(bucket, insightRange).Unix()
		if i, ok := index[key]; ok {
			buckets[i].Count = count
		}
	}
	return buckets, rows.Err()
}

func collectionFieldInsight(ctx context.Context, pool *pgxpool.Pool, table string, collection *Collection, field Field, total int) (CollectionFieldInsight, error) {
	column := recordColumnSQL(collection, field.Name)
	insight := CollectionFieldInsight{Name: field.Name, Type: field.Type, Top: []InsightNameValue{}}
	if err := pool.QueryRow(ctx, fmt.Sprintf(`select count(*) from %s where %s is not null`, table, column)).Scan(&insight.Filled); err != nil {
		return insight, mapRecordDBError(err)
	}
	if insight.Filled > total {
		insight.Filled = total
	}
	insight.Empty = total - insight.Filled
	if insight.Empty < 0 {
		insight.Empty = 0
	}
	if fieldSupportsDistinctInsight(field) {
		if err := pool.QueryRow(ctx, fmt.Sprintf(`select count(distinct %s) from %s where %s is not null`, column, table, column)).Scan(&insight.Distinct); err != nil {
			return insight, mapRecordDBError(err)
		}
	}
	if fieldSupportsTopInsight(field) {
		top, err := fieldTopValues(ctx, pool, table, column)
		if err != nil {
			return insight, err
		}
		insight.Top = top
	}
	if field.Type == "number" {
		stats, err := fieldNumberStats(ctx, pool, table, column)
		if err != nil {
			return insight, err
		}
		insight.Numeric = stats
	}
	return insight, nil
}

func fieldTopValues(ctx context.Context, pool *pgxpool.Pool, table string, column string) ([]InsightNameValue, error) {
	rows, err := pool.Query(ctx, fmt.Sprintf(`
		select left(%s::text, 120) as name, count(*)::double precision as value
		from %s
		where %s is not null
		group by name
		order by value desc, name asc
		limit 8`, column, table, column))
	if err != nil {
		return nil, mapRecordDBError(err)
	}
	defer rows.Close()
	return scanNameValues(rows)
}

func fieldNumberStats(ctx context.Context, pool *pgxpool.Pool, table string, column string) (*InsightNumberStats, error) {
	stats := &InsightNumberStats{}
	if err := pool.QueryRow(ctx, fmt.Sprintf(`
		select coalesce(sum(%[1]s), 0), coalesce(avg(%[1]s), 0), coalesce(min(%[1]s), 0), coalesce(max(%[1]s), 0)
		from %[2]s
		where %[1]s is not null`, column, table)).Scan(&stats.Sum, &stats.Avg, &stats.Min, &stats.Max); err != nil {
		return nil, mapRecordDBError(err)
	}
	stats.Sum = roundFloat(stats.Sum)
	stats.Avg = roundFloat(stats.Avg)
	stats.Min = roundFloat(stats.Min)
	stats.Max = roundFloat(stats.Max)
	return stats, nil
}

func scanNameValues(rows pgx.Rows) ([]InsightNameValue, error) {
	out := []InsightNameValue{}
	for rows.Next() {
		var item InsightNameValue
		if err := rows.Scan(&item.Name, &item.Value); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func insightFields(collection *Collection) []Field {
	if collection == nil {
		return nil
	}
	out := []Field{}
	for _, field := range collection.Fields {
		if field.Hidden || field.Type == "password" || field.Type == "json" || field.Type == "file" || field.Type == "editor" {
			continue
		}
		out = append(out, field)
		if len(out) >= 16 {
			break
		}
	}
	return out
}

func fieldSupportsDistinctInsight(field Field) bool {
	switch field.Type {
	case "text", "email", "url", "select", "bool", "date", "autodate", "relation", "number":
		return true
	default:
		return false
	}
}

func fieldSupportsTopInsight(field Field) bool {
	switch field.Type {
	case "text", "email", "url", "select", "bool", "relation":
		return true
	default:
		return false
	}
}

func filledRequestBuckets(insightRange InsightsRange) []RequestInsightBucket {
	start := normalizeBucketTime(insightRange.StartedAt, insightRange)
	finished := normalizeBucketTime(insightRange.FinishedAt, insightRange)
	step := time.Duration(insightRange.BucketMinutes) * time.Minute
	out := []RequestInsightBucket{}
	for t := start; !t.After(finished); t = t.Add(step) {
		out = append(out, RequestInsightBucket{Timestamp: t})
	}
	return out
}

func filledCountBuckets(insightRange InsightsRange) []InsightCountBucket {
	start := normalizeBucketTime(insightRange.StartedAt, insightRange)
	finished := normalizeBucketTime(insightRange.FinishedAt, insightRange)
	step := time.Duration(insightRange.BucketMinutes) * time.Minute
	out := []InsightCountBucket{}
	for t := start; !t.After(finished); t = t.Add(step) {
		out = append(out, InsightCountBucket{Timestamp: t})
	}
	return out
}

func normalizeBucketTime(value time.Time, insightRange InsightsRange) time.Time {
	seconds := int64(insightRange.BucketMinutes * 60)
	if seconds <= 0 {
		seconds = 3600
	}
	unix := value.UTC().Unix()
	return time.Unix((unix/seconds)*seconds, 0).UTC()
}

func roundFloat(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return math.Round(value*100) / 100
}
