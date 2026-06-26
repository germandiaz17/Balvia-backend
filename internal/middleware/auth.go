package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/germandiaz17/Balvia-backend/internal/auth"
)

// userIDKey is the Locals key under which the authenticated user id is stored.
const userIDKey = "userID"

// JWTAuth validates the Bearer access token and stores the user id in Locals.
func JWTAuth(tm *auth.TokenManager) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := c.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			return fiber.NewError(fiber.StatusUnauthorized, "missing or malformed Authorization header")
		}
		raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))

		userID, err := tm.ParseAccessToken(raw)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid or expired token")
		}
		c.Locals(userIDKey, userID)
		return c.Next()
	}
}

// UserID returns the authenticated user id set by the auth middleware.
// The bool is false if no (valid) user id is present on the context.
func UserID(c *fiber.Ctx) (uuid.UUID, bool) {
	id, ok := c.Locals(userIDKey).(uuid.UUID)
	return id, ok
}
