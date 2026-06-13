package router

import (
	"fmt"

	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
)

// Router picks which Provider handles a request. Trivial today (single
// provider), but it's the seam where smart routing lands later.
type Router struct {
	providers map[string]provider.Provider
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

// Route returns the Provider for a request. Future: pick by complexity/cost/tenant.
func (r *Router) Route(req provider.Request) provider.Provider {
	if p, ok := r.providers[req.Model]; ok {
		return p
	}
	return r.defaultP
}