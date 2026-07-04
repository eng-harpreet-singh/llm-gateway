package provider

import (
	"bytes"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AnthropicProvider calls the Anthropic Messages API directly via net/http.
// Same five-step Complete spine as OpenAI — only the wire format and the
// auth headers differ.
type AnthropicProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

type AnthropicOption func(*AnthropicProvider)

func WithAnthropicHTTPClient(c *http.Client) AnthropicOption {
	return func(p *AnthropicProvider) { p.client = c }
}

func NewAnthropicProvider(apiKey, baseURL string, opts ...AnthropicOption) *AnthropicProvider {
	p := &AnthropicProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100, // same reasoning as OpenAI — reuse conns
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *AnthropicProvider) Name() string { return NameAnthropic }

// ---- Anthropic wire DTOs (private to this file) ----

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"` // REQUIRED by Anthropic
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicResponse struct {
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *AnthropicProvider) Complete(ctx context.Context, req Request) (Response, error) {
	// 1. gateway Request -> Anthropic format
	body, err := json.Marshal(toAnthropicRequest(req))
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	// 2. build request with ctx
	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body),
	)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: build request: %w", err)
	}
	// Anthropic auth differs from OpenAI: x-api-key + anthropic-version,
	// NOT Authorization: Bearer.
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	// 3. execute
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: do request: %w: %v", ErrUpstreamUnavailable, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: read body: %w", err)
	}

	// 4. non-2xx -> typed error
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, newAPIError(p.Name(), resp.StatusCode, extractAnthropicError(raw))
	}

	// 5. decode + map back to gateway Response
	var ar anthropicResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		return Response{}, fmt.Errorf("anthropic: decode response: %w", err)
	}
	if len(ar.Content) == 0 {
		return Response{}, fmt.Errorf("anthropic: %w: empty content", ErrUpstreamError)
	}

	return Response{
		Content:  ar.Content[0].Text,
		Model:    ar.Model,
		Provider: p.Name(),
		Usage: Usage{
			InputTokens:  ar.Usage.InputTokens,
			OutputTokens: ar.Usage.OutputTokens,
		},
	}, nil
}

// toAnthropicRequest maps gateway shape -> Anthropic shape. The key difference
// from OpenAI: the system prompt is a TOP-LEVEL field, not a message with
// role "system". So we pull any system message out of the list.
func toAnthropicRequest(req Request) anthropicRequest {
	var system string
	msgs := make([]anthropicMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			system = m.Content
			continue
		}
		msgs = append(msgs, anthropicMessage{Role: string(m.Role), Content: m.Content})
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 1024 // Anthropic requires max_tokens; default if caller omits
	}
	return anthropicRequest{
		Model:     req.Model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  msgs,
	}
}

func extractAnthropicError(raw []byte) string {
	var ar anthropicResponse
	if err := json.Unmarshal(raw, &ar); err == nil && ar.Error != nil {
		return ar.Error.Message
	}
	return string(raw)
}

// Stream runs a streaming completion against Anthropic. It sets stream:true,
// then relays the raw SSE bytes as chunks (pass-through).
func (p *AnthropicProvider) Stream(ctx context.Context, req Request) (<-chan StreamChunk, error) {
	// Reuse the same request mapping, then flip on streaming.
	ar := toAnthropicRequest(req)
	payload := struct {
		anthropicRequest
		Stream bool `json:"stream"`
	}{anthropicRequest: ar, Stream: true}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("anthropic: build stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: do stream request: %w: %v", ErrUpstreamUnavailable, err)
	}

	// Non-2xx: read the error body now (no stream to relay) and fail before
	// we hand back a channel.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		return nil, newAPIError(p.Name(), resp.StatusCode, extractAnthropicError(raw))
	}

	// Relay the SSE stream on a channel. The goroutine owns closing the body
	// and the channel, so the caller just ranges until it closes.
	out := make(chan StreamChunk)
	go func() {
		defer resp.Body.Close()
		defer close(out)

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				// forward the raw SSE line; select lets ctx cancel unblock us
				// if the client went away and nobody is reading.
				select {
				case out <- StreamChunk{Data: line}:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					select {
					case out <- StreamChunk{Err: err}:
					case <-ctx.Done():
					}
				}
				return // EOF or error: stream is done
			}
		}
	}()

	return out, nil
}