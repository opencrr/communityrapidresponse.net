package handlers

import (
	"net/http"
	"strings"
	"time"

	sentryhttp "github.com/getsentry/sentry-go/http"

	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

// Router handles HTTP routing
type Router struct {
	mux                *http.ServeMux
	auth               *AuthHandler
	mfa                *MFAHandler
	regions            *RegionHandler
	signalGroups       *SignalGroupHandler
	verification       *VerificationHandler
	admin              *AdminHandler
	membership         *MembershipHandler
	blocklistProposals  *BlocklistProposalHandler
	deletionProposals   *DeletionProposalHandler
	schools             *SchoolHandler
	userReports         *UserReportHandler
	encryption          *EncryptionHandler
	secretUpdates       *SecretUpdateHandler
	meshtastic          *MeshtasticHandler
	jwtAuth            *middleware.JWTAuth
	rateLimiter        services.RateLimiter
	rateLimitConfig    *RateLimitOptions
	csrfConfig         *CSRFConfig
	corsOrigins        []string
	securityConfig     *middleware.SecurityConfig
}

// RateLimitOptions configures rate limiting for the router
type RateLimitOptions struct {
	Enabled  bool
	Limit    int
	WindowSecs int
}

// CSRFConfig configures CSRF protection for the router
type CSRFConfig struct {
	Enabled       bool
	Secret        string
	SecureCookies bool
}

// NewRouter creates a new router with all handlers
func NewRouter(
	auth *AuthHandler,
	mfa *MFAHandler,
	regions *RegionHandler,
	signalGroups *SignalGroupHandler,
	verification *VerificationHandler,
	admin *AdminHandler,
	membership *MembershipHandler,
	blocklistProposals *BlocklistProposalHandler,
	deletionProposals *DeletionProposalHandler,
	schools *SchoolHandler,
	userReports *UserReportHandler,
	encryption *EncryptionHandler,
	secretUpdates *SecretUpdateHandler,
	meshtastic *MeshtasticHandler,
	jwtAuth *middleware.JWTAuth,
	rateLimiter services.RateLimiter,
	rateLimitConfig *RateLimitOptions,
	csrfConfig *CSRFConfig,
	corsOrigins []string,
	securityConfig *middleware.SecurityConfig,
) *Router {
	return &Router{
		mux:                http.NewServeMux(),
		auth:               auth,
		mfa:                mfa,
		regions:            regions,
		signalGroups:       signalGroups,
		verification:       verification,
		admin:              admin,
		membership:         membership,
		blocklistProposals:  blocklistProposals,
		deletionProposals:   deletionProposals,
		schools:             schools,
		userReports:         userReports,
		encryption:          encryption,
		secretUpdates:       secretUpdates,
		meshtastic:          meshtastic,
		jwtAuth:            jwtAuth,
		rateLimiter:        rateLimiter,
		rateLimitConfig:    rateLimitConfig,
		csrfConfig:         csrfConfig,
		corsOrigins:        corsOrigins,
		securityConfig:     securityConfig,
	}
}

// Setup configures all routes
func (r *Router) Setup() http.Handler {
	// Public routes
	r.mux.HandleFunc("/api/v1/auth/register", r.methodHandler(http.MethodPost, r.auth.Register))
	r.mux.HandleFunc("/api/v1/auth/login", r.methodHandler(http.MethodPost, r.auth.Login))
	r.mux.HandleFunc("/api/v1/auth/logout", r.methodHandler(http.MethodPost, r.auth.Logout))
	r.mux.HandleFunc("/api/v1/auth/verify-email", r.methodHandler(http.MethodGet, r.auth.VerifyEmail))

	// Public password reset routes
	r.mux.HandleFunc("/api/v1/auth/forgot-password", r.methodHandler(http.MethodPost, r.auth.ForgotPassword))
	r.mux.HandleFunc("/api/v1/auth/reset-password", r.methodHandler(http.MethodPost, r.auth.ResetPassword))
	r.mux.HandleFunc("/api/v1/auth/validate-reset-token", r.methodHandler(http.MethodGet, r.auth.ValidateResetToken))

	// Authenticated password change
	r.mux.HandleFunc("/api/v1/auth/change-password", r.authenticated(r.methodHandler(http.MethodPost, r.auth.ChangePassword)))

	// Email verification routes (require email_unverified token)
	r.mux.HandleFunc("/api/v1/auth/resend-verification", r.authenticatedEmailUnverified(r.methodHandler(http.MethodPost, r.auth.ResendVerificationEmail)))

	// MFA routes (require specific token types)
	r.mux.HandleFunc("/api/v1/mfa/setup", r.authenticatedMFASetup(r.methodHandler(http.MethodPost, r.mfa.InitSetup)))
	r.mux.HandleFunc("/api/v1/mfa/setup/complete", r.authenticatedMFASetup(r.methodHandler(http.MethodPost, r.mfa.CompleteSetup)))
	r.mux.HandleFunc("/api/v1/mfa/verify", r.authenticatedPendingMFA(r.methodHandler(http.MethodPost, r.mfa.Verify)))

	// Protected routes - user
	r.mux.HandleFunc("/api/v1/users/me/deletion-preflight", r.authenticated(r.methodHandler(http.MethodGet, r.auth.DeletionPreflight)))
	r.mux.HandleFunc("/api/v1/users/me", r.authenticated(r.handleUserMe))

	// Protected routes - communities (regions)
	r.mux.HandleFunc("/api/v1/communities", r.handleRegions)
	r.mux.HandleFunc("/api/v1/communities/admin", r.authenticated(r.methodHandler(http.MethodGet, r.regions.ListAdmin)))
	r.mux.HandleFunc("/api/v1/communities/", r.handleRegionByID)

	// Protected routes - membership requests
	r.mux.HandleFunc("/api/v1/membership-requests", r.handleMembershipRequests)
	r.mux.HandleFunc("/api/v1/membership-requests/admin", r.authenticated(r.methodHandler(http.MethodGet, r.membership.ListPendingForAdmin)))
	r.mux.HandleFunc("/api/v1/membership-requests/", r.handleMembershipRequestByID)

	// Protected routes - invitations
	r.mux.HandleFunc("/api/v1/invitations", r.authenticated(r.methodHandler(http.MethodGet, r.membership.ListInvitations)))
	r.mux.HandleFunc("/api/v1/invitations/", r.handleInvitationByID)

	// Protected routes - signal groups
	r.mux.HandleFunc("/api/v1/signal-groups", r.handleSignalGroups)
	r.mux.HandleFunc("/api/v1/signal-groups/admin", r.authenticated(r.methodHandler(http.MethodGet, r.signalGroups.ListAdmin)))
	r.mux.HandleFunc("/api/v1/signal-groups/", r.handleSignalGroupByID)

	// Secret proposal routes (replaces invite-link-proposals)
	r.mux.HandleFunc("/api/v1/secret-proposals", r.authenticated(r.methodHandler(http.MethodGet, r.secretUpdates.ListProposals)))
	r.mux.HandleFunc("/api/v1/secret-proposals/", r.handleSecretProposal)

	// Encrypted secret finalization
	r.mux.HandleFunc("/api/v1/encrypted-secrets/", r.handleEncryptedSecretByID)

	// Meshtastic channel routes
	r.mux.HandleFunc("/api/v1/meshtastic-channels", r.handleMeshtasticChannels)
	r.mux.HandleFunc("/api/v1/meshtastic-channels/admin", r.authenticated(r.methodHandler(http.MethodGet, r.meshtastic.ListAdmin)))
	r.mux.HandleFunc("/api/v1/meshtastic-channels/", r.handleMeshtasticChannelByID)

	// School routes (all authenticated)
	r.mux.HandleFunc("/api/v1/schools", r.handleSchools)
	r.mux.HandleFunc("/api/v1/schools/my", r.authenticated(r.methodHandler(http.MethodGet, r.schools.ListMySchools)))
	r.mux.HandleFunc("/api/v1/schools/", r.handleSchoolByID)
	r.mux.HandleFunc("/api/v1/school-districts", r.authenticated(r.methodHandler(http.MethodGet, r.schools.SearchDistricts)))
	r.mux.HandleFunc("/api/v1/school-districts/", r.handleSchoolDistrictByID)

	// Protected routes - verification
	r.mux.HandleFunc("/api/v1/verification/status", r.authenticated(r.methodHandler(http.MethodGet, r.verification.GetStatus)))
	r.mux.HandleFunc("/api/v1/verification/postcard/request", r.authenticated(r.methodHandler(http.MethodPost, r.verification.RequestPostcardVerification)))
	r.mux.HandleFunc("/api/v1/verification/postcard/verify", r.authenticated(r.methodHandler(http.MethodPost, r.verification.VerifyCode)))
	r.mux.HandleFunc("/api/v1/verification/vouch", r.authenticated(r.methodHandler(http.MethodPost, r.verification.Vouch)))
	r.mux.HandleFunc("/api/v1/verification/vouch/request", r.authenticated(r.methodHandler(http.MethodPost, r.verification.RequestVouchVerification)))
	r.mux.HandleFunc("/api/v1/verification/vouch/pending", r.authenticated(r.methodHandler(http.MethodGet, r.verification.GetPendingVouchRequests)))
	r.mux.HandleFunc("/api/v1/verification/vouch/status/", r.handleVouchStatus)

	// Protected routes - blocklist proposals
	r.mux.HandleFunc("/api/v1/blocklist-proposals", r.authenticated(r.methodHandler(http.MethodGet, r.blocklistProposals.ListProposals)))
	r.mux.HandleFunc("/api/v1/blocklist-proposals/", r.handleBlocklistProposal)

	// Protected routes - deletion proposals
	r.mux.HandleFunc("/api/v1/deletion-proposals", r.handleDeletionProposals)
	r.mux.HandleFunc("/api/v1/deletion-proposals/", r.handleDeletionProposal)

	// Protected routes - encryption keys
	r.mux.HandleFunc("/api/v1/encryption/keys", r.handleEncryptionKeys)
	r.mux.HandleFunc("/api/v1/encryption/keys/rotate", r.authenticated(r.methodHandler(http.MethodPost, r.handleEncryptionKeysRotate)))
	r.mux.HandleFunc("/api/v1/encryption/public-keys", r.authenticated(r.methodHandler(http.MethodGet, r.handleEncryptionPublicKeys)))
	r.mux.HandleFunc("/api/v1/encryption/pending-rekeys", r.authenticated(r.methodHandler(http.MethodGet, r.handleEncryptionPendingRekeys)))
	r.mux.HandleFunc("/api/v1/encryption/rekey", r.authenticated(r.methodHandler(http.MethodPost, r.handleEncryptionRekey)))

	// Protected routes - user reports
	r.mux.HandleFunc("/api/v1/reports", r.authenticated(r.methodHandler(http.MethodGet, r.userReports.ListReports)))
	r.mux.HandleFunc("/api/v1/reports/", r.handleReportByID)

	// Admin routes (superuser only)
	r.mux.HandleFunc("/api/v1/admin/users", r.authenticated(r.methodHandler(http.MethodGet, r.admin.ListUsers)))
	r.mux.HandleFunc("/api/v1/admin/users/", r.handleAdminUserByID)
	r.mux.HandleFunc("/api/v1/admin/audit-logs", r.authenticated(r.methodHandler(http.MethodGet, r.admin.GetAuditLogs)))
	r.mux.HandleFunc("/api/v1/admin/audit-logs/export", r.authenticated(r.methodHandler(http.MethodGet, r.admin.ExportAuditLogs)))
	r.mux.HandleFunc("/api/v1/admin/blocked-addresses", r.authenticated(r.methodHandler(http.MethodGet, r.blocklistProposals.ListBlockedAddresses)))
	r.mux.HandleFunc("/api/v1/admin/blocked-addresses/", r.handleBlockedAddress)

	// Health check (both paths for compatibility)
	healthHandler := func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
	r.mux.HandleFunc("/health", healthHandler)
	r.mux.HandleFunc("/api/v1/health", healthHandler)

	// Apply middleware chain
	handler := http.Handler(r.mux)
	handler = middleware.ContentType(handler)
	if r.csrfConfig != nil && r.csrfConfig.Enabled {
		handler = middleware.CSRFProtection(r.csrfConfig.Secret, r.csrfConfig.SecureCookies)(handler)
	}
	handler = middleware.SecurityHeadersWithConfig(r.securityConfig)(handler)
	corsOrigins := r.corsOrigins
	if len(corsOrigins) == 0 {
		corsOrigins = []string{"*"}
	}
	handler = middleware.CORS(corsOrigins)(handler)

	// Apply rate limiting if configured
	if r.rateLimiter != nil && r.rateLimitConfig != nil && r.rateLimitConfig.Enabled {
		window := time.Duration(r.rateLimitConfig.WindowSecs) * time.Second
		handler = middleware.RateLimitMiddleware(r.rateLimiter, r.rateLimitConfig.Limit, window)(handler)
	}

	handler = middleware.Recoverer(handler)
	if appSentry.Initialized() {
		sentryHandler := sentryhttp.New(sentryhttp.Options{Repanic: false})
		handler = sentryHandler.Handle(handler)
	}
	handler = middleware.RequestContext(handler) // Extract IP and User-Agent into context
	handler = middleware.Logger(handler)

	return handler
}

// methodHandler creates a handler that only accepts specific HTTP methods
func (r *Router) methodHandler(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != method {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return
		}
		handler(w, req)
	}
}

// authenticated wraps a handler with authentication middleware (requires full token)
func (r *Router) authenticated(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.jwtAuth.Authenticate(http.HandlerFunc(handler)).ServeHTTP(w, req)
	}
}

// optionalAuth wraps a handler with optional authentication middleware.
// If a valid token is present, claims are set in context; otherwise the handler runs without claims.
func (r *Router) optionalAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.jwtAuth.OptionalAuthenticate(http.HandlerFunc(handler)).ServeHTTP(w, req)
	}
}

// authenticatedMFASetup wraps a handler with authentication for MFA setup token
func (r *Router) authenticatedMFASetup(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler), middleware.TokenTypeMFASetup).ServeHTTP(w, req)
	}
}

// authenticatedPendingMFA wraps a handler with authentication for pending MFA token
func (r *Router) authenticatedPendingMFA(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler), middleware.TokenTypePendingMFA).ServeHTTP(w, req)
	}
}

// authenticatedEmailUnverified wraps a handler with authentication for email unverified token
func (r *Router) authenticatedEmailUnverified(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler), middleware.TokenTypeEmailUnverified).ServeHTTP(w, req)
	}
}

// handleUserMe handles GET and DELETE /api/v1/users/me
func (r *Router) handleUserMe(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		r.auth.GetCurrentUser(w, req)
	case http.MethodDelete:
		r.auth.DeleteAccount(w, req)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleRegions handles /api/v1/communities
func (r *Router) handleRegions(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		r.authenticated(r.regions.List)(w, req)
	case http.MethodPost:
		r.authenticated(r.regions.Create)(w, req)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleRegionByID handles /api/v1/communities/:id and sub-routes
func (r *Router) handleRegionByID(w http.ResponseWriter, req *http.Request) {
	// Extract ID from path
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/communities/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Community ID required")
		return
	}

	parts := strings.Split(path, "/")
	regionID := parts[0]

	// Check for membership-requests sub-route
	if len(parts) >= 2 && parts[1] == "membership-requests" {
		q := req.URL.Query()
		q.Set("id", regionID)
		req.URL.RawQuery = q.Encode()

		switch req.Method {
		case http.MethodGet:
			r.authenticated(r.membership.ListPendingForRegion)(w, req)
		case http.MethodPost:
			r.authenticated(r.membership.CreateRequest)(w, req)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
		return
	}

	// Check for invitations sub-route
	if len(parts) >= 2 && parts[1] == "invitations" {
		q := req.URL.Query()
		q.Set("id", regionID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.membership.CreateInvitation)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Check for blocklist-proposals sub-route
	if len(parts) >= 2 && parts[1] == "blocklist-proposals" {
		q := req.URL.Query()
		q.Set("id", regionID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.blocklistProposals.CreateProposal)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Check for reports sub-route
	if len(parts) >= 2 && parts[1] == "reports" {
		q := req.URL.Query()
		q.Set("id", regionID)
		q.Set("scope", "region")
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.userReports.CreateReport)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Check for members sub-route (member-visible, email-stripped)
	if len(parts) >= 2 && parts[1] == "members" {
		q := req.URL.Query()
		q.Set("id", regionID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodGet {
			r.authenticated(r.regions.ListMembers)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Check for users sub-route
	if len(parts) >= 2 && parts[1] == "users" {
		q := req.URL.Query()
		q.Set("id", regionID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodGet {
			r.authenticated(r.regions.ListUsersInRegion)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Add ID to query params for handler
	q := req.URL.Query()
	q.Set("id", regionID)
	req.URL.RawQuery = q.Encode()

	switch req.Method {
	case http.MethodGet:
		r.authenticated(r.regions.Get)(w, req)
	case http.MethodPut:
		r.authenticated(r.regions.Update)(w, req)
	case http.MethodDelete:
		r.authenticated(r.regions.Delete)(w, req)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleSignalGroups handles /api/v1/signal-groups
func (r *Router) handleSignalGroups(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		r.authenticated(r.signalGroups.List)(w, req)
	case http.MethodPost:
		r.authenticated(r.signalGroups.Create)(w, req)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleSignalGroupByID handles /api/v1/signal-groups/:id and sub-routes
func (r *Router) handleSignalGroupByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/signal-groups/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Signal group ID required")
		return
	}

	groupID := parts[0]

	// Check for secret-proposals sub-route
	if len(parts) >= 2 && parts[1] == "secret-proposals" {
		q := req.URL.Query()
		q.Set("id", groupID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.secretUpdates.CreateProposal)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Regular signal group operations
	q := req.URL.Query()
	q.Set("id", groupID)
	req.URL.RawQuery = q.Encode()

	switch req.Method {
	case http.MethodPut:
		r.authenticated(r.signalGroups.Update)(w, req)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleSecretProposal handles /api/v1/secret-proposals/:id and sub-routes
func (r *Router) handleSecretProposal(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/secret-proposals/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Proposal ID required")
		return
	}

	proposalID := parts[0]

	// Check for vote sub-route
	if len(parts) >= 2 && parts[1] == "vote" {
		q := req.URL.Query()
		q.Set("id", proposalID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.secretUpdates.Vote)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Check for expire sub-route (superuser only)
	if len(parts) >= 2 && parts[1] == "expire" {
		q := req.URL.Query()
		q.Set("id", proposalID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.secretUpdates.ExpireProposal)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Handle GET request for proposal details
	if req.Method == http.MethodGet {
		q := req.URL.Query()
		q.Set("id", proposalID)
		req.URL.RawQuery = q.Encode()
		r.authenticated(r.secretUpdates.GetProposal)(w, req)
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
}

// handleEncryptedSecretByID handles /api/v1/encrypted-secrets/:id and sub-routes
func (r *Router) handleEncryptedSecretByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/encrypted-secrets/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Secret ID required")
		return
	}

	secretID := parts[0]

	// Check for finalize sub-route
	if len(parts) >= 2 && parts[1] == "finalize" {
		q := req.URL.Query()
		q.Set("id", secretID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.secretUpdates.Finalize)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	writeError(w, http.StatusNotFound, "not_found", "Endpoint not found")
}

// handleVouchStatus handles /api/v1/verification/vouch/status/:user_id
func (r *Router) handleVouchStatus(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/verification/vouch/status/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "User ID required")
		return
	}

	q := req.URL.Query()
	q.Set("user_id", path)
	req.URL.RawQuery = q.Encode()

	if req.Method == http.MethodGet {
		r.verification.GetVouchStatus(w, req)
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
}

// handleAdminUserByID handles /api/v1/admin/users/:id and /api/v1/admin/users/:id/grant-vouch, etc.
func (r *Router) handleAdminUserByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/admin/users/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "User ID required")
		return
	}

	// Check for action suffixes
	if strings.HasSuffix(path, "/grant-vouch") {
		userID := strings.TrimSuffix(path, "/grant-vouch")
		q := req.URL.Query()
		q.Set("id", userID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.admin.GrantVouchVerification)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	if strings.HasSuffix(path, "/revoke-vouch") {
		userID := strings.TrimSuffix(path, "/revoke-vouch")
		q := req.URL.Query()
		q.Set("id", userID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.admin.RevokeVouchVerification)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	if strings.HasSuffix(path, "/block") {
		userID := strings.TrimSuffix(path, "/block")
		q := req.URL.Query()
		q.Set("id", userID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.admin.BlockUser)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	if strings.HasSuffix(path, "/unblock") {
		userID := strings.TrimSuffix(path, "/unblock")
		q := req.URL.Query()
		q.Set("id", userID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.admin.UnblockUser)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Default: get or delete user by ID
	q := req.URL.Query()
	q.Set("id", path)
	req.URL.RawQuery = q.Encode()

	switch req.Method {
	case http.MethodGet:
		r.authenticated(r.admin.GetUser)(w, req)
	case http.MethodDelete:
		r.authenticated(r.admin.DeleteUser)(w, req)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleMembershipRequests handles /api/v1/membership-requests
func (r *Router) handleMembershipRequests(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodGet {
		r.authenticated(r.membership.ListUserRequests)(w, req)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
}

// handleMembershipRequestByID handles /api/v1/membership-requests/:id and sub-routes
func (r *Router) handleMembershipRequestByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/membership-requests/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Request ID required")
		return
	}

	parts := strings.Split(path, "/")
	requestID := parts[0]

	// Check for vote sub-route
	if len(parts) >= 2 && parts[1] == "vote" {
		q := req.URL.Query()
		q.Set("id", requestID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.membership.VoteOnRequest)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Add ID to query params
	q := req.URL.Query()
	q.Set("id", requestID)
	req.URL.RawQuery = q.Encode()

	switch req.Method {
	case http.MethodGet:
		r.authenticated(r.membership.GetRequest)(w, req)
	case http.MethodDelete:
		r.authenticated(r.membership.CancelRequest)(w, req)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleInvitationByID handles /api/v1/invitations/:id and sub-routes
func (r *Router) handleInvitationByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/invitations/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Invitation ID required")
		return
	}

	parts := strings.Split(path, "/")
	invitationID := parts[0]

	// Check for respond sub-route
	if len(parts) >= 2 && parts[1] == "respond" {
		q := req.URL.Query()
		q.Set("id", invitationID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.membership.RespondToInvitation)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	writeError(w, http.StatusNotFound, "not_found", "Endpoint not found")
}

// handleBlocklistProposal handles /api/v1/blocklist-proposals/:id and sub-routes
func (r *Router) handleBlocklistProposal(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/blocklist-proposals/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Proposal ID required")
		return
	}

	proposalID := parts[0]

	// Check for vote sub-route
	if len(parts) >= 2 && parts[1] == "vote" {
		q := req.URL.Query()
		q.Set("id", proposalID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.blocklistProposals.VoteOnProposal)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Check for expire sub-route (superuser only)
	if len(parts) >= 2 && parts[1] == "expire" {
		q := req.URL.Query()
		q.Set("id", proposalID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.blocklistProposals.ExpireProposal)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Handle GET request for proposal details
	if req.Method == http.MethodGet {
		q := req.URL.Query()
		q.Set("id", proposalID)
		req.URL.RawQuery = q.Encode()
		r.authenticated(r.blocklistProposals.GetProposal)(w, req)
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
}

// handleSchools handles /api/v1/schools
func (r *Router) handleSchools(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodGet {
		r.authenticated(r.schools.Search)(w, req)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
}

// handleSchoolByID handles /api/v1/schools/:id and sub-routes
func (r *Router) handleSchoolByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/schools/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "School ID required")
		return
	}

	parts := strings.Split(path, "/")
	schoolID := parts[0]

	// Sub-routes
	if len(parts) >= 2 {
		switch parts[1] {
		case "join":
			q := req.URL.Query()
			q.Set("id", schoolID)
			req.URL.RawQuery = q.Encode()
			if req.Method == http.MethodPost {
				r.authenticated(r.schools.Join)(w, req)
				return
			}
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return

		case "leave":
			q := req.URL.Query()
			q.Set("id", schoolID)
			req.URL.RawQuery = q.Encode()
			if req.Method == http.MethodPost {
				r.authenticated(r.schools.Leave)(w, req)
				return
			}
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return

		case "vouch":
			// Check for /vouch/pending sub-route
			if len(parts) >= 3 && parts[2] == "pending" {
				q := req.URL.Query()
				q.Set("id", schoolID)
				req.URL.RawQuery = q.Encode()
				if req.Method == http.MethodGet {
					r.authenticated(r.schools.GetPendingVouchRequests)(w, req)
					return
				}
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
				return
			}
			q := req.URL.Query()
			q.Set("id", schoolID)
			req.URL.RawQuery = q.Encode()
			if req.Method == http.MethodPost {
				r.authenticated(r.schools.Vouch)(w, req)
				return
			}
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return

		case "vouch-status":
			if len(parts) >= 3 {
				q := req.URL.Query()
				q.Set("id", schoolID)
				q.Set("user_id", parts[2])
				req.URL.RawQuery = q.Encode()
				if req.Method == http.MethodGet {
					r.authenticated(r.schools.GetVouchStatus)(w, req)
					return
				}
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
				return
			}
			writeError(w, http.StatusBadRequest, "missing_user_id", "User ID required")
			return

		case "members":
			q := req.URL.Query()
			q.Set("id", schoolID)
			req.URL.RawQuery = q.Encode()
			if req.Method == http.MethodGet {
				r.authenticated(r.schools.ListMembers)(w, req)
				return
			}
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return

		case "signal-groups":
			q := req.URL.Query()
			q.Set("id", schoolID)
			req.URL.RawQuery = q.Encode()
			switch req.Method {
			case http.MethodGet:
				r.authenticated(r.schools.ListSignalGroups)(w, req)
			case http.MethodPost:
				r.authenticated(r.schools.CreateSignalGroup)(w, req)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			}
			return

		case "reports":
			q := req.URL.Query()
			q.Set("id", schoolID)
			q.Set("scope", "school")
			req.URL.RawQuery = q.Encode()
			if req.Method == http.MethodPost {
				r.authenticated(r.userReports.CreateReport)(w, req)
				return
			}
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return
		}
	}

	// Default: get school by ID
	q := req.URL.Query()
	q.Set("id", schoolID)
	req.URL.RawQuery = q.Encode()

	if req.Method == http.MethodGet {
		r.authenticated(r.schools.Get)(w, req)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
}

// handleSchoolDistrictByID handles /api/v1/school-districts/:id and sub-routes
func (r *Router) handleSchoolDistrictByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/school-districts/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "District ID required")
		return
	}

	parts := strings.Split(path, "/")
	districtID := parts[0]

	// Sub-routes
	if len(parts) >= 2 && parts[1] == "members" {
		q := req.URL.Query()
		q.Set("id", districtID)
		req.URL.RawQuery = q.Encode()
		if req.Method == http.MethodGet {
			r.authenticated(r.schools.ListDistrictMembers)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	if len(parts) >= 2 && parts[1] == "signal-groups" {
		q := req.URL.Query()
		q.Set("id", districtID)
		req.URL.RawQuery = q.Encode()
		switch req.Method {
		case http.MethodGet:
			r.authenticated(r.schools.ListDistrictSignalGroups)(w, req)
		case http.MethodPost:
			r.authenticated(r.schools.CreateDistrictSignalGroup)(w, req)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
		return
	}

	if len(parts) >= 2 && parts[1] == "reports" {
		q := req.URL.Query()
		q.Set("id", districtID)
		q.Set("scope", "district")
		req.URL.RawQuery = q.Encode()
		if req.Method == http.MethodPost {
			r.authenticated(r.userReports.CreateReport)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Default: get district by ID
	q := req.URL.Query()
	q.Set("id", districtID)
	req.URL.RawQuery = q.Encode()

	if req.Method == http.MethodGet {
		r.authenticated(r.schools.GetDistrict)(w, req)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
}

// handleDeletionProposals handles /api/v1/deletion-proposals
func (r *Router) handleDeletionProposals(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		r.authenticated(r.deletionProposals.ListProposals)(w, req)
	case http.MethodPost:
		r.authenticated(r.deletionProposals.CreateProposal)(w, req)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleDeletionProposal handles /api/v1/deletion-proposals/:id and sub-routes
func (r *Router) handleDeletionProposal(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/deletion-proposals/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Proposal ID required")
		return
	}

	proposalID := parts[0]

	// Check for vote sub-route
	if len(parts) >= 2 && parts[1] == "vote" {
		q := req.URL.Query()
		q.Set("id", proposalID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.deletionProposals.VoteOnProposal)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Check for expire sub-route
	if len(parts) >= 2 && parts[1] == "expire" {
		q := req.URL.Query()
		q.Set("id", proposalID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.deletionProposals.ExpireProposal)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Handle GET request for proposal details
	if req.Method == http.MethodGet {
		q := req.URL.Query()
		q.Set("id", proposalID)
		req.URL.RawQuery = q.Encode()
		r.authenticated(r.deletionProposals.GetProposal)(w, req)
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
}

// handleBlockedAddress handles /api/v1/admin/blocked-addresses/:hash/expire
func (r *Router) handleBlockedAddress(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/admin/blocked-addresses/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing_hash", "Address hash required")
		return
	}

	addressHash := parts[0]

	// Check for expire sub-route
	if len(parts) >= 2 && parts[1] == "expire" {
		q := req.URL.Query()
		q.Set("hash", addressHash)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.blocklistProposals.ExpireAddress)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	writeError(w, http.StatusNotFound, "not_found", "Endpoint not found")
}

// handleReportByID handles /api/v1/reports/:id and sub-routes
func (r *Router) handleReportByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/reports/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Report ID required")
		return
	}

	reportID := parts[0]

	// Check for resolve sub-route
	if len(parts) >= 2 && parts[1] == "resolve" {
		q := req.URL.Query()
		q.Set("id", reportID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.userReports.ResolveReport)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Handle GET request for report details
	if req.Method == http.MethodGet {
		q := req.URL.Query()
		q.Set("id", reportID)
		req.URL.RawQuery = q.Encode()
		r.authenticated(r.userReports.GetReport)(w, req)
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
}

// handleEncryptionKeys handles GET/POST/PUT /api/v1/encryption/keys
func (r *Router) handleEncryptionKeys(w http.ResponseWriter, req *http.Request) {
	if r.encryption == nil {
		writeError(w, http.StatusNotFound, "not_found", "Endpoint not available")
		return
	}
	switch req.Method {
	case http.MethodGet:
		r.authenticated(r.encryption.GetKeys)(w, req)
	case http.MethodPost:
		r.authenticated(r.encryption.UploadKeys)(w, req)
	case http.MethodPut:
		r.authenticated(r.encryption.UpdateKeys)(w, req)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleEncryptionKeysRotate handles POST /api/v1/encryption/keys/rotate
func (r *Router) handleEncryptionKeysRotate(w http.ResponseWriter, req *http.Request) {
	if r.encryption == nil {
		writeError(w, http.StatusNotFound, "not_found", "Endpoint not available")
		return
	}
	r.encryption.RotateKeys(w, req)
}

// handleEncryptionPublicKeys handles GET /api/v1/encryption/public-keys
func (r *Router) handleEncryptionPublicKeys(w http.ResponseWriter, req *http.Request) {
	if r.encryption == nil {
		writeError(w, http.StatusNotFound, "not_found", "Endpoint not available")
		return
	}
	r.encryption.GetPublicKeys(w, req)
}

// handleEncryptionPendingRekeys handles GET /api/v1/encryption/pending-rekeys
func (r *Router) handleEncryptionPendingRekeys(w http.ResponseWriter, req *http.Request) {
	if r.encryption == nil {
		writeError(w, http.StatusNotFound, "not_found", "Endpoint not available")
		return
	}
	r.encryption.GetPendingRekeys(w, req)
}

// handleEncryptionRekey handles POST /api/v1/encryption/rekey
func (r *Router) handleEncryptionRekey(w http.ResponseWriter, req *http.Request) {
	if r.encryption == nil {
		writeError(w, http.StatusNotFound, "not_found", "Endpoint not available")
		return
	}
	r.encryption.SubmitRekeys(w, req)
}

// handleMeshtasticChannels handles /api/v1/meshtastic-channels
func (r *Router) handleMeshtasticChannels(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		r.authenticated(r.meshtastic.List)(w, req)
	case http.MethodPost:
		r.authenticated(r.meshtastic.Create)(w, req)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleMeshtasticChannelByID handles /api/v1/meshtastic-channels/:id and sub-routes
func (r *Router) handleMeshtasticChannelByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/meshtastic-channels/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Meshtastic channel ID required")
		return
	}

	channelID := parts[0]

	// Check for secret-proposals sub-route
	if len(parts) >= 2 && parts[1] == "secret-proposals" {
		q := req.URL.Query()
		q.Set("channel_id", channelID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.secretUpdates.CreateProposal)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Regular meshtastic channel operations
	q := req.URL.Query()
	q.Set("id", channelID)
	req.URL.RawQuery = q.Encode()

	switch req.Method {
	case http.MethodPut:
		r.authenticated(r.meshtastic.Update)(w, req)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}
