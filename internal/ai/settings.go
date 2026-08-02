package ai

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// settingsStore is the slice of the data store SettingsService needs.
type settingsStore interface {
	UpsertUserAISettings(ctx context.Context, arg sqlc.UpsertUserAISettingsParams) (sqlc.UserAiSetting, error)
	GetUserAISettings(ctx context.Context, userID uuid.UUID) (sqlc.UserAiSetting, error)
	DeleteUserAISettings(ctx context.Context, userID uuid.UUID) error
}

// Encrypter seals the user's API key for storage.
type Encrypter interface {
	Encrypt(plaintext string) ([]byte, error)
}

// SettingsService manages a user's BYOK AI configuration. The API key is stored
// encrypted and never returned to the client.
type SettingsService struct {
	store  settingsStore
	crypto Encrypter // nil when AI encryption is not configured server-side
}

func NewSettingsService(store settingsStore, crypto Encrypter) *SettingsService {
	return &SettingsService{store: store, crypto: crypto}
}

// SetInput is the user-provided AI configuration.
type SetInput struct {
	Provider string
	APIKey   string
	BaseURL  *string
	Model    *string
}

// Set encrypts the API key and upserts the user's AI settings.
func (s *SettingsService) Set(ctx context.Context, userID uuid.UUID, in SetInput) (sqlc.UserAiSetting, error) {
	if s.crypto == nil {
		return sqlc.UserAiSetting{}, domain.ErrAIUnavailable
	}
	switch Provider(in.Provider) {
	case ProviderAnthropic, ProviderOpenAICompatible:
	default:
		return sqlc.UserAiSetting{}, domain.ErrInvalidAIProvider
	}
	if strings.TrimSpace(in.APIKey) == "" {
		return sqlc.UserAiSetting{}, domain.ErrInvalidAIProvider
	}

	ct, err := s.crypto.Encrypt(in.APIKey)
	if err != nil {
		return sqlc.UserAiSetting{}, err
	}

	return s.store.UpsertUserAISettings(ctx, sqlc.UpsertUserAISettingsParams{
		UserID:          userID,
		Provider:        in.Provider,
		ApiKeyEncrypted: ct,
		BaseUrl:         normalizePtr(in.BaseURL),
		Model:           normalizePtr(in.Model),
	})
}

// Get returns the user's AI settings (the handler strips the encrypted key
// before responding). Returns domain.ErrNotFound when the user has none.
func (s *SettingsService) Get(ctx context.Context, userID uuid.UUID) (sqlc.UserAiSetting, error) {
	row, err := s.store.GetUserAISettings(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.UserAiSetting{}, domain.ErrNotFound
	}
	return row, err
}

// Delete removes the user's AI settings (idempotent).
func (s *SettingsService) Delete(ctx context.Context, userID uuid.UUID) error {
	return s.store.DeleteUserAISettings(ctx, userID)
}

// Configured reports whether AI encryption is available server-side.
func (s *SettingsService) Configured() bool { return s.crypto != nil }

func normalizePtr(p *string) *string {
	if p == nil {
		return nil
	}
	if strings.TrimSpace(*p) == "" {
		return nil
	}
	return p
}
