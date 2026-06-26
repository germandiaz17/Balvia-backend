package services

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/germandiaz17/Balvia-backend/internal/auth"
	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// Tokens is an access/refresh token pair returned to clients.
type Tokens struct {
	AccessToken     string
	AccessExpiresAt time.Time
	RefreshToken    string
}

// AuthResult bundles the authenticated user with a fresh token pair.
type AuthResult struct {
	User   sqlc.User
	Tokens Tokens
}

// RegisterInput is the validated input for account creation.
type RegisterInput struct {
	Email    string
	Password string
	FullName *string
}

// AuthService handles registration, login and refresh-token rotation. It reuses
// OnboardingService so that registering also provisions settings + first period.
type AuthService struct {
	store      database.Store
	onboarding *OnboardingService
	tokens     *auth.TokenManager
}

func NewAuthService(store database.Store, onboarding *OnboardingService, tokens *auth.TokenManager) *AuthService {
	return &AuthService{store: store, onboarding: onboarding, tokens: tokens}
}

// Register hashes the password, provisions the account (user + settings + first
// tracking period) atomically, and issues a token pair.
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (AuthResult, error) {
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return AuthResult{}, err
	}

	res, err := s.onboarding.Onboard(ctx, OnboardingInput{
		Email:        in.Email,
		FullName:     in.FullName,
		PasswordHash: &hash,
	})
	if err != nil {
		return AuthResult{}, err // ErrEmailAlreadyExists handled by caller
	}

	tokens, err := s.issueTokens(ctx, res.User.ID)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{User: res.User, Tokens: tokens}, nil
}

// Login verifies credentials and issues a token pair.
func (s *AuthService) Login(ctx context.Context, email, password string) (AuthResult, error) {
	user, err := s.store.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthResult{}, domain.ErrInvalidCredentials
	} else if err != nil {
		return AuthResult{}, err
	}
	if user.PasswordHash == nil || auth.CheckPassword(*user.PasswordHash, password) != nil {
		return AuthResult{}, domain.ErrInvalidCredentials
	}

	tokens, err := s.issueTokens(ctx, user.ID)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{User: user, Tokens: tokens}, nil
}

// Refresh validates a refresh token, rotates it (revoke old + issue new) and
// returns a fresh token pair.
func (s *AuthService) Refresh(ctx context.Context, rawRefresh string) (Tokens, error) {
	rt, err := s.store.GetRefreshToken(ctx, auth.HashToken(rawRefresh))
	if errors.Is(err, pgx.ErrNoRows) {
		return Tokens{}, domain.ErrInvalidToken
	} else if err != nil {
		return Tokens{}, err
	}

	if err := s.store.RevokeRefreshToken(ctx, rt.ID); err != nil {
		return Tokens{}, err
	}
	return s.issueTokens(ctx, rt.UserID)
}

// Logout revokes a refresh token. It is idempotent (unknown tokens are a no-op).
func (s *AuthService) Logout(ctx context.Context, rawRefresh string) error {
	rt, err := s.store.GetRefreshToken(ctx, auth.HashToken(rawRefresh))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	return s.store.RevokeRefreshToken(ctx, rt.ID)
}

// Me returns the current user by id.
func (s *AuthService) Me(ctx context.Context, userID uuid.UUID) (sqlc.User, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.User{}, domain.ErrNotFound
	}
	return user, err
}

func (s *AuthService) issueTokens(ctx context.Context, userID uuid.UUID) (Tokens, error) {
	access, accessExp, err := s.tokens.GenerateAccessToken(userID)
	if err != nil {
		return Tokens{}, err
	}

	raw, hash, err := auth.GenerateRefreshToken()
	if err != nil {
		return Tokens{}, err
	}
	if _, err := s.store.CreateRefreshToken(ctx, sqlc.CreateRefreshTokenParams{
		UserID:    userID,
		TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(s.tokens.RefreshTTL()), Valid: true},
	}); err != nil {
		return Tokens{}, err
	}

	return Tokens{AccessToken: access, AccessExpiresAt: accessExp, RefreshToken: raw}, nil
}
