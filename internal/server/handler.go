package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
	"github.com/eng-harpreet-singh/llm-gateway/internal/router"
)

// Handler holds injected deps so it's testable (no globals).
type Handler struct {
	router       *router.Router
	advisor      *router.Advisor
	logger       *slog.Logger
	defaultModel string
}

func NewHandler(r *router.Router, advisor *router.Advisor, logger *slog.Logger, defaultModel string) *Handler {
	return &Handler{router: r, advisor: advisor, logger: logger, defaultModel: defaultModel}
}

// Routes registers handlers on a stdlib ServeMux (Go 1.22+ method+path patterns).
func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("POST /v1/messages", h.messages)
	mux.HandleFunc("POST /v1/advise", h.advise) // cost/tier advisory
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
}

func (h *Handler) messages(w http.ResponseWriter, r *http.Request) {
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

	req := provider.Request{
		Model:       model,
		Messages:    body.Messages,
		MaxTokens:   body.MaxTokens,
		Temperature: body.Temperature,
	}

	// pass r.Context() so client disconnect/deadline cancels the upstream call
	p := h.router.Route(req)
	resp, err := p.Complete(r.Context(), req)
	if err != nil {
		h.handleProviderError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
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