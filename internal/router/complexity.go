package router

import (
	"strings"

	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
)

// Tier is a model class. We keep two: an affordable, fast model and a
// premium, more capable one. Most requests are fine on the affordable tier;
// only harder ones need the premium tier.
type Tier string

const (
	TierAffordable Tier = "affordable"
	TierPremium    Tier = "premium"
)

// Recommendation is the scorer's output: which tier we suggest and why.
// The reason is included so a caller (or an advisory UI) can show the user
// what drove the decision.
type Recommendation struct {
	Tier   Tier
	Reason string
}

// ComplexityScorer judges how hard a request is and recommends a tier.
// It's an interface so the heuristic can be swapped later (e.g. for a
// classifier) without touching the router.
type ComplexityScorer interface {
	Score(req provider.Request) Recommendation
}

// HeuristicScorer recommends a tier from two cheap, free signals:
//   1. length  — longer prompts are more likely to need the premium model
//   2. keywords — words like "analyze" or "prove" signal harder reasoning
//
// Both signals are read straight from the request. No model call, no network.
type HeuristicScorer struct {
	// tokenThreshold is the prompt size (in approx tokens) above which we
	// escalate to the premium tier on length alone.
	tokenThreshold int

	// signalWords are lowercase markers of a harder request. Presence of any
	// one escalates to the premium tier.
	signalWords []string
}

// NewHeuristicScorer builds a scorer. Threshold and words come from config so
// they can be tuned without a recompile.
func NewHeuristicScorer(tokenThreshold int, signalWords []string) *HeuristicScorer {
	// store words lowercased once, so Score doesn't lowercase them every call
	lowered := make([]string, len(signalWords))
	for i, w := range signalWords {
		lowered[i] = strings.ToLower(w)
	}
	return &HeuristicScorer{
		tokenThreshold: tokenThreshold,
		signalWords:    lowered,
	}
}

// Score applies the two signals. Either one is enough to escalate — we'd
// rather pay for the premium model than return a weak answer on a hard request.
func (s *HeuristicScorer) Score(req provider.Request) Recommendation {
	text := joinMessages(req)

	// signal 1: a keyword that marks harder reasoning
	if word, found := s.firstSignalWord(text); found {
		return Recommendation{
			Tier:   TierPremium,
			Reason: "request contains a complex-reasoning keyword: " + word,
		}
	}

	// signal 2: a long prompt. We use a rough token estimate here (~4 chars
	// per token) rather than a real tokenizer call, because this decision must
	// stay cheap. An exact count isn't needed to compare against a threshold.
	if approxTokens(text) > s.tokenThreshold {
		return Recommendation{
			Tier:   TierPremium,
			Reason: "prompt is long enough to likely need the premium model",
		}
	}

	return Recommendation{
		Tier:   TierAffordable,
		Reason: "short prompt with no complex signals",
	}
}

// firstSignalWord returns the first signal word found in the text, if any.
func (s *HeuristicScorer) firstSignalWord(text string) (string, bool) {
	lower := strings.ToLower(text)
	for _, w := range s.signalWords {
		if strings.Contains(lower, w) {
			return w, true
		}
	}
	return "", false
}

// joinMessages flattens a request's messages into one string for scanning.
func joinMessages(req provider.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content)
		b.WriteByte(' ')
	}
	return b.String()
}

// approxTokens is a cheap token estimate (~4 chars per token). Deliberately
// rough: we only need it to compare against a threshold, not to bill anyone.
func approxTokens(text string) int {
	return len(text) / 4
}