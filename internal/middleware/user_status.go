package middleware

import (
	"context"
	"time"
)

// UserStatus holds the minimal fields needed for token validation.
type UserStatus struct {
	TokenInvalidatedAt *time.Time
	IsBlocked          bool
	DeletedAt          *time.Time
}

// UserStatusChecker fetches lightweight user status for token validation.
type UserStatusChecker interface {
	GetUserStatus(ctx context.Context, userID string) (*UserStatus, error)
}
