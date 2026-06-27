// Package provider defines the LLM backend interface and its adapters
// (OpenAI, Anthropic, Ollama). Everything else depends on Provider, not
// on a specific vendor — new backend = new file here, nothing else changes.
package provider

import "context"


//Role identifies who authored a message in a consersation
type Role string

const (
	RoleSystem Role = "syetem"
	RoleUser Role = "user"
	RoleAssistant Role = "assistant"
)

//provider names
const (
	NameOpenAI    = "openai"
	NameAnthropic = "anthropic"
	NameOllama    = "ollama"
)

// Message is one turn in a conversation, normalized across providers.
type Message struct {
	Role Role `json:"role"`
	Content string `json:"content"`
}


/// Request is the vendor-neutral request. Each adapter maps it to its own format.
type Request struct {
	Model string `json:"model"`
	Messages []Message `json:"messages"`
	MaxTokens int `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
}


// Usage is captured now even though cost tracking comes later — avoids a retrofit.
type Usage struct {
	InputTokens int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}


// Response is the gateway's vendor-neutral completion response.

type Response struct {
	Content string `json:"content"`
	Model string `json:"model"`
	Provider string `json:"provider"`
	Usage Usage `json:"usage"`
}

// Provider is implemented by every backend.
// Complete only, for now — streaming will be a separate interface so we don't
// force it onto adapters that don't need it yet.
type Provider interface {
	Name() string

	// Complete runs one non-streaming completion. Must honor ctx and return
	// a typed error (errors.go) so callers can branch on failure class.
	Complete(ctx context.Context, req Request) (Response, error)
}









