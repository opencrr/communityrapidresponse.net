package models

// ProposalStatus represents the status of any consensus proposal
type ProposalStatus string

const (
	ProposalStatusPending                    ProposalStatus = "pending"
	ProposalStatusApproved                   ProposalStatus = "approved"
	ProposalStatusRejected                   ProposalStatus = "rejected"
	ProposalStatusExpired                    ProposalStatus = "expired"
	ProposalStatusApprovedPendingFinalization ProposalStatus = "approved_pending_finalization"
)
