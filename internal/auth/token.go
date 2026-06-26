package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Default token lifetimes. Access tokens are short-ish; refresh tokens are long
// (mobile keeps users signed in) and are rotated on every use.
const (
	defaultAccessTTL  = 24 * time.Hour
	defaultRefreshTTL = 30 * 24 * time.Hour
)

// Claims is the access-token payload. UserID is the subject.
type Claims struct {
	jwt.RegisteredClaims
	UserID string `json:"uid"`
}

// TokenManager issues and verifies access tokens and mints refresh tokens.
type TokenManager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewTokenManager builds a manager from the HMAC secret, using default TTLs.
func NewTokenManager(secret string) *TokenManager {
	return &TokenManager{
		secret:     []byte(secret),
		accessTTL:  defaultAccessTTL,
		refreshTTL: defaultRefreshTTL,
	}
}

// RefreshTTL exposes the refresh-token lifetime so callers can set expires_at.
func (m *TokenManager) RefreshTTL() time.Duration { return m.refreshTTL }

// GenerateAccessToken signs a JWT for userID and returns it with its expiry.
func (m *TokenManager) GenerateAccessToken(userID uuid.UUID) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(m.accessTTL)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		UserID: userID.String(),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, exp, nil
}

// ParseAccessToken validates the token signature/expiry and returns the user id.
func (m *TokenManager) ParseAccessToken(tokenString string) (uuid.UUID, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil || !token.Valid {
		return uuid.Nil, fmt.Errorf("invalid access token: %w", err)
	}
	return uuid.Parse(claims.UserID)
}

// GenerateRefreshToken returns a random opaque token and its SHA-256 hash. Only
// the hash is stored; the raw value is given to the client once.
func GenerateRefreshToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashToken(raw), nil
}

// HashToken returns the hex SHA-256 of a refresh token for storage/lookup.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
