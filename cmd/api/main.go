// Command api is the entry point for the Balvia HTTP backend.
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/rs/zerolog"

	"github.com/germandiaz17/Balvia-backend/internal/ai"
	"github.com/germandiaz17/Balvia-backend/internal/auth"
	"github.com/germandiaz17/Balvia-backend/internal/config"
	appcrypto "github.com/germandiaz17/Balvia-backend/internal/crypto"
	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/handlers"
	"github.com/germandiaz17/Balvia-backend/internal/middleware"
	"github.com/germandiaz17/Balvia-backend/internal/services"
	"github.com/germandiaz17/Balvia-backend/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// Bootstrap logger: config failed before we know LogLevel, so use a basic one.
		boot := logger.New("error", true)
		boot.Fatal().Err(err).Msg("failed to load config")
	}

	log := logger.New(cfg.LogLevel, !cfg.IsProduction())

	// Connect to PostgreSQL (Neon). Fatal if unreachable: the app is useless without it.
	pool, err := database.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer pool.Close()
	log.Info().Msg("connected to database")

	app := fiber.New(fiber.Config{
		AppName:               "balvia-backend",
		DisableStartupMessage: true,
		// Map all errors to a consistent JSON shape; log unexpected (5xx) ones.
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			msg := "internal server error"
			var fe *fiber.Error
			if errors.As(err, &fe) {
				code = fe.Code
				msg = fe.Message
			}
			if code >= fiber.StatusInternalServerError {
				log.Error().Err(err).Str("path", c.Path()).Msg("unhandled error")
			}
			return c.Status(code).JSON(fiber.Map{"error": msg})
		},
	})

	app.Use(recover.New())
	app.Use(requestid.New())
	app.Use(middleware.RequestLogger(log))

	// Dependency wiring: store -> services -> handlers.
	store := database.NewStore(pool)
	validate := validator.New(validator.WithRequiredStructEnabled())

	tokenManager := auth.NewTokenManager(cfg.JWTSecret)

	onboardingSvc := services.NewOnboardingService(store)
	authSvc := services.NewAuthService(store, onboardingSvc, tokenManager)
	authHandler := handlers.NewAuthHandler(authSvc, validate)

	transactionSvc := services.NewTransactionService(store)
	transactionHandler := handlers.NewTransactionHandler(transactionSvc, validate)

	accountHandler := handlers.NewAccountHandler(services.NewAccountService(store), validate)
	categoryHandler := handlers.NewCategoryHandler(services.NewCategoryService(store), validate)
	budgetHandler := handlers.NewBudgetHandler(services.NewBudgetService(store), validate)
	trackingPeriodHandler := handlers.NewTrackingPeriodHandler(services.NewPeriodQueryService(store), validate)
	savingsGoalHandler := handlers.NewSavingsGoalHandler(services.NewSavingsGoalService(store), validate)
	userSettingsHandler := handlers.NewUserSettingsHandler(services.NewUserSettingsService(store), validate)

	periodSvc := services.NewPeriodService(store, log)
	recurringEngineSvc := services.NewRecurringEngineService(store, log)

	recurringHandler := handlers.NewRecurringTransactionHandler(services.NewRecurringTransactionService(store), recurringEngineSvc, validate)

	syncSvc := services.NewSyncService(store, transactionSvc, log)
	syncHandler := handlers.NewSyncHandler(syncSvc)

	// AI features (BYOK — bring your own key). Each user supplies their own provider
	// key, stored encrypted with AI_ENCRYPTION_KEY. When that key is absent,
	// encryption is unavailable and /ai/* returns 503.
	var aiEnc ai.Encrypter
	var aiDec ai.Decrypter
	if len(cfg.AIEncryptionKey) > 0 {
		box, err := appcrypto.NewAESGCM(cfg.AIEncryptionKey)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to init AI encryption")
		}
		aiEnc, aiDec = box, box
	} else {
		log.Warn().Msg("AI_ENCRYPTION_KEY not set — AI features disabled (/ai/* returns 503)")
	}
	aiHandler := handlers.NewAIHandler(
		ai.NewService(store, aiDec),
		ai.NewSettingsService(store, aiEnc),
		validate,
	)

	api := app.Group("/api/v1")
	// Public routes (no token required).
	authHandler.RegisterPublic(api)
	// Authenticated routes (require a valid Bearer access token).
	authed := api.Group("", middleware.JWTAuth(tokenManager))
	authHandler.RegisterProtected(authed)
	transactionHandler.Register(authed)
	accountHandler.Register(authed)
	categoryHandler.Register(authed)
	budgetHandler.Register(authed)
	trackingPeriodHandler.Register(authed)
	savingsGoalHandler.Register(authed)
	userSettingsHandler.Register(authed)
	recurringHandler.Register(authed)
	syncHandler.Register(authed)
	aiHandler.Register(authed)

	// Liveness: is the process up? (no external dependencies)
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "ok",
			"env":    cfg.AppEnv,
			"time":   time.Now().UTC().Format(time.RFC3339),
		})
	})

	// Readiness: can the app serve traffic? (checks the database)
	app.Get("/health/ready", func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), 3*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"status": "unavailable",
				"db":     "down",
			})
		}
		return c.JSON(fiber.Map{"status": "ok", "db": "up"})
	})

	// Background schedulers: stopped via context on shutdown.
	schedCtx, stopScheduler := context.WithCancel(context.Background())
	defer stopScheduler()
	// Close due tracking periods hourly (lazy close on access is the fallback).
	go runCloseScheduler(schedCtx, periodSvc, log)
	// Materialise due recurring transactions hourly (lazy trigger on API access
	// is the fallback).
	go runRecurringScheduler(schedCtx, recurringEngineSvc, log)

	// Start the server in a goroutine so main can wait for shutdown signals.
	go func() {
		addr := ":" + cfg.Port
		log.Info().Str("addr", addr).Str("env", cfg.AppEnv).Msg("starting server")
		if err := app.Listen(addr); err != nil && !errors.Is(err, context.Canceled) {
			log.Fatal().Err(err).Msg("server stopped unexpectedly")
		}
	}()

	// Wait for an interrupt or terminate signal for graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(ctx); err != nil {
		log.Error().Err(err).Msg("graceful shutdown failed")
	}

	log.Info().Msg("server stopped")
}

// runCloseScheduler periodically closes tracking periods that have ended. It
// runs once on startup and then hourly (a period only closes the day after its
// end_date, so hourly is plenty for a "nightly" job without external cron).
func runCloseScheduler(ctx context.Context, svc *services.PeriodService, log zerolog.Logger) {
	const interval = time.Hour

	run := func() {
		n, err := svc.CloseDuePeriods(ctx)
		if err != nil {
			log.Error().Err(err).Msg("close-due-periods job failed")
			return
		}
		if n > 0 {
			log.Info().Int("closed", n).Msg("closed due tracking periods")
		}
	}

	run() // catch up immediately on startup

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

// runRecurringScheduler periodically materialises due recurring transaction
// templates into real transactions. Runs once on startup (catch-up) then
// hourly. The lazy trigger on GET /recurring-transactions is the per-user
// fallback.
func runRecurringScheduler(ctx context.Context, svc *services.RecurringEngineService, log zerolog.Logger) {
	const interval = time.Hour

	run := func() {
		n, err := svc.ProcessDueRecurring(ctx)
		if err != nil {
			log.Error().Err(err).Msg("recurring-engine job failed")
			return
		}
		if n > 0 {
			log.Info().Int("generated", n).Msg("materialised recurring transactions")
		}
	}

	run() // catch up immediately on startup

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
