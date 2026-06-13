package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// roundTripFunc lets a func satisfy http.RoundTripper, so we stub the
// transport and test without hitting the network.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stubClient(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func TestComplete_Success(t *testing.T) {
	const body = `{
		"model": "gpt-4o-mini",
		"choices": [{"message": {"role": "assistant", "content": "hi there"}}],
		"usage": {"prompt_tokens": 5, "completion_tokens": 2}
	}`

	p := NewOpenAIProvider("test-key", "https://example.test/v1",
		WithHTTPClient(stubClient(func(r *http.Request) (*http.Response, error) {
			if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
				t.Fatalf("auth header = %q", got)
			}
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		})),
	)

	resp, err := p.Complete(context.Background(), Request{
		Model:    "gpt-4o-mini",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hi there" {
		t.Errorf("content = %q, want %q", resp.Content, "hi there")
	}
	if resp.Usage.InputTokens != 5 || resp.Usage.OutputTokens != 2 {
		t.Errorf("usage = %+v, want {5 2}", resp.Usage)
	}
}

func TestComplete_RateLimitedIsTyped(t *testing.T) {
	p := NewOpenAIProvider("k", "https://example.test/v1",
		WithHTTPClient(stubClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 429,
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"slow down"}}`)),
				Header:     make(http.Header),
			}, nil
		})),
	)

	_, err := p.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "x"}},
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}