package models

import (
	"time"
)

// Report reason constants
const (
	ReportReasonHarassment             = "harassment"
	ReportReasonSpam                   = "spam"
	ReportReasonImpersonation          = "impersonation"
	ReportReasonFraudulentVerification = "fraudulent_verification"
	ReportReasonOther                  = "other"
)

// ValidReportReasons maps valid report reason strings for validation
var ValidReportReasons = map[string]bool{
	ReportReasonHarassment:             true,
	ReportReasonSpam:                   true,
	ReportReasonImpersonation:          true,
	ReportReasonFraudulentVerification: true,
	ReportReasonOther:                  true,
}

// Report status constants
const (
	ReportStatusPending           = "pending"
	ReportStatusDismissed         = "dismissed"
	ReportStatusResolvedBlocklist = "resolved_blocklist"
)

// UserReport represents a user report in the database
type UserReport struct {
	ID                  string     `json:"id" db:"id"`
	ReporterID          string     `json:"reporter_id" db:"reporter_id"`
	ReportedUserID      string     `json:"reported_user_id" db:"reported_user_id"`
	RegionID            *string    `json:"region_id,omitempty" db:"region_id"`
	SchoolID            *string    `json:"school_id,omitempty" db:"school_id"`
	DistrictID          *string    `json:"district_id,omitempty" db:"district_id"`
	Reason              string     `json:"reason" db:"reason"`
	Details             *string    `json:"details,omitempty" db:"details"`
	Status              string     `json:"status" db:"status"`
	ResolvedBy          *string    `json:"resolved_by,omitempty" db:"resolved_by"`
	ResolutionNote      *string    `json:"resolution_note,omitempty" db:"resolution_note"`
	BlocklistProposalID *string    `json:"blocklist_proposal_id,omitempty" db:"blocklist_proposal_id"`
	CreatedAt           time.Time  `json:"created_at" db:"created_at"`
	ResolvedAt          *time.Time `json:"resolved_at,omitempty" db:"resolved_at"`
}

// CreateReportRequest represents a request to create a user report
type CreateReportRequest struct {
	ReportedUserID string  `json:"reported_user_id"`
	Reason         string  `json:"reason"`
	Details        *string `json:"details,omitempty"`
}

// CreateReportResponse represents the response after creating a report
type CreateReportResponse struct {
	ReportID string `json:"report_id"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

// ReportSummary represents a report in list views
type ReportSummary struct {
	ID               string    `json:"id"`
	ReportedUser     string    `json:"reported_user"`
	ReportedUserID   string    `json:"reported_user_id"`
	ScopeType        string    `json:"scope_type"`
	ScopeName        string    `json:"scope_name"`
	ScopeID          string    `json:"scope_id"`
	Reason           string    `json:"reason"`
	Status           string    `json:"status"`
	ReportCount      int       `json:"report_count"`
	CreatedAt        time.Time `json:"created_at"`
}

// ReportDetailResponse represents the full detail of a report
type ReportDetailResponse struct {
	ID                  string     `json:"id"`
	ReporterUsername     string     `json:"reporter_username"`
	ReportedUserID      string     `json:"reported_user_id"`
	ReportedUsername     string     `json:"reported_username"`
	ScopeType           string     `json:"scope_type"`
	ScopeID             string     `json:"scope_id"`
	ScopeName           string     `json:"scope_name"`
	Reason              string     `json:"reason"`
	Details             *string    `json:"details,omitempty"`
	Status              string     `json:"status"`
	ResolvedByUsername   *string    `json:"resolved_by_username,omitempty"`
	ResolutionNote      *string    `json:"resolution_note,omitempty"`
	BlocklistProposalID *string    `json:"blocklist_proposal_id,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	ResolvedAt          *time.Time `json:"resolved_at,omitempty"`
}

// ResolveReportRequest represents a request to resolve a report
type ResolveReportRequest struct {
	Action string  `json:"action"` // "dismiss" or "initiate_blocklist"
	Note   *string `json:"note,omitempty"`
}

// ReportListFilter represents filters for listing reports
type ReportListFilter struct {
	Status     string
	RegionID   string
	SchoolID   string
	DistrictID string
}
