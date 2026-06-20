package router

import (
	"context"
	"testing"

	"github.com/eng-harpreet-singh/llm-gateway/internal/config"
	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
)

// fakeCounter returns a fixed token count, so advisor tests run offline with
// no real tokenizer or network call. We control the number to make costs
// predictable.
type fakeCounter struct {
	tokens int
}

func (f fakeCounter) CountRequest(ctx context.Context, req provider.Request) (int, error) {
	return f.tokens, nil
}

// testModels is a small catalogue matching the real shape: two tiers, two
// providers each.
func testModels() config.Models {
	return config.Models{
		{Tier: "affordable", Provider: "openai", Model: "gpt-4o-mini", PricePer1MInput: 0.15},
		{Tier: "affordable", Provider: "anthropic", Model: "claude-haiku-4-5", PricePer1MInput: 0.25},
		{Tier: "premium", Provider: "openai", Model: "gpt-4o", PricePer1MInput: 2.50},
		{Tier: "premium", Provider: "anthropic", Model: "claude-sonnet-4-5", PricePer1MInput: 3.00},
	}
}

func TestAdvise_RecommendsTierAndPricesAllModels(t *testing.T) {
	cases := []struct {
		name         string
		prompt       string
		tokens       int
		signalWords  []string
		threshold    int
		wantTier     string
		wantProvider string // cheapest in the recommended tier
		wantModel    string
	}{
		{
			name:         "short simple prompt -> affordable, cheapest = openai",
			prompt:       "hello there",
			tokens:       10,
			signalWords:  []string{"analyze", "prove"},
			threshold:    1000,
			wantTier:     "affordable",
			wantProvider: "openai", // gpt-4o-mini (0.15) cheaper than claude-haiku (0.25)
			wantModel:    "gpt-4o-mini",
		},
		{
			name:         "keyword signal -> premium, cheapest = openai",
			prompt:       "please analyze this contract carefully",
			tokens:       20,
			signalWords:  []string{"analyze", "prove"},
			threshold:    1000,
			wantTier:     "premium",
			wantProvider: "openai", // gpt-4o (2.50) cheaper than claude-sonnet (3.00)
			wantModel:    "gpt-4o",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			scorer := NewHeuristicScorer(c.threshold, c.signalWords)
			counter := fakeCounter{tokens: c.tokens}
			advisor := NewAdvisor(scorer, counter, testModels(), "INR")

			req := provider.Request{
				Model:    "auto",
				Messages: []provider.Message{{Role: provider.RoleUser, Content: c.prompt}},
			}

			advice, err := advisor.Advise(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// token count is passed straight through
			if advice.InputTokens != c.tokens {
				t.Errorf("InputTokens = %d, want %d", advice.InputTokens, c.tokens)
			}

			// recommendation: right tier and cheapest model in it
			if advice.Recommendation.Tier != c.wantTier {
				t.Errorf("recommended tier = %q, want %q", advice.Recommendation.Tier, c.wantTier)
			}
			if advice.Recommendation.Provider != c.wantProvider {
				t.Errorf("recommended provider = %q, want %q", advice.Recommendation.Provider, c.wantProvider)
			}
			if advice.Recommendation.Model != c.wantModel {
				t.Errorf("recommended model = %q, want %q", advice.Recommendation.Model, c.wantModel)
			}

			// every model in the catalogue must appear as an option
			if len(advice.Options) != 4 {
				t.Errorf("got %d options, want 4", len(advice.Options))
			}
		})
	}
}

func TestInputCost(t *testing.T) {
	// 1,000,000 tokens at price 2.50 per 1M = exactly 2.50
	if got := inputCost(1_000_000, 2.50); got != 2.50 {
		t.Errorf("inputCost(1M, 2.50) = %v, want 2.50", got)
	}
	// 0 tokens = 0 cost
	if got := inputCost(0, 2.50); got != 0 {
		t.Errorf("inputCost(0, 2.50) = %v, want 0", got)
	}
}