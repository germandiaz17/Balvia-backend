package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// categoryStore is the slice of the data store the AI service needs.
type categoryStore interface {
	ListCategoriesForUser(ctx context.Context, userID uuid.NullUUID) ([]sqlc.Category, error)
	GetUserAISettings(ctx context.Context, userID uuid.UUID) (sqlc.UserAiSetting, error)
}

// Decrypter opens the stored (encrypted) API key.
type Decrypter interface {
	Decrypt(blob []byte) (string, error)
}

// Service loads a user's BYOK AI settings, decrypts the key, builds the matching
// provider client, and asks it to pick a category.
type Service struct {
	store  categoryStore
	crypto Decrypter // nil when AI encryption is not configured server-side
	build  func(ResolvedSettings) (Categorizer, error)
}

// NewService builds the service. crypto may be nil (AI disabled), in which case
// Categorize returns domain.ErrAIUnavailable.
func NewService(store categoryStore, crypto Decrypter) *Service {
	return &Service{store: store, crypto: crypto, build: Build}
}

// CategorizeInput is what the caller (handler) provides.
type CategorizeInput struct {
	Description     string
	Amount          string
	Merchant        string
	TransactionType string // "expense" (default), "income", "transfer"
}

// Categorize returns a category suggestion for the given expense text.
//
//   - ErrAIUnavailable  — AI encryption is not configured on the server.
//   - ErrAINotConfigured — the user hasn't set up an AI provider/key.
//   - Otherwise a Suggestion. CategoryID is uuid.Nil (Confidence 0) when the model
//     can't confidently match one of the user's categories.
func (s *Service) Categorize(ctx context.Context, userID uuid.UUID, in CategorizeInput) (Suggestion, error) {
	if s.crypto == nil {
		return Suggestion{}, domain.ErrAIUnavailable
	}

	settings, err := s.store.GetUserAISettings(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Suggestion{}, domain.ErrAINotConfigured
	}
	if err != nil {
		return Suggestion{}, err
	}
	if !settings.Enabled {
		return Suggestion{}, domain.ErrAINotConfigured
	}

	apiKey, err := s.crypto.Decrypt(settings.ApiKeyEncrypted)
	if err != nil {
		return Suggestion{}, fmt.Errorf("ai: decrypt key: %w", err)
	}

	cat, err := s.build(ResolvedSettings{
		Provider: Provider(settings.Provider),
		APIKey:   apiKey,
		BaseURL:  deref(settings.BaseUrl),
		Model:    deref(settings.Model),
	})
	if err != nil {
		return Suggestion{}, err
	}

	txType := strings.TrimSpace(in.TransactionType)
	if txType == "" {
		txType = "expense"
	}

	cats, err := s.store.ListCategoriesForUser(ctx, uuid.NullUUID{UUID: userID, Valid: true})
	if err != nil {
		return Suggestion{}, err
	}

	// Only offer categories of the matching type; track valid ids to reject
	// anything the model invents.
	candidates := make([]Candidate, 0, len(cats))
	valid := make(map[uuid.UUID]struct{}, len(cats))
	for _, c := range cats {
		if c.CategoryType != txType {
			continue
		}
		candidates = append(candidates, Candidate{ID: c.ID, Name: c.Name})
		valid[c.ID] = struct{}{}
	}
	if len(candidates) == 0 {
		return Suggestion{}, nil
	}

	sug, err := cat.Suggest(ctx, SuggestRequest{
		Description: in.Description,
		Amount:      in.Amount,
		Merchant:    in.Merchant,
		Candidates:  candidates,
	})
	if err != nil {
		// Provider-side failure (bad user key, rate limit, outage): surface as an
		// upstream error (502) rather than a generic 500.
		return Suggestion{}, fmt.Errorf("%w: %w", domain.ErrAIUpstream, err)
	}

	// Guard against hallucinated ids: only accept a candidate we actually offered.
	if _, ok := valid[sug.CategoryID]; !ok {
		return Suggestion{}, nil
	}
	switch {
	case sug.Confidence < 0:
		sug.Confidence = 0
	case sug.Confidence > 1:
		sug.Confidence = 1
	}
	return sug, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
