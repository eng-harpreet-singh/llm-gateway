// Package ledger records per-request cost to Postgres for cost observability.
// Writes are async: Record enqueues and returns, a worker drains the channel.
package ledger

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// writeTimeout caps a single Postgres write so a slow DB can't stall the worker.
const writeTimeout = 2 * time.Second

// bufferSize bounds pending entries. On a full buffer we drop, not block.
const bufferSize = 1000

// Entry is one cost record: who spent what, on which model.
type Entry struct {
	TenantID     string
	Model        string
	InputTokens  int
	OutputTokens int
	Cost         float64 // total input+output cost in the configured currency
}

// Ledger records cost entries async. Record enqueues; a worker writes.
type Ledger struct {
	pool    *pgxpool.Pool
	logger  *slog.Logger
	entries chan Entry    // buffered queue of pending writes
	done    chan struct{} // closed once the worker has drained and exited
	dropped int           // entries dropped on a full buffer, for observability
}

// New builds a Ledger and starts its worker. Call Close on shutdown to drain.
func New(pool *pgxpool.Pool, logger *slog.Logger) *Ledger {
	l := &Ledger{
		pool:    pool,
		logger:  logger,
		entries: make(chan Entry, bufferSize),
		done:    make(chan struct{}),
	}
	go l.worker()
	return l
}

// Record enqueues an entry without blocking. Full buffer = drop + count, since
// cost tracking is best-effort and must never slow a request.
func (l *Ledger) Record(e Entry) {
	select {
	case l.entries <- e:
	default:
		l.dropped++
		l.logger.Warn("ledger: buffer full, dropping cost entry", "tenant", e.TenantID, "total_dropped", l.dropped)
	}
}

// worker drains the channel until it closes, then signals done. The range
// flushes everything still buffered before exiting.
func (l *Ledger) worker() {
	defer close(l.done)
	for e := range l.entries {
		l.write(e)
	}
}

// write persists one entry. Best-effort: log on failure, no retry.
func (l *Ledger) write(e Entry) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	const q = `
		INSERT INTO cost_ledger (tenant_id, model, input_tokens, output_tokens, cost)
		VALUES ($1, $2, $3, $4, $5)`

	if _, err := l.pool.Exec(ctx, q, e.TenantID, e.Model, e.InputTokens, e.OutputTokens, e.Cost); err != nil {
		l.logger.Warn("ledger: failed to record cost", "tenant", e.TenantID, "error", err)
	}
}

// Close stops new entries and waits for the worker to flush the buffer.
func (l *Ledger) Close() {
	close(l.entries)
	<-l.done
}

// TenantSpend returns a tenant's total cost since a given time, for reporting.
func (l *Ledger) TenantSpend(ctx context.Context, tenantID string, since time.Time) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()

	const q = `
		SELECT COALESCE(SUM(cost), 0)
		FROM cost_ledger
		WHERE tenant_id = $1 AND created_at >= $2`

	var total float64
	if err := l.pool.QueryRow(ctx, q, tenantID, since).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}