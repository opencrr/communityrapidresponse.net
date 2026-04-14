// Package services contains business logic and external service integrations.
//
// External integrations:
//
//   - [PostgridService] / [LobService]: address validation and postcard mailing
//   - [MapboxService]: geocoding, reverse-geocoding, and administrative boundary lookups
//   - [NCESService]: fetching school and district data from the National Center for Education Statistics
//   - [SendGridEmailService] / [SMTPEmailService]: transactional email delivery
//
// Internal services:
//
//   - [MFAService]: TOTP secret generation and verification
//   - [NotificationService] / [NotificationWorker]: email notification queueing and background processing
//   - [DBRateLimiter] / [InMemoryRateLimiter]: rate limiting implementations
//   - [EmailTemplates]: HTML email template rendering
package services
