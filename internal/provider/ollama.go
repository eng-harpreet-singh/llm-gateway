package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaProvider calls a local Ollama server (default localhost:11434).
// It needs no API key because the model runs on this machine. It uses
// Ollama's own /api/chat format, and a longer timeout since local models
// can be slow to load on the first call.
type OllamaProvider struct {
	baseURL string
	client  *http.Client
}

// NewOllamaProvider builds the provider. No API key needed (local server).
func NewOllamaProvider(baseURL string, opts ...OllamaOption) *OllamaProvider {
	p := &OllamaProvider{
		baseURL: baseURL,
		client: &http.Client{
			// Local models can be slow to load the first time, so give
			// more time than the cloud providers.
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// OllamaOption configures an OllamaProvider.
type OllamaOption func(*OllamaProvider)

// WithOllamaHTTPClient injects a custom client (used in tests to stub transport).
func WithOllamaHTTPClient(c *http.Client) OllamaOption {
	return func(p *OllamaProvider) { p.client = c }
}

func (p *OllamaProvider) Name() string { return NameOllama }

// Request and response types for Ollama's /api/chat endpoint.
// Kept private so they don't leak into the gateway's neutral model.

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

// Ollama puts the reply under "message" and reports tokens as
// prompt_eval_count (input) and eval_count (output).
type ollamaResponse struct {
	Model           string        `json:"model"`
	Message         ollamaMessage `json:"message"`
	Done            bool          `json:"done"`
	PromptEvalCount int           `json:"prompt_eval_count"`
	EvalCount       int           `json:"eval_count"`
}

// Complete sends a chat request to the local Ollama server and maps the
// reply back to the gateway's neutral Response.
func (p *OllamaProvider) Complete(ctx context.Context, req Request) (Response, error) {
	body, err := json.Marshal(p.toOllamaRequest(req))
	if err != nil {
		return Response{}, fmt.Errorf("ollama: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body),
	)
	if err != nil {
		return Response{}, fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// no auth header: local server

	resp, err := p.client.Do(httpReq)
	if err != nil {
		// a transport error here usually means Ollama is not running
		return Response{}, fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Response{}, fmt.Errorf("ollama: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, fmt.Errorf("%w: ollama status %d: %s",
			classifyStatus(resp.StatusCode), resp.StatusCode, string(raw))
	}

	var out ollamaResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Response{}, fmt.Errorf("ollama: decode response: %w", err)
	}

	return Response{
		Provider: p.Name(),
		Model:    out.Model,
		Content:  out.Message.Content,
		Usage: Usage{
			InputTokens:  out.PromptEvalCount,
			OutputTokens: out.EvalCount,
		},
	}, nil
}

// toOllamaRequest maps the gateway request into Ollama's format.
// Stream is false so we get one full JSON object, not a stream.
func (p *OllamaProvider) toOllamaRequest(req Request) ollamaRequest {
	msgs := make([]ollamaMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, ollamaMessage{Role: string(m.Role), Content: m.Content})
	}
	return ollamaRequest{
		Model:    req.Model,
		Messages: msgs,
		Stream:   false,
	}
}