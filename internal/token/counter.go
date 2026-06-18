// Package tokens counts how many tokens a request will use before we send it.
// Design: Strategy pattern behind one Counter interface (SOLID: Dependency Inversion).
package token


import(
	"context"
	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
)

type Counter interface {
	Name() string
	CountRequest(ctx context.Context, req provider.Request) (int, error)
}
