// Package config loads and validates application configuration from
// environment variables.
//
// [Load] reads the environment and returns a [Config] struct that
// contains sub-configs for every subsystem: [ServerConfig],
// [DatabaseConfig], [JWTConfig], [MFAConfig], [MapboxConfig],
// [EmailConfig], [PostgridConfig], [LobConfig], [SentryConfig],
// [RateLimitConfig], [ConsensusConfig], and others. [Validate] checks
// required fields and cross-field constraints.
//
// The [Environment] type distinguishes dev, staging, production, and
// test modes, gating behavior like mandatory MFA and TLS requirements.
package config
