// Package ratelimit enforces per-tenant request and token limits using Redis.
// It fails open: if Redis is unreachable, requests are allowed and the failure
// is logged, so a limiter outage never takes down the gateway.
package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// window is the length of one rate-limit bucket. Limits are per this window.
const window = time.Minute

// redisTimeout caps how long we wait on Redis. If it is slow or down we fail
// open fast, rather than adding latency to every request.
const redisTimeout = 100 * time.Millisecond

// Decision is the result of a limit check.
type Decision struct {
	Allowed    bool
	Reason     string        // which limit was hit (for the client message)
	RetryAfter time.Duration // how long until the window resets
}

// Limiter checks and records per-tenant request and token usage.
type Limiter struct {
	rdb      *redis.Client
	logger   *slog.Logger
	rpmLimit int // max requests per window per tenant
	tpmLimit int // max tokens per window per tenant
	failures int // count of Redis failures (fail-open events) for observability
}

// New builds a Limiter. The caller owns the Redis client lifecycle.
func New(rdb *redis.Client, logger *slog.Logger, rpmLimit, tpmLimit int) *Limiter {
	return &Limiter{rdb: rdb, logger: logger, rpmLimit: rpmLimit, tpmLimit: tpmLimit}
}

// Check decides whether a request is allowed, based on the tenant's request
// count and token usage so far this window. inputTokens is the pre-flight
// estimate we gate on before spending money on the upstream call.
func (l *Limiter) Check(ctx context.Context, tenant string, inputTokens int) Decision {
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	bucket := currentBucket()
	rpmKey := fmt.Sprintf("rpm:%s:%d", tenant, bucket)
	tpmKey := fmt.Sprintf("tpm:%s:%d", tenant, bucket)

	// Count this request first. INCR returns the new count; on the first call
	// we set the key's TTL so the bucket auto-expires (no manual cleanup).
	rpmCount, err := l.incr(ctx, rpmKey)
	if err != nil {
		return l.failOpen("rpm incr", err)
	}
	if rpmCount > int64(l.rpmLimit) {
		return Decision{Allowed: false, Reason: "requests per minute exceeded", RetryAfter: untilNextWindow()}
	}

	// Read tokens used so far; block if adding this request's input would go
	// over. We gate on input tokens (known now); actual usage is reconciled in
	// Record after the call.
	tpmUsed, err := l.getInt(ctx, tpmKey)
	if err != nil {
		return l.failOpen("tpm get", err)
	}
	if tpmUsed+inputTokens > l.tpmLimit {
		return Decision{Allowed: false, Reason: "tokens per minute exceeded", RetryAfter: untilNextWindow()}
	}

	return Decision{Allowed: true}
}

// Record adds the actual tokens used (input + output) to the tenant's window
// total, after the upstream call. This reconciles the estimate from Check with
// real usage, so the next Check sees the true running total.
func (l *Limiter) Record(ctx context.Context, tenant string, totalTokens int) {
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	tpmKey := fmt.Sprintf("tpm:%s:%d", tenant, currentBucket())
	if err := l.incrByWithTTL(ctx, tpmKey, totalTokens); err != nil {
		// Recording is best-effort; a miss only under-counts one request.
		l.logger.Warn("ratelimit: record failed", "tenant", tenant, "error", err)
	}
}

// incr increments a key and ensures it has a TTL, returning the new count.
func (l *Limiter) incr(ctx context.Context, key string) (int64, error) {
	n, err := l.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		// first write to this bucket: set expiry a bit past the window so
		// late reads still see it, then it disappears on its own.
		l.rdb.Expire(ctx, key, window+time.Second)
	}
	return n, nil
}

// incrByWithTTL adds n to a key and ensures a TTL is set.
func (l *Limiter) incrByWithTTL(ctx context.Context, key string, n int) error {
	newVal, err := l.rdb.IncrBy(ctx, key, int64(n)).Result()
	if err != nil {
		return err
	}
	if newVal == int64(n) {
		l.rdb.Expire(ctx, key, window+time.Second)
	}
	return nil
}

// getInt reads an integer key, returning 0 if the key is absent.
func (l *Limiter) getInt(ctx context.Context, key string) (int, error) {
	v, err := l.rdb.Get(ctx, key).Int()
	if err == redis.Nil {
		return 0, nil // no usage recorded yet this window
	}
	if err != nil {
		return 0, err
	}
	return v, nil
}

// failOpen logs a Redis failure, counts it, and allows the request through.
// Rate limiting is protective, not correctness-critical, so a limiter outage
// must not fail the request.
func (l *Limiter) failOpen(op string, err error) Decision {
	l.failures++
	l.logger.Warn("ratelimit: redis unavailable, failing open", "op", op, "error", err, "total_failures", l.failures)
	return Decision{Allowed: true}
}

// currentBucket returns the current window as a unix-minute number, used in
// the Redis key so each minute is a separate bucket.
func currentBucket() int64 {
	return time.Now().Unix() / int64(window.Seconds())
}

// untilNextWindow returns the time left until the current window resets, for
// the Retry-After header.
func untilNextWindow() time.Duration {
	now := time.Now()
	next := now.Truncate(window).Add(window)
	return next.Sub(now)
}