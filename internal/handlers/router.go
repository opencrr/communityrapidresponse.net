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
	mux            *http.ServeMux
	auth           *AuthHandler
	mfa            *MFAHandler
	regions        *RegionHandler
	verification   *VerificationHandler
	admin          *AdminHandler
	schools        *SchoolHandler
	encryption     *EncryptionHandler
	groups         *GroupHandler
	connections    *ConnectionHandler
	jwtAuth        *middleware.JWTAuth
	rateLimiter    services.RateLimiter
	rateLimitConfig *RateLimitOptions
	csrfConfig     *CSRFConfig
	corsOrigins    []string
	securityConfig *middleware.SecurityConfig
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
	verification *VerificationHandler,
	admin *AdminHandler,
	schools *SchoolHandler,
	encryption *EncryptionHandler,
	groups *GroupHandler,
	connections *ConnectionHandler,
	jwtAuth *middleware.JWTAuth,
	rateLimiter services.RateLimiter,
	rateLimitConfig *RateLimitOptions,
	csrfConfig *CSRFConfig,
	corsOrigins []string,
	securityConfig *middleware.SecurityConfig,
) *Router {
	return &Router{
		mux:             http.NewServeMux(),
		auth:            auth,
		mfa:             mfa,
		regions:         regions,
		verification:    verification,
		admin:           admin,
		schools:         schools,
		encryption:      encryption,
		groups:          groups,
		connections:     connections,
		jwtAuth:         jwtAuth,
		rateLimiter:     rateLimiter,
		rateLimitConfig: rateLimitConfig,
		csrfConfig:      csrfConfig,
		corsOrigins:     corsOrigins,
		securityConfig:  securityConfig,
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
	r.mux.HandleFunc("/api/v1/communities/", r.handleRegionByID)

	// Group routes (nil-safe for tests that don't need groups)
	if r.groups != nil {
		r.mux.HandleFunc("/api/v1/groups", r.handleGroups)
		r.mux.HandleFunc("/api/v1/groups/", r.handleGroupByID)
		r.mux.HandleFunc("/api/v1/group-invitations", r.authenticated(r.methodHandler(http.MethodGet, r.groups.ListMyInvitations)))
		r.mux.HandleFunc("/api/v1/group-invitations/", r.handleGroupInvitationByID)
		r.mux.HandleFunc("/api/v1/topic-board", r.authenticated(r.methodHandler(http.MethodGet, r.groups.BrowsePostings)))
	}

	// Connection routes (nil-safe for tests that don't need connections)
	if r.connections != nil {
		r.mux.HandleFunc("/api/v1/connections", r.handleConnections)
		r.mux.HandleFunc("/api/v1/connections/", r.handleConnectionByID)
		r.mux.HandleFunc("/api/v1/connection-proposals", r.authenticated(r.methodHandler(http.MethodGet, r.connections.ListPendingProposals)))
		r.mux.HandleFunc("/api/v1/connection-proposals/", r.handleConnectionProposalByID)
		r.mux.HandleFunc("/api/v1/connection-chat-proposals/", r.handleConnectionChatProposalByID)
	}

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

	// Protected routes - encryption keys
	r.mux.HandleFunc("/api/v1/encryption/keys", r.handleEncryptionKeys)
	r.mux.HandleFunc("/api/v1/encryption/keys/rotate", r.authenticated(r.methodHandler(http.MethodPost, r.handleEncryptionKeysRotate)))
	r.mux.HandleFunc("/api/v1/encryption/public-keys", r.authenticated(r.methodHandler(http.MethodGet, r.handleEncryptionPublicKeys)))
	r.mux.HandleFunc("/api/v1/encryption/pending-rekeys", r.authenticated(r.methodHandler(http.MethodGet, r.handleEncryptionPendingRekeys)))
	r.mux.HandleFunc("/api/v1/encryption/rekey", r.authenticated(r.methodHandler(http.MethodPost, r.handleEncryptionRekey)))

	// Admin routes (superuser only)
	r.mux.HandleFunc("/api/v1/admin/users", r.authenticated(r.methodHandler(http.MethodGet, r.admin.ListUsers)))
	r.mux.HandleFunc("/api/v1/admin/users/", r.handleAdminUserByID)
	r.mux.HandleFunc("/api/v1/admin/audit-logs", r.authenticated(r.methodHandler(http.MethodGet, r.admin.GetAuditLogs)))
	r.mux.HandleFunc("/api/v1/admin/audit-logs/export", r.authenticated(r.methodHandler(http.MethodGet, r.admin.ExportAuditLogs)))

	// Health check (both paths for compatibility)
	healthHandler := func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
	r.mux.HandleFunc("/health", healthHandler)
	r.mux.HandleFunc("/api/v1/health", healthHandler)

	// Apply middleware chain
	handler := http.Handler(r.mux)
	handler = middleware.ContentType(handler)
	handler = middleware.MaxBodySize(1 << 20)(handler) // 1MB request body limit
	if r.csrfConfig != nil && r.csrfConfig.Enabled {
		handler = middleware.CSRFProtection(r.csrfConfig.Secret, r.csrfConfig.SecureCookies)(handler)
	}
	handler = middleware.SecurityHeadersWithConfig(r.securityConfig)(handler)
	handler = middleware.CORS(r.corsOrigins)(handler)

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

	// Reject malformed IDs early (defense-in-depth; carried over from main's
	// router hardening).
	if !isValidUUID(regionID) {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid community ID")
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

	// Reject malformed IDs early (defense-in-depth; carried over from main's
	// router hardening).
	if !isValidUUID(schoolID) {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid school ID")
		return
	}

	// Sub-routes
	if len(parts) >= 2 && parts[1] == "join" {
		q := req.URL.Query()
		q.Set("id", schoolID)
		req.URL.RawQuery = q.Encode()
		if req.Method == http.MethodPost {
			r.authenticated(r.schools.Join)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
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

	// Reject malformed IDs early (defense-in-depth; carried over from main's
	// router hardening).
	if !isValidUUID(districtID) {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid district ID")
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


// handleGroups handles /api/v1/groups
func (r *Router) handleGroups(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		r.authenticated(r.groups.List)(w, req)
	case http.MethodPost:
		r.authenticated(r.groups.Create)(w, req)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleGroupByID handles /api/v1/groups/:id and sub-routes
func (r *Router) handleGroupByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/groups/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Group ID required")
		return
	}

	parts := strings.Split(path, "/")
	groupID := parts[0]

	// Check for members sub-route
	if len(parts) >= 2 && parts[1] == "members" {
		q := req.URL.Query()
		q.Set("id", groupID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodGet {
			r.authenticated(r.groups.ListMembers)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Check for blocked-users sub-route:
	//   POST   /api/v1/groups/{id}/blocked-users          → ban a user
	//   DELETE /api/v1/groups/{id}/blocked-users/{userID} → lift a ban
	if len(parts) >= 2 && parts[1] == "blocked-users" {
		q := req.URL.Query()
		q.Set("id", groupID)
		if len(parts) >= 3 && parts[2] != "" {
			q.Set("user_id", parts[2])
			req.URL.RawQuery = q.Encode()
			if req.Method == http.MethodDelete {
				r.authenticated(r.groups.UnblockMember)(w, req)
				return
			}
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return
		}
		req.URL.RawQuery = q.Encode()
		if req.Method == http.MethodPost {
			r.authenticated(r.groups.BlockMember)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Check for leave sub-route
	if len(parts) >= 2 && parts[1] == "leave" {
		q := req.URL.Query()
		q.Set("id", groupID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.groups.Leave)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Check for resources sub-route: /api/v1/groups/{id}/resources[/{rid}]
	if len(parts) >= 2 && parts[1] == "resources" {
		q := req.URL.Query()
		q.Set("id", groupID)
		req.URL.RawQuery = q.Encode()

		if len(parts) >= 3 && parts[2] != "" {
			// /api/v1/groups/{id}/resources/{rid}
			q.Set("rid", parts[2])
			req.URL.RawQuery = q.Encode()

			switch req.Method {
			case http.MethodPut:
				r.authenticated(r.groups.UpdateResource)(w, req)
			case http.MethodDelete:
				r.authenticated(r.groups.DeleteResource)(w, req)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			}
			return
		}

		// /api/v1/groups/{id}/resources
		switch req.Method {
		case http.MethodPost:
			r.authenticated(r.groups.CreateResource)(w, req)
		case http.MethodGet:
			r.authenticated(r.groups.ListResources)(w, req)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
		return
	}

	// Check for signal-groups sub-route: /api/v1/groups/{id}/signal-groups
	if len(parts) >= 2 && parts[1] == "signal-groups" {
		q := req.URL.Query()
		q.Set("id", groupID)
		req.URL.RawQuery = q.Encode()

		switch req.Method {
		case http.MethodPost:
			r.authenticated(r.groups.CreateSignalGroup)(w, req)
		case http.MethodGet:
			r.authenticated(r.groups.ListSignalGroups)(w, req)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
		return
	}

	// Check for meshtastic-channels sub-route: /api/v1/groups/{id}/meshtastic-channels
	if len(parts) >= 2 && parts[1] == "meshtastic-channels" {
		q := req.URL.Query()
		q.Set("id", groupID)
		req.URL.RawQuery = q.Encode()

		switch req.Method {
		case http.MethodPost:
			r.authenticated(r.groups.CreateMeshtasticChannel)(w, req)
		case http.MethodGet:
			r.authenticated(r.groups.ListMeshtasticChannels)(w, req)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
		return
	}

	// Check for invite-links sub-route: /api/v1/groups/{id}/invite-links
	if len(parts) >= 2 && parts[1] == "invite-links" {
		q := req.URL.Query()
		q.Set("id", groupID)
		req.URL.RawQuery = q.Encode()

		switch req.Method {
		case http.MethodPost:
			r.authenticated(r.groups.CreateInviteLink)(w, req)
		case http.MethodGet:
			r.authenticated(r.groups.ListInviteLinks)(w, req)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
		return
	}

	// Check for invitations sub-route: /api/v1/groups/{id}/invitations
	if len(parts) >= 2 && parts[1] == "invitations" {
		q := req.URL.Query()
		q.Set("id", groupID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.groups.CreateInvitation)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Check for trust-vouches sub-route: /api/v1/groups/{id}/trust-vouches[/{user_id}]
	if len(parts) >= 2 && parts[1] == "trust-vouches" {
		q := req.URL.Query()
		q.Set("id", groupID)
		req.URL.RawQuery = q.Encode()

		// GET /api/v1/groups/{id}/trust-vouches/{user_id}
		if len(parts) >= 3 && parts[2] != "" {
			q.Set("user_id", parts[2])
			req.URL.RawQuery = q.Encode()

			if req.Method == http.MethodGet {
				r.authenticated(r.groups.GetTrustVouchStatus)(w, req)
				return
			}
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return
		}

		// POST /api/v1/groups/{id}/trust-vouches
		if req.Method == http.MethodPost {
			r.authenticated(r.groups.VouchForMember)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Check for browse sub-route: /api/v1/groups/browse
	if groupID == "browse" {
		if req.Method == http.MethodGet {
			r.authenticated(r.groups.Browse)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Check for join sub-route: /api/v1/groups/join/{token}
	// Here groupID == "join" and parts[1] is the token
	if groupID == "join" && len(parts) >= 2 {
		q := req.URL.Query()
		q.Set("token", parts[1])
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.groups.JoinViaLink)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Check for topic-board sub-route: /api/v1/groups/{id}/topic-board
	if len(parts) >= 2 && parts[1] == "topic-board" {
		q := req.URL.Query()
		q.Set("id", groupID)
		req.URL.RawQuery = q.Encode()

		switch req.Method {
		case http.MethodPost:
			r.authenticated(r.groups.CreateOrUpdatePosting)(w, req)
		case http.MethodGet:
			r.authenticated(r.groups.GetPosting)(w, req)
		case http.MethodDelete:
			r.authenticated(r.groups.RemovePosting)(w, req)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
		return
	}

	// Check for blocks sub-route: /api/v1/groups/{id}/blocks[/{gid}]
	if len(parts) >= 2 && parts[1] == "blocks" {
		q := req.URL.Query()
		q.Set("id", groupID)
		req.URL.RawQuery = q.Encode()

		if len(parts) >= 3 && parts[2] != "" {
			// /api/v1/groups/{id}/blocks/{gid}
			q.Set("gid", parts[2])
			req.URL.RawQuery = q.Encode()

			if req.Method == http.MethodDelete {
				r.authenticated(r.groups.UnblockGroup)(w, req)
				return
			}
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return
		}

		// /api/v1/groups/{id}/blocks
		switch req.Method {
		case http.MethodPost:
			r.authenticated(r.groups.BlockGroup)(w, req)
		case http.MethodGet:
			r.authenticated(r.groups.ListBlockedGroups)(w, req)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
		return
	}

	// Default: CRUD operations on group by ID
	q := req.URL.Query()
	q.Set("id", groupID)
	req.URL.RawQuery = q.Encode()

	switch req.Method {
	case http.MethodGet:
		r.authenticated(r.groups.Get)(w, req)
	case http.MethodPut:
		r.authenticated(r.groups.Update)(w, req)
	case http.MethodDelete:
		r.authenticated(r.groups.Delete)(w, req)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleGroupInvitationByID handles /api/v1/group-invitations/:id/respond
func (r *Router) handleGroupInvitationByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/group-invitations/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Invitation ID required")
		return
	}

	parts := strings.Split(path, "/")
	invitationID := parts[0]

	if len(parts) >= 2 && parts[1] == "respond" {
		q := req.URL.Query()
		q.Set("id", invitationID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.groups.RespondToInvitation)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	writeError(w, http.StatusNotFound, "not_found", "Not found")
}

// handleConnections handles /api/v1/connections
func (r *Router) handleConnections(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodPost:
		r.authenticated(r.connections.ProposeConnection)(w, req)
	case http.MethodGet:
		r.authenticated(r.connections.ListMyConnections)(w, req)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleConnectionByID handles /api/v1/connections/{id} and sub-routes
func (r *Router) handleConnectionByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/connections/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Connection ID required")
		return
	}

	parts := strings.Split(path, "/")
	connectionID := parts[0]

	q := req.URL.Query()
	q.Set("id", connectionID)
	req.URL.RawQuery = q.Encode()

	// POST /api/v1/connections/{id}/invite
	if len(parts) >= 2 && parts[1] == "invite" {
		if req.Method == http.MethodPost {
			r.authenticated(r.connections.InviteToConnection)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// POST /api/v1/connections/{id}/leave
	if len(parts) >= 2 && parts[1] == "leave" {
		if req.Method == http.MethodPost {
			r.authenticated(r.connections.LeaveConnection)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// POST/GET /api/v1/connections/{id}/signal-group-proposals
	if len(parts) >= 2 && parts[1] == "signal-group-proposals" {
		switch req.Method {
		case http.MethodPost:
			r.authenticated(r.connections.ProposeSignalChat)(w, req)
		case http.MethodGet:
			r.authenticated(r.connections.ListChatProposals)(w, req)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
		return
	}

	// POST/GET/DELETE /api/v1/connections/{id}/shared-resources[/{rid}]
	if len(parts) >= 2 && parts[1] == "shared-resources" {
		if len(parts) >= 3 && parts[2] != "" {
			// /api/v1/connections/{id}/shared-resources/{rid}
			q := req.URL.Query()
			q.Set("resource_id", parts[2])
			req.URL.RawQuery = q.Encode()
			if req.Method == http.MethodDelete {
				r.authenticated(r.connections.UnshareResource)(w, req)
				return
			}
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return
		}
		switch req.Method {
		case http.MethodPost:
			r.authenticated(r.connections.ShareResource)(w, req)
		case http.MethodGet:
			r.authenticated(r.connections.ListConnectionResources)(w, req)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
		return
	}

	// GET /api/v1/connections/{id}/signal-groups
	if len(parts) >= 2 && parts[1] == "signal-groups" {
		if req.Method == http.MethodGet {
			r.authenticated(r.connections.ListConnectionSignalGroups)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// GET /api/v1/connections/{id}
	if req.Method == http.MethodGet {
		r.authenticated(r.connections.GetConnection)(w, req)
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
}

// handleConnectionChatProposalByID handles /api/v1/connection-chat-proposals/{id}/vote
func (r *Router) handleConnectionChatProposalByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/connection-chat-proposals/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Proposal ID required")
		return
	}

	parts := strings.Split(path, "/")
	proposalID := parts[0]

	if len(parts) >= 2 && parts[1] == "vote" {
		q := req.URL.Query()
		q.Set("id", proposalID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.connections.VoteOnChatProposal)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	writeError(w, http.StatusNotFound, "not_found", "Not found")
}

// handleConnectionProposalByID handles /api/v1/connection-proposals/{id}/respond
func (r *Router) handleConnectionProposalByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/connection-proposals/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Proposal ID required")
		return
	}

	parts := strings.Split(path, "/")
	proposalID := parts[0]

	if len(parts) >= 2 && parts[1] == "respond" {
		q := req.URL.Query()
		q.Set("id", proposalID)
		req.URL.RawQuery = q.Encode()

		if req.Method == http.MethodPost {
			r.authenticated(r.connections.RespondToProposal)(w, req)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	writeError(w, http.StatusNotFound, "not_found", "Not found")
}
