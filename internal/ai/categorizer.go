// Package ai contains AI-backed features. Currently: expense auto-categorization
// powered by the Claude API. The concrete provider is isolated behind the
// Categorizer interface so the service layer can be unit-tested without network.
package ai

import (
	"context"

	"github.com/google/uuid"
)

// Candidate is one category the model may choose from.
type Candidate struct {
	ID   uuid.UUID
	Name string
}

// SuggestRequest is the input to a single categorization call.
type SuggestRequest struct {
	Description string
	Amount      string // decimal as string, e.g. "20000.00"; may be empty
	Merchant    string // optional
	Candidates  []Candidate
}

// Suggestion is the model's pick plus its self-reported confidence (0..1).
type Suggestion struct {
	CategoryID uuid.UUID
	Confidence float64
}

// Categorizer suggests the best-matching category for an expense. Implemented by
// the Anthropic and OpenAI-compatible clients in production and by a fake in tests.
type Categorizer interface {
	Suggest(ctx context.Context, req SuggestRequest) (Suggestion, error)
}

// Provider identifies which AI backend a user brought their key for.
type Provider string

const (
	ProviderAnthropic        Provider = "anthropic"
	ProviderOpenAICompatible Provider = "openai_compatible"
)

// ResolvedSettings is a user's decrypted AI configuration, used to build a
// Categorizer for a single request (BYOK — bring your own key).
type ResolvedSettings struct {
	Provider Provider
	APIKey   string
	BaseURL  string // openai_compatible only (defaults to OpenAI when empty)
	Model    string // optional; the adapter picks a sensible default when empty
}
