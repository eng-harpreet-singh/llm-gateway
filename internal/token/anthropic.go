package token

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
)

// AnthropicCounter counts via the count_tokens API (no local tokenizer exists).
type AnthropicCounter struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewAnthropicCounter(apiKey, baseURL string) *AnthropicCounter {
	return &AnthropicCounter{
		apiKey:  apiKey,
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (c *AnthropicCounter) Name() string { return "anthropic" }

// wire DTOs for the count_tokens endpoint
type anthropicCountMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicCountRequest struct {
	Model    string                  `json:"model"`
	System   string                  `json:"system,omitempty"`
	Messages []anthropicCountMessage `json:"messages"`
}

type anthropicCountResponse struct {
	InputTokens int `json:"input_tokens"`
}

// CountRequest calls Anthropic's count_tokens endpoint for an exact count.
func (c *AnthropicCounter) CountRequest(ctx context.Context, req provider.Request) (int, error) {
	// system prompt is a top-level field in Anthropic, so pull it out of messages
	var system string
	msgs := make([]anthropicCountMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == provider.RoleSystem {
			system = m.Content
			continue
		}
		msgs = append(msgs, anthropicCountMessage{Role: string(m.Role), Content: m.Content})
	}

	body, err := json.Marshal(anthropicCountRequest{
		Model:    req.Model,
		System:   system,
		Messages: msgs,
	})
	if err != nil {
		return 0, fmt.Errorf("tokens: marshal count request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.baseURL+"/v1/messages/count_tokens", bytes.NewReader(body),
	)
	if err != nil {
		return 0, fmt.Errorf("tokens: build count request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("tokens: count request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, fmt.Errorf("tokens: read count response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("tokens: count_tokens status %d: %s", resp.StatusCode, string(raw))
	}

	var out anthropicCountResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, fmt.Errorf("tokens: decode count response: %w", err)
	}
	return out.InputTokens, nil
}