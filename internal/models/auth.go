package models

// RegisterRequest represents user registration request
type RegisterRequest struct {
	Username string `json:"username" validate:"required,min=3,max=50"`
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=12"`
}

// RegisterResponse represents user registration response
type RegisterResponse struct {
	UserID                string `json:"user_id"`
	Username              string `json:"username"`
	Email                 string `json:"email"`
	EmailVerificationSent bool   `json:"email_verification_sent"`
	Message               string `json:"message,omitempty"`
}

// LoginRequest represents user login request.
// The Email field accepts either an email address or username.
type LoginRequest struct {
	Email    string `json:"email" validate:"required"`
	Password string `json:"password" validate:"required"`
}

// LoginResponse represents user login response
type LoginResponse struct {
	Token string      `json:"token"`
	User  *PublicUser `json:"user"`
}

// TokenClaims represents JWT token claims
type TokenClaims struct {
	UserID           string           `json:"user_id"`
	Username         string           `json:"username"`
	Email            string           `json:"email"`
	VerificationTier VerificationTier `json:"verification_tier"`
}
