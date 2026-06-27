package router

import (
	"context"

	"github.com/eng-harpreet-singh/llm-gateway/internal/config"
	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
)

// TokenCounter is the slice of token-counting we need here: turn a request
// into an exact input-token count. The token package already satisfies this.
// We depend on a small interface (not the concrete counter) so the advisor
// stays testable and decoupled.
type TokenCounter interface {
	CountRequest(ctx context.Context, req provider.Request) (int, error)
}

// Option is one model the caller can pick, with its exact input cost.
type Option struct {
	Tier      string  `json:"tier"`
	Provider  string  `json:"provider"`
	Model     string  `json:"model"`
	InputCost float64 `json:"input_cost"`
	Currency  string  `json:"currency"`
}

// Advice is the advisory response: how big the prompt is, which model we
// recommend, every option with its cost, and an honesty note about output.
type Advice struct {
	InputTokens    int      `json:"input_tokens"`
	Recommendation Advised  `json:"recommendation"`
	Options        []Option `json:"options"`
	Note           string   `json:"note"`
}

// Advised is the single model we suggest, with the reason behind it.
type Advised struct {
	Tier     string `json:"tier"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Reason   string `json:"reason"`
}

// Advisor builds an Advice for a request: it scores complexity to pick a tier,
// counts tokens for exact cost, and prices every model from the catalogue.
type Advisor struct {
	scorer   ComplexityScorer
	counter  TokenCounter
	models   config.Models
	currency string
}

// NewAdvisor wires the advisor with its dependencies. The catalogue and
// currency come from config so nothing here is hardcoded.
func NewAdvisor(scorer ComplexityScorer, counter TokenCounter, models config.Models, currency string) *Advisor {
	return &Advisor{
		scorer:   scorer,
		counter:  counter,
		models:   models,
		currency: currency,
	}
}

// Advise produces the advisory response for a request.
//
// Cost is INPUT only: the output isn't generated yet, so its length is unknown.
// We count input once and price every model against that same count. Note this
// is an estimate across providers — OpenAI and Anthropic tokenize differently,
// so a Claude count would differ slightly; for advisory purposes one count is
// close enough, and we flag it in the note.
func (a *Advisor) Advise(ctx context.Context, req provider.Request) (Advice, error) {
	// exact input-token count (one count, reused to price every model)
	tokens, err := a.counter.CountRequest(ctx, req)
	if err != nil {
		return Advice{}, err
	}

	// which tier do we recommend? (cheap heuristic, no model call)
	rec := a.scorer.Score(req)

	// price every model in the catalogue against the input-token count
	options := make([]Option, 0, len(a.models))
	for _, m := range a.models {
		options = append(options, Option{
			Tier:      m.Tier,
			Provider:  m.Provider,
			Model:     m.Model,
			InputCost: inputCost(tokens, m.PricePer1MInput),
			Currency:  a.currency,
		})
	}

	// pick the recommended model: the cheapest one in the recommended tier
	advised := a.pickRecommended(rec, options)

	return Advice{
		InputTokens:    tokens,
		Recommendation: advised,
		Options:        options,
		Note:           "Input cost only, estimated using one tokenizer. Output is billed separately per response.",
	}, nil
}

// localTier marks models that run on this machine (Ollama). They appear as
// options with zero API cost, but we never auto-recommend them: local models
// are less capable, so the user should choose local deliberately.
const localTier = "local"

// pickRecommended chooses, within the recommended tier, the cheapest model.
// Local-tier models are skipped here — they show as options but are never the
// auto-recommendation. Falls back to the first non-local option if needed.
func (a *Advisor) pickRecommended(rec Recommendation, options []Option) Advised {
	var best *Option
	for i := range options {
		if options[i].Tier == localTier {
			continue // local is an option, never the recommendation
		}
		if options[i].Tier != string(rec.Tier) {
			continue
		}
		if best == nil || options[i].InputCost < best.InputCost {
			best = &options[i]
		}
	}

	// safety net: if the recommended tier matched nothing, pick the cheapest
	// non-local option, so we never recommend the local model by accident.
	if best == nil {
		for i := range options {
			if options[i].Tier == localTier {
				continue
			}
			if best == nil || options[i].InputCost < best.InputCost {
				best = &options[i]
			}
		}
	}

	return Advised{
		Tier:     best.Tier,
		Provider: best.Provider,
		Model:    best.Model,
		Reason:   rec.Reason,
	}
}

// inputCost converts a token count + a per-1M-token price into an actual cost.
func inputCost(tokens int, pricePer1M float64) float64 {
	return float64(tokens) * pricePer1M / 1_000_000
}