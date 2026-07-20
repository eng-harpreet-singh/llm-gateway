package router

import (
	"testing"

	"github.com/eng-harpreet-singh/llm-gateway/internal/config"
	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
)

func TestFallback(t *testing.T) {
	// Local catalogue for this test: two "affordable" on different providers,
	// one "premium" with no alternative provider.
	models := config.Models{
		{Tier: "affordable", Provider: provider.NameOpenAI, Model: "gpt-4o-mini"},
		{Tier: "affordable", Provider: provider.NameAnthropic, Model: "claude-haiku-4-5"},
		{Tier: "premium", Provider: provider.NameAnthropic, Model: "claude-sonnet-4-5"},
	}

	// Reuse the package's existing fakeProvider (from router_test.go).
	rtr, err := New(
		fakeProvider{name: provider.NameOpenAI},
		fakeProvider{name: provider.NameAnthropic},
	)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}

	tests := []struct {
		name   string
		failed string
		want   string
	}{
		{"anthropic down → same-tier openai", "claude-haiku-4-5", "gpt-4o-mini"},
		{"openai down → same-tier anthropic", "gpt-4o-mini", "claude-haiku-4-5"},
		{"no same-tier alternative → empty", "claude-sonnet-4-5", ""},
		{"unknown model → empty", "does-not-exist", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := rtr.Fallback(models, tc.failed); got != tc.want {
				t.Errorf("Fallback(%q) = %q, want %q", tc.failed, got, tc.want)
			}
		})
	}
}