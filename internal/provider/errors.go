package provider

import (
	"errors"
	"fmt"
)

// Sentinel errors so callers can branch on failure class via errors.Is,
// instead of string-matching. The fallback logic (later) uses these.
var (
	ErrUpstreamUnavailable = errors.New("provider: upstream unavailable")
	ErrRateLimited         = errors.New("provider: rate limited by upstream")
	ErrInvalidRequest      = errors.New("provider: invalid request")
	ErrUpstreamError       = errors.New("provider: upstream error")
)

// APIError carries upstream HTTP detail but still unwraps to a sentinel,
// so errors.Is(err, ErrRateLimited) works on it.
type APIError struct {
	Provider   string
	StatusCode int
	Message    string
	kind       error
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: status=%d: %s", e.Provider, e.StatusCode, e.Message)
}

func (e *APIError) Unwrap() error { return e.kind }

// map an HTTP status to a sentinel category
func classifyStatus(code int) error {
	switch {
	case code == 429:
		return ErrRateLimited
	case code >= 500:
		return ErrUpstreamUnavailable
	case code >= 400:
		return ErrInvalidRequest
	default:
		return ErrUpstreamError
	}
}

func newAPIError(providerName string, status int, msg string) *APIError {
	return &APIError{
		Provider:   providerName,
		StatusCode: status,
		Message:    msg,
		kind:       classifyStatus(status),
	}
}