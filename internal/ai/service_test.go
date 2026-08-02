package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// fakeStore satisfies categoryStore. settings/settingsErr control GetUserAISettings.
type fakeStore struct {
	cats        []sqlc.Category
	settings    sqlc.UserAiSetting
	settingsErr error
}

func (f fakeStore) ListCategoriesForUser(_ context.Context, _ uuid.NullUUID) ([]sqlc.Category, error) {
	return f.cats, nil
}

func (f fakeStore) GetUserAISettings(_ context.Context, _ uuid.UUID) (sqlc.UserAiSetting, error) {
	if f.settingsErr != nil {
		return sqlc.UserAiSetting{}, f.settingsErr
	}
	return f.settings, nil
}

// noopCrypto returns the ciphertext bytes as the plaintext string (test only).
type noopCrypto struct{}

func (noopCrypto) Decrypt(blob []byte) (string, error) { return string(blob), nil }

type fakeCategorizer struct {
	got SuggestRequest
	sug Suggestion
	err error
}

func (f *fakeCategorizer) Suggest(_ context.Context, req SuggestRequest) (Suggestion, error) {
	f.got = req
	return f.sug, f.err
}

func cat(name, typ string) sqlc.Category {
	return sqlc.Category{ID: uuid.New(), Name: name, CategoryType: typ}
}

// enabledSettings returns a UserAiSetting row that decrypts (via noopCrypto) to key.
func enabledSettings(key string) sqlc.UserAiSetting {
	return sqlc.UserAiSetting{Provider: string(ProviderAnthropic), ApiKeyEncrypted: []byte(key), Enabled: true}
}

// newTestService wires a Service with a fake builder that always returns fc.
func newTestService(store categoryStore, fc Categorizer) *Service {
	s := NewService(store, noopCrypto{})
	s.build = func(_ ResolvedSettings) (Categorizer, error) { return fc, nil }
	return s
}

func TestCategorize_EncryptionNotConfigured(t *testing.T) {
	svc := NewService(fakeStore{}, nil) // nil Decrypter = server not configured
	_, err := svc.Categorize(context.Background(), uuid.New(), CategorizeInput{Description: "x"})
	assert.ErrorIs(t, err, domain.ErrAIUnavailable)
}

func TestCategorize_UserNotConfigured(t *testing.T) {
	store := fakeStore{settingsErr: pgx.ErrNoRows}
	svc := newTestService(store, &fakeCategorizer{})
	_, err := svc.Categorize(context.Background(), uuid.New(), CategorizeInput{Description: "x"})
	assert.ErrorIs(t, err, domain.ErrAINotConfigured)
}

func TestCategorize_DisabledSettings(t *testing.T) {
	s := enabledSettings("sk-x")
	s.Enabled = false
	svc := newTestService(fakeStore{settings: s}, &fakeCategorizer{})
	_, err := svc.Categorize(context.Background(), uuid.New(), CategorizeInput{Description: "x"})
	assert.ErrorIs(t, err, domain.ErrAINotConfigured)
}

func TestCategorize_HappyPath_OnlyOffersMatchingType(t *testing.T) {
	food := cat("Alimentación", "expense")
	salary := cat("Salario", "income")
	fc := &fakeCategorizer{sug: Suggestion{CategoryID: food.ID, Confidence: 0.9}}

	store := fakeStore{cats: []sqlc.Category{food, salary}, settings: enabledSettings("sk-x")}
	got, err := newTestService(store, fc).Categorize(context.Background(), uuid.New(),
		CategorizeInput{Description: "almuerzo", Amount: "20000.00"})
	require.NoError(t, err)

	assert.Equal(t, food.ID, got.CategoryID)
	assert.InDelta(t, 0.9, got.Confidence, 1e-9)
	require.Len(t, fc.got.Candidates, 1) // income category not offered for an expense
	assert.Equal(t, food.ID, fc.got.Candidates[0].ID)
}

func TestCategorize_RejectsHallucinatedID(t *testing.T) {
	food := cat("Alimentación", "expense")
	fc := &fakeCategorizer{sug: Suggestion{CategoryID: uuid.New(), Confidence: 0.99}} // id not offered
	store := fakeStore{cats: []sqlc.Category{food}, settings: enabledSettings("sk-x")}

	got, err := newTestService(store, fc).Categorize(context.Background(), uuid.New(), CategorizeInput{Description: "x"})
	require.NoError(t, err)
	assert.Equal(t, uuid.Nil, got.CategoryID)
	assert.Zero(t, got.Confidence)
}

func TestCategorize_ClampsConfidence(t *testing.T) {
	food := cat("Alimentación", "expense")
	fc := &fakeCategorizer{sug: Suggestion{CategoryID: food.ID, Confidence: 1.7}}
	store := fakeStore{cats: []sqlc.Category{food}, settings: enabledSettings("sk-x")}

	got, err := newTestService(store, fc).Categorize(context.Background(), uuid.New(), CategorizeInput{Description: "x"})
	require.NoError(t, err)
	assert.Equal(t, 1.0, got.Confidence)
}

func TestCategorize_NoCandidates(t *testing.T) {
	store := fakeStore{cats: nil, settings: enabledSettings("sk-x")}
	got, err := newTestService(store, &fakeCategorizer{}).Categorize(context.Background(), uuid.New(), CategorizeInput{Description: "x"})
	require.NoError(t, err)
	assert.Equal(t, uuid.Nil, got.CategoryID)
}

func TestCategorize_PropagatesCategorizerError(t *testing.T) {
	food := cat("Alimentación", "expense")
	boom := errors.New("upstream 500")
	fc := &fakeCategorizer{err: boom}
	store := fakeStore{cats: []sqlc.Category{food}, settings: enabledSettings("sk-x")}

	_, err := newTestService(store, fc).Categorize(context.Background(), uuid.New(), CategorizeInput{Description: "x"})
	assert.ErrorIs(t, err, boom)                 // original cause preserved
	assert.ErrorIs(t, err, domain.ErrAIUpstream) // wrapped as an upstream error (→ 502)
}
