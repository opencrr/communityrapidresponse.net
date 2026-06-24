package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	gosentry "github.com/getsentry/sentry-go"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/handlers"
	"github.com/opencrr/communityrapidresponse.net/internal/logging"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

// version is set at build time via -ldflags "-X main.version=..."
// See deploy/digitalocean/scripts/build-linux.sh
var version = "dev"

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize structured logging early (before any slog calls)
	logging.Init(cfg.Log.Format, cfg.Log.Level)

	slog.Info("starting community rapid response", "version", version)

	// Use build-embedded version as Sentry release if not overridden by env var
	if cfg.Sentry.Release == "" {
		cfg.Sentry.Release = version
	}

	// Validate configuration for the current environment
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Configuration error:\n  - %v", err)
	}
	slog.Info("configuration loaded", "environment", cfg.Env)

	if !cfg.MFA.Required {
		slog.Warn("mfa is disabled, should only be used in development", "mfa_required", false)
	}

	// Initialize Sentry error tracking
	if err := appSentry.Init(&cfg.Sentry); err != nil {
		log.Fatalf("Failed to initialize Sentry: %v", err)
	}
	defer gosentry.Flush(2 * time.Second)

	// Connect to database
	db, err := database.New(&cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer func() { _ = db.Close() }()

	slog.Info("connected to database")

	// Initialize repositories
	userRepo := database.NewUserRepository(db)
	regionRepo := database.NewRegionRepository(db)
	verificationRepo := database.NewVerificationRepository(db)
	signalGroupRepo := database.NewSignalGroupRepository(db)
	encryptedSecretRepo := database.NewEncryptedSecretRepository(db)
	secretUpdateProposalRepo := database.NewSecretUpdateProposalRepository(db)
	auditRepo := database.NewAuditRepository(db)


	// Encryption key repository
	encryptionKeyRepo := database.NewEncryptionKeyRepository(db)

	// School repositories
	schoolRepo := database.NewSchoolRepository(db)
	districtRepo := database.NewSchoolDistrictRepository(db)

	// Start audit log cleanup worker (runs every 24 hours, retains 90 days)
	auditRepo.StartCleanupWorker(context.Background(), 24*time.Hour, 90*24*time.Hour)

	// Start secret proposal expiration worker (runs every hour)
	secretUpdateProposalRepo.StartExpirationWorker(context.Background(), time.Hour)

	// Initialize services
	mailService, err := services.NewMailService(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize mail service: %v", err)
	}
	mapboxService := services.NewMapboxService(&cfg.Mapbox)

	// Initialize MFA service only if MFA is required
	var mfaService *services.MFAService
	if cfg.MFA.Required {
		var err error
		mfaService, err = services.NewMFAService(&cfg.MFA)
		if err != nil {
			log.Fatalf("Failed to initialize MFA service: %v", err)
		}
	}

	// Configure trusted proxies for IP extraction (must happen before rate limiter use)
	middleware.SetTrustedProxies(cfg.RateLimit.TrustedProxies)

	// Initialize rate limiter
	var rateLimiter services.RateLimiter
	var rateLimitConfig *handlers.RateLimitOptions
	if cfg.RateLimit.Enabled {
		if cfg.RateLimit.Backend == "database" {
			dbLimiter := services.NewDBRateLimiter(db.DB)
			dbLimiter.StartCleanupWorker(context.Background(), 5*time.Minute, time.Hour)
			rateLimiter = dbLimiter
			slog.Info("rate limiting enabled", "backend", "database", "limit", cfg.RateLimit.IPLimit, "window_secs", cfg.RateLimit.IPWindowSecs)
		} else {
			memLimiter := services.NewInMemoryRateLimiter()
			memLimiter.StartCleanupWorker(context.Background(), 5*time.Minute, time.Hour)
			rateLimiter = memLimiter
			slog.Info("rate limiting enabled", "backend", "in-memory", "limit", cfg.RateLimit.IPLimit, "window_secs", cfg.RateLimit.IPWindowSecs)
		}
		rateLimitConfig = &handlers.RateLimitOptions{
			Enabled:    true,
			Limit:      cfg.RateLimit.IPLimit,
			WindowSecs: cfg.RateLimit.IPWindowSecs,
		}
	} else {
		rateLimiter = services.NewNoOpRateLimiter()
		slog.Warn("rate limiting is disabled")
	}

	// Initialize email service
	emailService, err := services.NewEmailService(&cfg.Email)
	if err != nil {
		log.Fatalf("Failed to initialize email service: %v", err)
	}
	if cfg.Email.Enabled {
		slog.Info("email verification enabled", "backend", emailService.Backend())
	} else {
		slog.Warn("email verification disabled", "backend", emailService.Backend())
	}

	// Initialize notification queue and service
	notificationQueue := database.NewDatabaseQueue(db)
	notificationService := services.NewNotificationService(notificationQueue)

	// Initialize email templates
	appName := "Community Rapid Response"
	loginURL := cfg.Email.VerificationURL // Reuse email verification URL base
	if loginURL == "" {
		loginURL = "http://localhost:3000/login"
	}
	emailTemplates := services.NewEmailTemplates(appName, loginURL)

	// Start notification worker
	notificationWorker := services.NewNotificationWorker(
		notificationQueue,
		emailService,
		emailTemplates,
		userRepo,
		regionRepo,
		encryptedSecretRepo,
		&cfg.Notification,
	)
	notificationWorker.Start(context.Background())

	// Initialize password reset repository
	passwordResetRepo := database.NewPasswordResetRepository(db)

	// Initialize middleware
	jwtAuth := middleware.NewJWTAuth(&cfg.JWT)

	// Set up user status cache for token revocation (60s TTL)
	statusCache := middleware.NewUserStatusCache(userRepo, 60*time.Second)
	jwtAuth.SetStatusCache(statusCache)

	// Derive base URL from login URL for password reset links
	baseURL := loginURL
	// Strip trailing path components like /login to get the base URL
	if idx := len(baseURL) - 1; idx > 0 {
		for idx > 0 && baseURL[idx] != '/' {
			idx--
		}
		if idx > 0 {
			baseURL = baseURL[:idx]
		}
	}

	// Initialize handlers
	authHandler := handlers.NewAuthHandlerWithEmailService(
		db,
		userRepo,
		jwtAuth,
		emailService,
		cfg.JWT.Secret,
		cfg.Server.SecureCookies,
		cfg.MFA.Required,
		auditRepo,
		passwordResetRepo,
		emailTemplates,
		baseURL,
		encryptionKeyRepo,
	)
	authHandler.SetRateLimiter(rateLimiter)

	mfaHandler := handlers.NewMFAHandler(db, userRepo, mfaService, jwtAuth, cfg.Server.SecureCookies, auditRepo)
	regionHandler := handlers.NewRegionHandler(regionRepo, userRepo, mapboxService, auditRepo)
	verificationHandler := handlers.NewVerificationHandler(
		db,
		verificationRepo,
		userRepo,
		regionRepo,
		mailService,
		mapboxService,
		auditRepo,
	)
	adminHandler := handlers.NewAdminHandler(userRepo, regionRepo, auditRepo)

	// Initialize NCES service for lazy-loading district boundaries
	ncesService := services.NewNCESService()

	// Initialize meshtastic channel repository
	meshtasticChannelRepo := database.NewMeshtasticChannelRepository(db)

	// Initialize group repository and handler
	groupRepo := database.NewGroupRepository(db)
	groupHandler := handlers.NewGroupHandler(groupRepo, signalGroupRepo, meshtasticChannelRepo, regionRepo, userRepo, auditRepo)
	groupHandler.SetRateLimiter(rateLimiter)

	// Initialize school handler
	schoolHandler := handlers.NewSchoolHandler(
		schoolRepo, districtRepo, groupRepo, userRepo, auditRepo, ncesService,
	)

	// Initialize connection repository and handler
	connectionRepo := database.NewConnectionRepository(db)
	connectionHandler := handlers.NewConnectionHandler(connectionRepo, groupRepo, auditRepo)

	// Initialize encryption handler
	encryptionHandler := handlers.NewEncryptionHandler(encryptionKeyRepo, encryptedSecretRepo, regionRepo, schoolRepo, userRepo, groupRepo, signalGroupRepo, connectionRepo)

	// Wire status cache to handlers for immediate cache eviction on privilege changes
	adminHandler.SetStatusCache(statusCache)
	verificationHandler.SetStatusCache(statusCache)

	// Wire JWT auth to verification handler for issuing fresh tokens after postcard verification
	verificationHandler.SetJWTAuth(jwtAuth, cfg.Server.SecureCookies)

	// Wire notification service to handlers
	verificationHandler.SetNotificationService(notificationService)
	encryptionHandler.SetNotificationService(notificationService)

	// Setup CSRF protection
	csrfConfig := &handlers.CSRFConfig{
		Enabled:       true,
		Secret:        cfg.JWT.Secret,
		SecureCookies: cfg.Server.SecureCookies,
	}

	// Build Content-Security-Policy directives.
	// NOTE: script-src has NO 'unsafe-inline' (inline scripts were externalized to
	// config.js / env-banner.js). style-src DOES retain 'unsafe-inline' because
	// Mapbox GL JS sets inline style attributes on map elements at runtime;
	// removing it breaks map rendering. Keep these directives in sync with
	// nginx.conf / nginx.test.conf, which serve the SPA document.
	cspDirectives := "default-src 'self'; " +
		"script-src 'self' https://api.mapbox.com; " +
		"style-src 'self' https://api.mapbox.com 'unsafe-inline'; " +
		"img-src 'self' data: blob: https://api.mapbox.com; " +
		"connect-src 'self' https://api.mapbox.com https://events.mapbox.com; " +
		"worker-src blob:; " +
		"child-src blob:; " +
		"object-src 'none'; " +
		"base-uri 'self'; " +
		"frame-ancestors 'none'"

	securityConfig := &middleware.SecurityConfig{
		SecureCookies: cfg.Server.SecureCookies,
		CSPDirectives: cspDirectives,
	}

	// Setup router
	router := handlers.NewRouter(
		authHandler,
		mfaHandler,
		regionHandler,
		verificationHandler,
		adminHandler,
		schoolHandler,
		encryptionHandler,
		groupHandler,
		connectionHandler,
		jwtAuth,
		rateLimiter,
		rateLimitConfig,
		csrfConfig,
		cfg.CORS.AllowedOrigins,
		securityConfig,
	)

	// Create HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:              addr,
		Handler:           router.Setup(),
		ReadTimeout:       cfg.Server.ReadTimeout,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		slog.Info("server starting", "addr", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	slog.Info("server stopped")
}
