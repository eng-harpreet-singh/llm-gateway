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



// OpenAIProvider calls the OpenAI REST API directly via a shared http.Client.
// We use net/http, not the SDK, to control connection pooling and timeouts —
// the levers that govern tail latency under load.
type OpenAIProvider struct {
	apiKey string
	baseURL string
	client *http.Client
}


// Functional options — keeps the constructor stable as config grows.
type OpenAIOption func(*OpenAIProvider)

// WithHTTPClient injects a client (used in tests to stub the transport).
func WithHTTPClient(c *http.Client) OpenAIOption {
	return func(p *OpenAIProvider) {p.client = c}
}

func NewOpenAIProvider(apiKey, baseURL string, opts ...OpenAIOption) *OpenAIProvider {
	p := &OpenAIProvider {
		apiKey: apiKey,
		baseURL: baseURL,
		client: &http.Client {
			Timeout: 30 * time.Second,
			Transport: &http.Transport {
				// Default MaxIdleConnsPerHost is 2 — far too low for a gateway
				// hitting one upstream. Low value = constant TCP/TLS handshakes
				// = tail latency. Raise it so connections get reused.
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

func (p *OpenAIProvider) Name() string { return "openai" }

// Private wire DTOs — OpenAI's shape never leaks past this file.
type openAIMessage struct {
	Role string `json:"role"`
	Content string `json:"content"`
}

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
}

type openAIResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *OpenAIProvider) Complete(ctx context.Context, req Request) (Response, error) {
	// gateway Request -> OpenAI format
	body, err := json.Marshal(toOpenAIRequest(req))
	if err != nil {
		return Response{}, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL + "/chat/completions", bytes.NewReader(body),
    )

	if err != nil {
		return Response{}, fmt.Errorf("openai: build request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		// dial/timeout/ctx-cancel — wrap as unavailable so callers can errors.Is it
		return Response{}, fmt.Errorf("openai: do request: %w: %v", ErrUpstreamUnavailable, err)
	}

	defer resp.Body.Close()

	// cap the read so a bad upstream can't blow up memory
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MiB

	if err != nil {
		return Response{}, fmt.Errorf("openai: read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, newAPIError(p.Name(), resp.StatusCode, extractErrorMessage(raw))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, newAPIError(p.Name(), resp.StatusCode, extractErrorMessage(raw))
	}

	var oa openAIResponse
	if err := json.Unmarshal(raw, &oa); err != nil {
		return Response{}, fmt.Errorf("openai: decode response: %w", err)
	}
	if len(oa.Choices) == 0 {
		return Response{}, fmt.Errorf("openai: %w: empty choices", ErrUpstreamError)
	}

	// OpenAI format -> gateway Response
	return Response{
		Content:  oa.Choices[0].Message.Content,
		Model:    oa.Model,
		Provider: p.Name(),
		Usage: Usage{
			InputTokens:  oa.Usage.PromptTokens,
			OutputTokens: oa.Usage.CompletionTokens,
		},
	}, nil
} 



// toOpenAIRequest maps the gateway shape to the vendor shape.
func toOpenAIRequest(req Request) openAIRequest {
	msgs := make([]openAIMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = openAIMessage{Role: string(m.Role), Content: m.Content}
	}
	return openAIRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
}

func extractErrorMessage(raw []byte) string {
	var oa openAIResponse
	if err := json.Unmarshal(raw, &oa); err == nil && oa.Error != nil {
		return oa.Error.Message
	}
	return string(raw)
}

