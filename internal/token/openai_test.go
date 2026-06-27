package token

import (
	"context"
	"testing"

	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
)

func TestOpenAICounter_CountsTokens(t *testing.T) {
	c := NewOpenAICounter()

	req := provider.Request{
		Model:    "gpt-4o-mini",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "this"}},
	}

	n, err := c.CountRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// "this" is 1 token; plus our per-message (+4) and reply-priming (+3)
	// overhead and the role token. We assert a 	 lower bound rather than
	// an exact number, since overhead constants are approximate.
	if n < 5 {
		t.Errorf("token count = %d, want at least 5 (content + overhead)", n)
	}
	t.Logf("counted %d tokens for a 1-word message", n)
}

func TestOpenAICounter_Name(t *testing.T) {
	if got := NewOpenAICounter().Name(); got != "openai" {
		t.Errorf("Name() = %q, want %q", got, "openai")
	}
}