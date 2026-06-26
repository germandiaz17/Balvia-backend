// Package middleware holds Fiber middlewares (logging, auth, etc.).
package middleware

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
)

// RequestLogger logs one structured line per request, including the request_id
// set by Fiber's requestid middleware (so it must run after it).
func RequestLogger(log zerolog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		// Process the request first so we know the outcome.
		err := c.Next()

		// The status code isn't written until Fiber's ErrorHandler runs (above
		// this middleware), so when a handler returns an error we derive the
		// status the same way the ErrorHandler will, to log the real value.
		status := c.Response().StatusCode()
		if err != nil {
			status = fiber.StatusInternalServerError
			var fe *fiber.Error
			if errors.As(err, &fe) {
				status = fe.Code
			}
		}

		reqID, _ := c.Locals("requestid").(string)

		var event *zerolog.Event
		switch {
		case status >= 500:
			event = log.Error().Err(err)
		case status >= 400:
			event = log.Warn().Err(err)
		default:
			event = log.Info()
		}

		event.
			Str("request_id", reqID).
			Str("method", c.Method()).
			Str("path", c.Path()).
			Int("status", status).
			Dur("latency", time.Since(start)).
			Str("ip", c.IP()).
			Msg("request")

		return err
	}
}
