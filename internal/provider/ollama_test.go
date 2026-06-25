package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOllamaComplete_Success(t *testing.T) {
	// Ollama's reply: content nested under "message", tokens as
	// prompt_eval_count / eval_count.
	const body = `{
		"model": "llama3.2",
		"message": {"role": "assistant", "content": "hello there"},
		"done": true,
		"prompt_eval_count": 7,
		"eval_count": 3
	}`

	p := NewOllamaProvider("http://localhost:11434",
		WithOllamaHTTPClient(stubClient(func(r *http.Request) (*http.Response, error) {
			// Ollama uses no auth header — assert we did NOT set one.
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("unexpected auth header: %q (Ollama needs none)", got)
			}
			// Assert we hit the right endpoint.
			if !strings.HasSuffix(r.URL.Path, "/api/chat") {
				t.Errorf("path = %q, want /api/chat", r.URL.Path)
			}
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		})),
	)

	resp, err := p.Complete(context.Background(), Request{
		Model:    "llama3.2",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "ollama" {
		t.Errorf("provider = %q, want %q", resp.Provider, "ollama")
	}
	if resp.Content != "hello there" {
		t.Errorf("content = %q, want %q", resp.Content, "hello there")
	}
	if resp.Usage.InputTokens != 7 || resp.Usage.OutputTokens != 3 {
		t.Errorf("usage = %+v, want {7 3}", resp.Usage)
	}
}

func TestOllamaComplete_ServerDown(t *testing.T) {
	// A transport error (server not running) should map to ErrUpstreamUnavailable.
	p := NewOllamaProvider("http://localhost:11434",
		WithOllamaHTTPClient(stubClient(func(r *http.Request) (*http.Response, error) {
			return nil, io.ErrUnexpectedEOF // simulate a connection failure
		})),
	)

	_, err := p.Complete(context.Background(), Request{
		Model:    "llama3.2",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	// We don't assert the exact type here unless ErrUpstreamUnavailable is
	// exported and you want errors.Is — see note below.
}