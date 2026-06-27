// Command regression runs a fixed set of prompts against a running gateway
// and reports per-prompt latency, the provider that served it, and pass/fail.
//
// It talks to the gateway over HTTP like any client, so it stays decoupled
// from internal code. Also serves as the benchmark harness (e.g. vs LiteLLM).
//
// Run: go run ./regression   (set GATEWAY_URL to point elsewhere)
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"
)

// prompt is one regression case from prompts.json.
type prompt struct {
	Name    string `json:"name"`
	Model   string `json:"model"`
	Content string `json:"content"`
}

// messagesRequest matches the gateway's POST /v1/messages body.
type messagesRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// messagesResponse matches the gateway's success response. We only read the
// fields we report on; extra fields are ignored.
type messagesResponse struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Content  string `json:"content"`
	Usage    struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// result holds the outcome of running one prompt.
type result struct {
	Name     string
	Provider string
	Latency  time.Duration
	OK       bool
	Detail   string // error or short note when not OK
}

func main() {
	// Flags + env so the harness points anywhere without code changes.
	promptsPath := flag.String("prompts", "prompts.json", "path to the prompts JSON file")
	flag.Parse()

	baseURL := os.Getenv("GATEWAY_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	prompts, err := loadPrompts(*promptsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load prompts: %v\n", err)
		os.Exit(1)
	}

	// One client, generous timeout: local models can be slow on first load.
	client := &http.Client{Timeout: 120 * time.Second}

	fmt.Printf("Running %d prompts against %s\n\n", len(prompts), baseURL)

	results := make([]result, 0, len(prompts))
	for _, p := range prompts {
		results = append(results, runOne(client, baseURL, p))
	}

	printReport(results)
}

// loadPrompts reads and parses the prompts JSON file.
func loadPrompts(path string) ([]prompt, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var prompts []prompt
	if err := json.Unmarshal(raw, &prompts); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return prompts, nil
}

// runOne sends a single prompt and records latency, provider, and success.
// A non-2xx status or transport error is recorded as a failed result, not a
// crash, so one bad provider can't stop the whole run.
func runOne(client *http.Client, baseURL string, p prompt) result {
	body, _ := json.Marshal(messagesRequest{
		Model:    p.Model,
		Messages: []message{{Role: "user", Content: p.Content}},
	})

	start := time.Now()
	resp, err := client.Post(baseURL+"/v1/messages", "application/json", bytes.NewReader(body))
	latency := time.Since(start)

	if err != nil {
		return result{Name: p.Name, Latency: latency, OK: false, Detail: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result{Name: p.Name, Latency: latency, OK: false,
			Detail: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}

	var out messagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return result{Name: p.Name, Latency: latency, OK: false, Detail: "bad JSON"}
	}

	// Treat an empty answer as a failure: a 200 with no content is still wrong.
	ok := out.Content != ""
	detail := ""
	if !ok {
		detail = "empty content"
	}
	return result{Name: p.Name, Provider: out.Provider, Latency: latency, OK: ok, Detail: detail}
}

// printReport prints a per-prompt table plus a summary with pass count and
// latency percentiles. p50/p95 capture tail latency, the metric that matters
// for "predictable under load".
func printReport(results []result) {
	fmt.Printf("%-28s %-10s %10s   %s\n", "PROMPT", "PROVIDER", "LATENCY", "STATUS")
	fmt.Println("------------------------------------------------------------------------")

	passed := 0
	latencies := make([]time.Duration, 0, len(results))
	for _, r := range results {
		status := "OK"
		if !r.OK {
			status = "FAIL: " + r.Detail
		} else {
			passed++
		}
		latencies = append(latencies, r.Latency)
		fmt.Printf("%-28s %-10s %10s   %s\n",
			r.Name, r.Provider, r.Latency.Round(time.Millisecond), status)
	}

	fmt.Println("------------------------------------------------------------------------")
	fmt.Printf("Passed: %d/%d\n", passed, len(results))
	if len(latencies) > 0 {
		fmt.Printf("Latency  p50: %s   p95: %s   max: %s\n",
			percentile(latencies, 50).Round(time.Millisecond),
			percentile(latencies, 95).Round(time.Millisecond),
			maxDuration(latencies).Round(time.Millisecond),
		)
	}

	// Non-zero exit if anything failed, so this can gate CI later.
	if passed != len(results) {
		os.Exit(1)
	}
}

// percentile returns the p-th percentile latency (nearest-rank method).
func percentile(durations []time.Duration, p int) time.Duration {
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	rank := (p * len(sorted)) / 100
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

// maxDuration returns the largest latency in the set.
func maxDuration(durations []time.Duration) time.Duration {
	var max time.Duration
	for _, d := range durations {
		if d > max {
			max = d
		}
	}
	return max
}