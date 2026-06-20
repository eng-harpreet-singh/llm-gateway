package router

import (
	"testing"

	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
)

func TestHeuristicScorer_Score(t *testing.T) {
	signalWords := []string{"analyze", "prove", "explain"}

	cases := []struct {
		name     string
		content  string
		threshold int
		wantTier Tier
	}{
		{
			name:      "short simple prompt -> affordable",
			content:   "hello there",
			threshold: 1000,
			wantTier:  TierAffordable,
		},
		{
			name:      "keyword 'analyze' -> premium",
			content:   "please analyze this contract",
			threshold: 1000,
			wantTier:  TierPremium,
		},
		{
			name:      "keyword is case-insensitive -> premium",
			content:   "ANALYZE this for me",
			threshold: 1000,
			wantTier:  TierPremium,
		},
		{
			name:      "long prompt over threshold -> premium",
			content:   makeLongText(5000), // ~1250 tokens, over threshold
			threshold: 1000,
			wantTier:  TierPremium,
		},
		{
			name:      "no signal, under threshold -> affordable",
			content:   "what is the capital of France",
			threshold: 1000,
			wantTier:  TierAffordable,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			scorer := NewHeuristicScorer(c.threshold, signalWords)
			req := provider.Request{
				Messages: []provider.Message{{Role: provider.RoleUser, Content: c.content}},
			}
			got := scorer.Score(req)
			if got.Tier != c.wantTier {
				t.Errorf("Score() tier = %q, want %q (reason: %s)", got.Tier, c.wantTier, got.Reason)
			}
		})
	}
}

// makeLongText builds a string of roughly n characters for threshold tests.
func makeLongText(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}