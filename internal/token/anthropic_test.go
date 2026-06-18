package token

import (
	"context"
	"os"
	"testing"

	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
)

func TestAnthropicCounter_Live(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set — skipping live test")
	}

	c := NewAnthropicCounter(key, "https://api.anthropic.com")
	req := provider.Request{
		Model:    "claude-sonnet-4-5",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "Hello, Claude"}},
	}

	n, err := c.CountRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("live count failed: %v", err)
	}
	t.Logf("Anthropic counted %d tokens", n)
}