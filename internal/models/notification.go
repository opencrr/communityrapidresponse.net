package models

import (
	"encoding/json"
	"time"
)

// NotificationType represents the type of email notification
type NotificationType string

const (
	NotificationTypeInviteLinkUpdated     NotificationType = "invite_link_updated"
	NotificationTypeVerificationComplete  NotificationType = "verification_complete"
	NotificationTypeVouchReceived         NotificationType = "vouch_received"
	NotificationTypeVouchComplete         NotificationType = "vouch_complete"
	NotificationTypeBlocklistProposal     NotificationType = "blocklist_proposal"
	NotificationTypeDeletionProposal      NotificationType = "deletion_proposal"
	NotificationTypeInviteLinkProposal    NotificationType = "invite_link_proposal"
	NotificationTypeSubRegionInvitation   NotificationType = "sub_region_invitation"
	NotificationTypeRekeyingNeeded       NotificationType = "rekeying_needed"
)

// NotificationStatus represents the status of a notification
type NotificationStatus string

const (
	NotificationStatusQueued NotificationStatus = "queued"
	NotificationStatusSent   NotificationStatus = "sent"
	NotificationStatusFailed NotificationStatus = "failed"
)

// EmailNotification represents an email notification record
type EmailNotification struct {
	ID               string             `json:"id" db:"id"`
	UserID           string             `json:"user_id" db:"user_id"`
	NotificationType NotificationType   `json:"notification_type" db:"notification_type"`
	ResourceType     *string            `json:"resource_type,omitempty" db:"resource_type"`
	ResourceID       *string            `json:"resource_id,omitempty" db:"resource_id"`
	Status           NotificationStatus `json:"status" db:"status"`
	QueuedAt         time.Time          `json:"queued_at" db:"queued_at"`
	SentAt           *time.Time         `json:"sent_at,omitempty" db:"sent_at"`
	ErrorMessage     *string            `json:"error_message,omitempty" db:"error_message"`
	ContentHash      *string            `json:"content_hash,omitempty" db:"content_hash"`
}

// AuditLog represents an audit log entry
type AuditLog struct {
	ID           string          `json:"id" db:"id"`
	UserID       *string         `json:"user_id,omitempty" db:"user_id"`
	Action       string          `json:"action" db:"action"`
	ResourceType *string         `json:"resource_type,omitempty" db:"resource_type"`
	ResourceID   *string         `json:"resource_id,omitempty" db:"resource_id"`
	Details      json.RawMessage `json:"details,omitempty" db:"details"`
	IPAddress    *string         `json:"ip_address,omitempty" db:"ip_address"`
	UserAgent    *string         `json:"user_agent,omitempty" db:"user_agent"`
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`
}

// AuditAction constants
const (
	AuditActionUserRegistered         = "user_registered"
	AuditActionUserLogin              = "user_login"
	AuditActionUserLoginFailed        = "user_login_failed"
	AuditActionUserLogout             = "user_logout"
	AuditActionUserVerified           = "user_verified"
	AuditActionUserVouched            = "user_vouched"
	AuditActionMFASetupStarted        = "mfa_setup_started"
	AuditActionMFASetupCompleted      = "mfa_setup_completed"
	AuditActionMFAVerified            = "mfa_verified"
	AuditActionMFAVerifyFailed        = "mfa_verify_failed"
	AuditActionPostcardRequested      = "postcard_requested"
	AuditActionPostcardVerified       = "postcard_verified"
	AuditActionVouchRequested         = "vouch_requested"
	AuditActionVouchGiven             = "vouch_given"
	AuditActionRegionCreated          = "region_created"
	AuditActionRegionUpdated          = "region_updated"
	AuditActionRegionDeleted          = "region_deleted"
	AuditActionSignalGroupCreated     = "signal_group_created"
	AuditActionSignalGroupUpdated     = "signal_group_updated"
	AuditActionSignalGroupDeleted     = "signal_group_deleted"
	AuditActionInviteLinkUpdated      = "invite_link_updated"
	AuditActionProposalCreated        = "proposal_created"
	AuditActionProposalVoted          = "proposal_voted"
	AuditActionProposalApproved       = "proposal_approved"
	AuditActionProposalRejected       = "proposal_rejected"
	AuditActionProposalExpired        = "proposal_expired"
	AuditActionUserBlocked            = "user_blocked"
	AuditActionUserUnblocked          = "user_unblocked"
	AuditActionSuperuserGrantVouch    = "superuser_grant_vouch"
	AuditActionSuperuserRevokeVouch   = "superuser_revoke_vouch"
	AuditActionSuperuserUserSearch    = "superuser_user_search"
	AuditActionSuperuserUserDelete    = "superuser_user_delete"

	// Sub-region membership actions
	AuditActionSubRegionRequestCreated  = "sub_region_request_created"
	AuditActionSubRegionRequestVoted    = "sub_region_request_voted"
	AuditActionSubRegionRequestApproved = "sub_region_request_approved"
	AuditActionSubRegionRequestExpired  = "sub_region_request_expired"
	AuditActionSubRegionInviteSent      = "sub_region_invite_sent"
	AuditActionSubRegionInviteAccepted  = "sub_region_invite_accepted"
	AuditActionSubRegionInviteDeclined  = "sub_region_invite_declined"

	// Password actions
	AuditActionPasswordChanged        = "password_changed"
	AuditActionPasswordResetRequested = "password_reset_requested"
	AuditActionPasswordResetCompleted = "password_reset_completed"

	// School actions
	AuditActionSchoolJoined             = "school_joined"
	AuditActionSchoolLeft               = "school_left"
	AuditActionSchoolVouchGiven         = "school_vouch_given"
	AuditActionSchoolVouchComplete      = "school_vouch_complete"
	AuditActionSchoolSignalGroupCreated = "school_signal_group_created"
	AuditActionSchoolUserBlocked        = "school_user_blocked"
	AuditActionSchoolUserUnblocked      = "school_user_unblocked"

	// Account deletion
	AuditActionAccountDeleted = "account_deleted"

	// User reports
	AuditActionReportCreated           = "report_created"
	AuditActionReportDismissed         = "report_dismissed"
	AuditActionReportResolvedBlocklist = "report_resolved_blocklist"

	// Encryption key actions
	AuditActionEncryptionKeyRotated = "encryption_key_rotated"
	AuditActionSecretRekeyed        = "secret_rekeyed"

	// Meshtastic channel actions
	AuditActionMeshtasticChannelCreated = "meshtastic_channel_created"
	AuditActionMeshtasticChannelUpdated = "meshtastic_channel_updated"
	AuditActionMeshtasticChannelDeleted = "meshtastic_channel_deleted"

	// Group actions
	AuditActionGroupCreated           = "group_created"
	AuditActionGroupUpdated           = "group_updated"
	AuditActionGroupDeleted           = "group_deleted"
	AuditActionGroupMemberRemoved     = "group_member_removed"
	AuditActionGroupInviteLinkCreated = "group_invite_link_created"
	AuditActionGroupInvitationSent    = "group_invitation_sent"
	AuditActionGroupMemberAdded           = "group_member_added"
	AuditActionGroupTrustVouchCreated        = "group_trust_vouch_created"
	AuditActionGroupSignalGroupCreated       = "group_signal_group_created"
	AuditActionGroupResourceCreated          = "group_resource_created"
	AuditActionGroupResourceUpdated          = "group_resource_updated"
	AuditActionGroupResourceDeleted          = "group_resource_deleted"
	AuditActionGroupBlocked                  = "group_blocked"
	AuditActionGroupUnblocked                = "group_unblocked"
	AuditActionTopicBoardPosted              = "topic_board_posted"
	AuditActionTopicBoardRemoved             = "topic_board_removed"

	// Connection actions
	AuditActionConnectionProposed         = "connection_proposed"
	AuditActionConnectionAccepted         = "connection_accepted"
	AuditActionConnectionLeft             = "connection_left"
	AuditActionConnectionChatProposed     = "connection_chat_proposed"
	AuditActionConnectionChatVoted        = "connection_chat_voted"
	AuditActionConnectionChatApproved         = "connection_chat_approved"
	AuditActionConnectionResourceShared       = "connection_resource_shared"
	AuditActionConnectionResourceUnshared     = "connection_resource_unshared"

	// Account lockout
	AuditActionAccountLocked            = "account_locked"
	AuditActionMFALocked                = "mfa_locked"
	AuditActionVerificationCodeLocked   = "verification_code_locked"
)

// AuditLogFilter represents filters for querying audit logs
type AuditLogFilter struct {
	UserID       *string    `json:"user_id,omitempty"`
	Actions      []string   `json:"actions,omitempty"`
	ResourceType *string    `json:"resource_type,omitempty"`
	ResourceID   *string    `json:"resource_id,omitempty"`
	DateFrom     *time.Time `json:"date_from,omitempty"`
	DateTo       *time.Time `json:"date_to,omitempty"`
	IPAddress    *string    `json:"ip_address,omitempty"`
	Page         int        `json:"page"`
	Limit        int        `json:"limit"`
}

// AuditLogResponse represents a paginated response of audit logs
type AuditLogResponse struct {
	Logs    []AuditLog `json:"logs"`
	Total   int        `json:"total"`
	Page    int        `json:"page"`
	Limit   int        `json:"limit"`
	HasMore bool       `json:"has_more"`
}
