// Package router maps a request's model name to the provider that serves it.
// This is the seam where smart, cost-based routing lands later.
package router

import (
	"errors"
	"fmt"
	"strings"

	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
)

// ErrNoProvider means the model maps to no registered provider.
// We error instead of silently defaulting, so a typo can't bill the wrong model.
var ErrNoProvider = errors.New("router: no provider for model")

// Router resolves a request to a Provider.
type Router struct {
	providers map[string]provider.Provider // keyed by provider name
}

// New builds a Router from the given providers.
func New(providers ...provider.Provider) (*Router, error) {
	if len(providers) == 0 {
		return nil, fmt.Errorf("router: at least one provider is required")
	}
	m := make(map[string]provider.Provider, len(providers))
	for _, p := range providers {
		m[p.Name()] = p
	}
	return &Router{providers: m}, nil
}

// Route picks the Provider for a request by its model name.
// Returns ErrNoProvider on empty, unknown, or unregistered models — no silent fallback.
func (r *Router) Route(req provider.Request) (provider.Provider, error) {
	model := normalizeModel(req.Model)
	if model == "" {
		return nil, fmt.Errorf("%w: model is empty", ErrNoProvider)
	}

	name := providerNameForModel(model)
	if name == "" {
		return nil, fmt.Errorf("%w: unknown model %q", ErrNoProvider, req.Model)
	}

	p, ok := r.providers[name]
	if !ok {
		// Model is known but its provider was not wired in.
		return nil, fmt.Errorf("%w: provider %q for model %q not registered", ErrNoProvider, name, req.Model)
	}
	return p, nil
}

// normalizeModel trims and lowercases, so " GPT-4o " and "gpt-4o" match.
func normalizeModel(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

// providerNameForModel maps a model to its provider by prefix.
// Prefix matching lets new model versions route without code changes.
func providerNameForModel(model string) string {
	switch {
	case strings.HasPrefix(model, "gpt"),
		strings.HasPrefix(model, "o1"),
		strings.HasPrefix(model, "o3"):
		return provider.NameOpenAI
	case strings.HasPrefix(model, "claude"):
		return provider.NameAnthropic
	case strings.HasPrefix(model, "llama"),
		strings.HasPrefix(model, "mistral"),
		strings.HasPrefix(model, "gemma"),
		strings.HasPrefix(model, "qwen"),
		strings.HasPrefix(model, "phi"):
		return provider.NameOllama
	default:
		return "" // unknown model family
	}
}