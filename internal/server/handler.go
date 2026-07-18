// Package server holds the HTTP layer: it decodes requests, enforces
// per-tenant rate limits, routes them to a provider (or the advisor), records
// cost, and maps results and typed errors to responses.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eng-harpreet-singh/llm-gateway/internal/config"
	"github.com/eng-harpreet-singh/llm-gateway/internal/ledger"
	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
	"github.com/eng-harpreet-singh/llm-gateway/internal/ratelimit"
	"github.com/eng-harpreet-singh/llm-gateway/internal/router"
)

// Handler is the HTTP entry point. It decodes requests, enforces per-tenant
// rate limits, delegates provider selection to the router, records cost to the
// ledger, and maps results and typed errors to responses.
type Handler struct {
	router       *router.Router
	advisor      *router.Advisor
	limiter      *ratelimit.Limiter
	ledger       *ledger.Ledger
	counter      router.TokenCounter
	models       config.Models
	currency     string // for cost endpoint responses
	logger       *slog.Logger
	defaultModel string
}

func NewHandler(
	r *router.Router,
	advisor *router.Advisor,
	limiter *ratelimit.Limiter,
	l *ledger.Ledger,
	counter router.TokenCounter,
	models config.Models,
	currency string,
	logger *slog.Logger,
	defaultModel string,
) *Handler {
	return &Handler{
		router:       r,
		advisor:      advisor,
		limiter:      limiter,
		ledger:       l,
		counter:      counter,
		models:       models,
		currency:     currency,
		logger:       logger,
		defaultModel: defaultModel,
	}
}

// Routes registers handlers on a stdlib ServeMux (Go 1.22+ method+path patterns).
func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("POST /v1/messages", h.messages)
	mux.HandleFunc("POST /v1/advise", h.advise) // cost/tier advisory
	mux.HandleFunc("GET /v1/cost", h.cost)
	return mux
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type messagesRequest struct {
	Model       string             `json:"model"`
	Messages    []provider.Message `json:"messages"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature"`
	Stream      bool               `json:"stream"`
	Fallback    string             `json:"fallback"` // "" / "none" (default) or "same-tier"
}

func (h *Handler) messages(w http.ResponseWriter, r *http.Request) {
	// Tenant is mandatory: every caller must identify itself so we can meter
	// per-tenant. No tenant = reject before doing any work.
	tenant := r.Header.Get("X-Tenant-ID")
	if tenant == "" {
		writeError(w, http.StatusBadRequest, "X-Tenant-ID header is required")
		return
	}

	var body messagesRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(body.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages must not be empty")
		return
	}

	model := body.Model
	if model == "" {
		model = h.defaultModel
	}
	model = strings.ToLower(strings.TrimSpace(model)) // clean once, used for routing and the upstream call

	req := provider.Request{
		Model:       model,
		Messages:    body.Messages,
		MaxTokens:   body.MaxTokens,
		Temperature: body.Temperature,
	}

	// Count input tokens for the pre-flight TPM check. If counting fails, treat
	// it as zero input rather than blocking (the check just under-counts once).
	inputTokens, err := h.counter.CountRequest(r.Context(), req)
	if err != nil {
		h.logger.Warn("token count failed, proceeding", "error", err)
		inputTokens = 0
	}

	// Rate-limit gate: block before spending on the upstream call if the tenant
	// is over its request or token budget for this minute.
	if d := h.limiter.Check(r.Context(), tenant, inputTokens); !d.Allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(d.RetryAfter.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, d.Reason)
		return
	}

	// Streaming path: if the caller asked for SSE, resolve the provider, check
	// it supports streaming, and relay chunks. Non-streaming falls through.
	if body.Stream {
		h.streamMessages(w, r, req, tenant)
		return
	}

	// Try the requested model. On a provider-side failure, and only if the
	// caller opted in, retry once on a same-tier model from another provider.
	resp, err := h.complete(r.Context(), req)
	if err != nil {
		if body.Fallback == "same-tier" && isRetriable(err) {
			if alt := h.router.Fallback(h.models, req.Model); alt != "" {
				h.logger.Warn("primary provider failed, trying fallback", "from", req.Model, "to", alt, "error", err)
				req.Model = alt
				resp, err = h.complete(r.Context(), req)
			}
		}
		if err != nil {
			// Unknown model is a client error (400); provider errors map by type.
			if errors.Is(err, router.ErrNoProvider) {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			h.handleProviderError(w, err)
			return
		}
	}

	// Reconcile the ACTUAL tokens used (input + output) against the model that
	// served, so the next check sees the true running total.
	h.limiter.Record(r.Context(), tenant, resp.Usage.InputTokens+resp.Usage.OutputTokens)

	// Record cost against the model that actually served (resp.Model), so a
	// fallback bills the fallback model, not the failed one.
	h.ledger.Record(ledger.Entry{
		TenantID:     tenant,
		Model:        resp.Model,
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
		Cost:         h.costFor(resp.Model, resp.Usage.InputTokens, resp.Usage.OutputTokens),
	})

	writeJSON(w, http.StatusOK, resp)
}

// complete routes a request and runs the provider call. Shared by the primary
// attempt and the fallback attempt.
func (h *Handler) complete(ctx context.Context, req provider.Request) (provider.Response, error) {
	p, err := h.router.Route(req)
	if err != nil {
		return provider.Response{}, err
	}
	return p.Complete(ctx, req)
}

// isRetriable reports whether an error is worth a fallback attempt. Provider
// outages and rate limits are; a bad request is not (it fails everywhere).
func isRetriable(err error) bool {
	return errors.Is(err, provider.ErrUpstreamUnavailable) || errors.Is(err, provider.ErrRateLimited)
}

// streamMessages handles the SSE path. It routes to a provider, checks that
// provider supports streaming, and relays chunks to the client, flushing each
// one so tokens appear as they arrive.
func (h *Handler) streamMessages(w http.ResponseWriter, r *http.Request, req provider.Request, tenant string) {
	// Resolve the provider (same routing as non-streaming).
	p, err := h.router.Route(req)
	if err != nil {
		if errors.Is(err, router.ErrNoProvider) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.logger.Error("routing failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Not every provider streams. If this one doesn't, say so clearly rather
	// than silently falling back to a buffered response.
	streamer, ok := p.(provider.Streamer)
	if !ok {
		writeError(w, http.StatusBadRequest, "streaming not supported for this model")
		return
	}

	// We need http.Flusher to push each chunk immediately. If the writer can't
	// flush, streaming is impossible on this connection.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	// Start the upstream stream before writing headers, so an upstream error
	// still lets us return a normal error status.
	chunks, err := streamer.Stream(r.Context(), req)
	if err != nil {
		h.handleProviderError(w, err)
		return
	}

	// SSE response headers. Once these are set and flushed, the status is
	// committed — any later error can only end the stream, not change status.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Relay each chunk and flush immediately so the client sees tokens live.
	for chunk := range chunks {
		if chunk.Err != nil {
			h.logger.Error("stream error", "error", chunk.Err)
			return // mid-stream failure: stop; status already sent
		}
		if _, err := w.Write(chunk.Data); err != nil {
			h.logger.Warn("client write failed, ending stream", "error", err)
			return // client went away
		}
		flusher.Flush()
	}
	// Note: cost + TPM Record are skipped in v1 streaming — token usage comes in
	// the final SSE event, which pass-through doesn't parse yet. Reconcile when
	// we add event translation.
}

// costFor computes total cost (input + output) for a model from the catalogue.
func (h *Handler) costFor(model string, inputTokens, outputTokens int) float64 {
	for _, m := range h.models {
		if m.Model == model {
			inCost := float64(inputTokens) * m.PricePer1MInput / 1_000_000
			outCost := float64(outputTokens) * m.PricePer1MOutput / 1_000_000
			return inCost + outCost
		}
	}
	return 0
}

// adviseRequest is the body for the advisory endpoint: just the prompt. No
// model is needed — the whole point is that we recommend one.
type adviseRequest struct {
	Messages []provider.Message `json:"messages"`
}

// advise returns a cost/tier recommendation for a prompt WITHOUT calling a
// model to answer it. The caller (or a UI) uses this to choose a model before
// committing to the spend. First call in the two-step flow: advise here, then
// execute via /v1/messages with the chosen model.
func (h *Handler) advise(w http.ResponseWriter, r *http.Request) {
	var body adviseRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(body.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages must not be empty")
		return
	}

	// model left empty: advise is about choosing one, not using one
	req := provider.Request{Messages: body.Messages}

	advice, err := h.advisor.Advise(r.Context(), req)
	if err != nil {
		h.logger.Error("advise failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not produce advice")
		return
	}

	writeJSON(w, http.StatusOK, advice)
}

// map typed provider errors to HTTP status via errors.Is (not string matching)
func (h *Handler) handleProviderError(w http.ResponseWriter, err error) {
	h.logger.Error("provider call failed", "error", err)

	switch {
	case errors.Is(err, provider.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "upstream rate limited")
	case errors.Is(err, provider.ErrUpstreamUnavailable):
		writeError(w, http.StatusBadGateway, "upstream unavailable")
	case errors.Is(err, provider.ErrInvalidRequest):
		writeError(w, http.StatusBadRequest, "invalid request to upstream")
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// cost returns a tenant's total spend since an optional ?since= time
// (RFC3339). Defaults to the last 30 days if not given.
func (h *Handler) cost(w http.ResponseWriter, r *http.Request) {
	tenant := r.URL.Query().Get("tenant_id")
	if tenant == "" {
		writeError(w, http.StatusBadRequest, "tenant_id query param is required")
		return
	}

	// Default window is the last 30 days; override with ?since=RFC3339.
	since := time.Now().Add(-30 * 24 * time.Hour)
	if s := r.URL.Query().Get("since"); s != "" {
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			writeError(w, http.StatusBadRequest, "since must be RFC3339, e.g. 2026-06-10T00:00:00Z")
			return
		}
		since = parsed
	}

	total, err := h.ledger.TenantSpend(r.Context(), tenant, since)
	if err != nil {
		h.logger.Error("cost query failed", "tenant", tenant, "error", err)
		writeError(w, http.StatusInternalServerError, "could not fetch cost")
		return
	}

	writeJSON(w, http.StatusOK, costResponse{
		TenantID:  tenant,
		Since:     since,
		TotalCost: total,
		Currency:  h.currency,
	})
}

// costResponse is the body for the cost endpoint.
type costResponse struct {
	TenantID  string    `json:"tenant_id"`
	Since     time.Time `json:"since"`
	TotalCost float64   `json:"total_cost"`
	Currency  string    `json:"currency"`
}