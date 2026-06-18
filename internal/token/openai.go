package token

import (
	"context"
	"fmt"
	"sync"

	"github.com/pkoukk/tiktoken-go"

	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
)

// OpenAICounter counts locally with tiktoken (no network call).
type OpenAICounter struct {
	mu sync.Mutex
	encoders map[string]*tiktoken.Tiktoken // cache one encoder per model
}


func NewOpenAICounter() *OpenAICounter {
	return &OpenAICounter{encoders: make(map[string]*tiktoken.Tiktoken)}
}

func (c *OpenAICounter) Name() string {return "openai"}

// encoderFor caches the encoder per model since loading it is expensive.
func (c *OpenAICounter) encoderFor(model string) (*tiktoken.Tiktoken, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if enc, ok := c.encoders[model]; ok {
		return enc, nil
	}

	enc, err := tiktoken.EncodingForModel(model)

	if err != nil {
		// unknown model name: fall back to a common encoding
		enc, err = tiktoken.GetEncoding("cl100k_base")
		if err != nil {
			return nil, fmt.Errorf("tokens: get encoding: %w", err)
		}
	}

	c.encoders[model] = enc
	return enc, nil
}

func (c *OpenAICounter) CountRequest(ctx context.Context, req provider.Request) (int, error){
	enc, err := c.encoderFor(req.Model)
	if err != nil {
		return 0, err
	}

	total := 0

	for _, m := range req.Messages {
		total += 4 // rough per-message overhead (role markers, formatting)
		total += len(enc.Encode(m.Content, nil, nil))
		total += len(enc.Encode(string(m.Role), nil, nil))
	}

	total += 3 // reply priming
	return total, nil
}