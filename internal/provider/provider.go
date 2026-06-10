package provider

import "context"

// Provider is the common interface every LLM backend implements.
// OpenAI, Anthropic, and Ollama will each satisfy this.
type Provider interface {
	Name() string
	Complete(ctx context.Context, req Request) (Response, error)
	Stream(ctx context.Context, req Request) (<-chan StreamChunk, error)
}

type Request struct {
	Model     string
	Messages  []Message
	MaxTokens int
	Stream    bool
}

type Message struct {
	Role    string // "user" | "assistant" | "system"
	Content string
}

type Response struct {
	Content      string
	InputTokens  int
	OutputTokens int
	Model        string
}

type StreamChunk struct {
	Delta string
	Done  bool
	Err   error
}