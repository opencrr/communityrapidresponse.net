// Package handlers implements the HTTP request handlers for the REST API.
//
// Each handler type groups related endpoints and is constructed with the
// repositories and services it needs. [NewRouter] wires all handlers together
// with middleware and returns the top-level [http.Handler].
//
// Handler types include [AuthHandler] (registration, login, password reset),
// [MFAHandler] (TOTP setup and verification), [RegionHandler] (geographic
// region CRUD), [SignalGroupHandler], [VerificationHandler] (postcard and
// vouch flows), [MembershipHandler] (sub-region join requests and
// invitations), [SchoolHandler], [AdminHandler] (superuser operations),
// and consensus-based proposal handlers ([DeletionProposalHandler],
// [BlocklistProposalHandler], [SecretUpdateHandler]).
package handlers
