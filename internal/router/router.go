package router

import (
	"fmt"
	"strings"

	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
)

// Router picks which Provider handles a request, based on the model name.
type Router struct {
	providers map[string]provider.Provider // keyed by provider name
	defaultP  provider.Provider
}

// New builds a Router. First provider registered is the default.
func New(providers ...provider.Provider) (*Router, error) {
	if len(providers) == 0 {
		return nil, fmt.Errorf("router: at least one provider is required")
	}
	m := make(map[string]provider.Provider, len(providers))
	for _, p := range providers {
		m[p.Name()] = p
	}
	return &Router{providers: m, defaultP: providers[0]}, nil
}

// Route picks a Provider from the model name. Future: complexity/cost/tenant.
func (r *Router) Route(req provider.Request) provider.Provider {
	name := providerNameForModel(req.Model)
	if p, ok := r.providers[name]; ok {
		return p
	}
	return r.defaultP
}

// providerNameForModel maps a model name to the provider that serves it.
func providerNameForModel(model string) string {
	switch {
	case strings.HasPrefix(model, "gpt"), strings.HasPrefix(model, "o1"), strings.HasPrefix(model, "o3"):
		return "openai"
	case strings.HasPrefix(model, "claude"):
		return "anthropic"
	default:
		return "" // unknown → no match → falls back to default in Route
	}
}