// Package router selects which Provider handles a given request.
//
// It maps the request's model name to the provider that serves it. This is the
// seam where smart, complexity-based routing lands later (cheap model first,
// escalate on signal); for now it routes purely by model name.
package router

import (
	"fmt"
	"strings"

	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
)

// Router resolves a request to a Provider.
type Router struct {
	providers map[string]provider.Provider // keyed by provider name
	defaultP  provider.Provider
}

// New builds a Router. The first provider registered becomes the default.
func New(providers ...provider.Provider) (*Router, error) {
	if len(providers) == 0 {
		return nil, fmt.Errorf("router: at least one provider is required")
	}
	m := make(map[string]provider.Provider, len(providers))
	for _, p := range providers {
		m[p.Name()] = p
	}
	return &Router{
		providers: m,
		defaultP:  providers[0],
	}, nil
}

// Route returns the Provider for a request, based on its model name.
func (r *Router) Route(req provider.Request) provider.Provider {
	name := providerNameForModel(req.Model)
	if p, ok := r.providers[name]; ok {
		return p
	}
	return r.defaultP
}

// providerNameForModel maps a model name to the provider that serves it.
// Uses prefix matching so new model versions route without code changes.
func providerNameForModel(model string) string {
	switch {
	case strings.HasPrefix(model, "gpt"),
		strings.HasPrefix(model, "o1"),
		strings.HasPrefix(model, "o3"):
		return "openai"
	case strings.HasPrefix(model, "claude"):
		return "anthropic"
	case strings.HasPrefix(model, "llama"),
		strings.HasPrefix(model, "mistral"),
		strings.HasPrefix(model, "gemma"),
		strings.HasPrefix(model, "qwen"),
		strings.HasPrefix(model, "phi"):
		return "ollama"
	default:
		return "" // unknown: no match, falls back to default
	}
}