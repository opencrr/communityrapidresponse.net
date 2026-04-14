// Package models defines the domain types for the Community Rapid Response platform.
//
// It contains request and response structs for every API endpoint, persistent
// domain entities that map to database rows, and shared enumerations used
// across the codebase.
//
// Key type groups:
//
//   - Authentication: [RegisterRequest], [LoginRequest], [TokenClaims]
//   - Users: [User], [UserRegion], [PublicUser], [UserStatus]
//   - Regions: [GeographicRegion], [RegionWithDetails], [CreateRegionRequest]
//   - Verification: [VerificationRequest], [Vouch], [VouchRequest]
//   - Proposals: [DeletionProposal], [BlocklistProposal], [SecretUpdateProposal]
//   - Signal groups: [SignalGroup], [CreateSignalGroupRequest]
//   - Schools: [School], [SchoolDistrict], [SchoolVouch]
//   - Encryption: [UserEncryptionKey], [EncryptedSecret]
//   - Notifications: [EmailNotification], [AuditLog]
package models
