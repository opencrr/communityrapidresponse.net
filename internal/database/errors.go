package database

import "errors"

// Sentinel errors for transaction control flow.
// Handlers use errors.Is() after db.Transaction() to map these to HTTP responses.
var (
	ErrProposalClosed    = errors.New("proposal is no longer pending")
	ErrPendingExists     = errors.New("pending proposal already exists")
	ErrLimitReached      = errors.New("limit reached")
	ErrRateLimited       = errors.New("rate limited")
	ErrAlreadyVerified   = errors.New("already verified")
	ErrAlreadyVouched    = errors.New("already vouched")
	ErrVouchLimitReached = errors.New("monthly vouch limit reached")
	ErrSchoolNotFound    = errors.New("school not found")
	ErrDistrictNotFound  = errors.New("district not found")
	ErrAlreadyMember          = errors.New("already a member of this school")
	ErrSchoolBlocked          = errors.New("user is blocked from this school")
	ErrSecretProposalNotFound = errors.New("secret proposal not found")
	ErrWrappedKeyNotFound     = errors.New("wrapped key not found")
)
