package core

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RequestLogWriter records request logs off the request path.
//
// The insert used to run inside the handler, so every API call paid for a
// blocking write and held a second connection while doing it. Under concurrent
// load those inserts reached their timeout, adding the full timeout to requests
// that had already produced their response.
//
// Events are handed to a buffered channel and written in batches. A request
// never waits for the database, and never blocks: if the buffer is full the
// event is dropped and counted, because losing an access-log line is a far
// smaller problem than stalling the request it describes.
type RequestLogWriter struct {
	pool    *pgxpool.Pool
	events  chan RequestLogEvent
	dropped atomic.Int64
	written atomic.Int64
	wg      sync.WaitGroup
	stop    chan struct{}
	once    sync.Once
}

const (
	requestLogBuffer    = 4096
	requestLogBatchSize = 250
	requestLogFlushIdle = 500 * time.Millisecond
)

func NewRequestLogWriter(pool *pgxpool.Pool) *RequestLogWriter {
	w := &RequestLogWriter{
		pool:   pool,
		events: make(chan RequestLogEvent, requestLogBuffer),
		stop:   make(chan struct{}),
	}
	if pool != nil {
		w.wg.Add(1)
		go w.run()
	}
	return w
}

// Record queues an event. It never blocks and never returns an error, so a
// caller in a request handler has nothing to wait for or handle.
func (w *RequestLogWriter) Record(event RequestLogEvent) {
	if w == nil || w.pool == nil {
		return
	}
	select {
	case w.events <- event:
	default:
		w.dropped.Add(1)
	}
}

// Stats reports what the writer has done, for the ops endpoint and tests.
func (w *RequestLogWriter) Stats() (written, dropped int64) {
	if w == nil {
		return 0, 0
	}
	return w.written.Load(), w.dropped.Load()
}

// Close drains what is buffered and stops the writer.
func (w *RequestLogWriter) Close() {
	if w == nil || w.pool == nil {
		return
	}
	w.once.Do(func() { close(w.stop) })
	w.wg.Wait()
}

func (w *RequestLogWriter) run() {
	defer w.wg.Done()
	batch := make([]RequestLogEvent, 0, requestLogBatchSize)
	timer := time.NewTimer(requestLogFlushIdle)
	defer timer.Stop()
	flush := func() {
		if len(batch) == 0 {
			return
		}
		w.writeBatch(batch)
		batch = batch[:0]
	}
	for {
		select {
		case event := <-w.events:
			batch = append(batch, event)
			if len(batch) >= requestLogBatchSize {
				flush()
			}
		case <-timer.C:
			flush()
			timer.Reset(requestLogFlushIdle)
		case <-w.stop:
			// Take whatever is still queued before going away.
			for {
				select {
				case event := <-w.events:
					batch = append(batch, event)
					if len(batch) >= requestLogBatchSize {
						flush()
					}
					continue
				default:
				}
				break
			}
			flush()
			return
		}
	}
}

func (w *RequestLogWriter) writeBatch(events []RequestLogEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	batch := &pgx.Batch{}
	queued := 0
	for _, event := range events {
		method := strings.ToUpper(strings.TrimSpace(event.Method))
		path := strings.TrimSpace(event.Path)
		if method == "" || path == "" {
			continue
		}
		if len(path) > 1000 {
			path = path[:1000]
		}
		metadata := []byte(`{}`)
		if event.Metadata != nil {
			if encoded, err := json.Marshal(redactAuditData(event.Metadata)); err == nil {
				metadata = encoded
			}
		}
		batch.Queue(`
			insert into _dbo.request_logs
				(project_id, project_slug, method, path, status, duration_ms, ip, user_agent, request_id, error, metadata)
			values (
				(select id from _dbo.projects where slug = nullif($1, '')),
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb
			)`,
			NormalizeProjectSlug(event.ProjectSlug), method, path, event.Status, event.DurationMS,
			truncateString(event.IP, 200), truncateString(event.UserAgent, 500),
			truncateString(event.RequestID, 200), truncateString(event.Error, 2000), metadata)
		queued++
	}
	if queued == 0 {
		return
	}
	results := w.pool.SendBatch(ctx, batch)
	defer results.Close()
	for i := 0; i < queued; i++ {
		if _, err := results.Exec(); err != nil {
			// One bad row must not discard the rest of the batch.
			continue
		}
		w.written.Add(1)
	}
}
