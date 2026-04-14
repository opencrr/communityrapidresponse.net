// Package testutil provides shared helpers for integration and unit tests.
//
// [TestDB] opens a connection to the test database and [CleanupDB]
// truncates all tables between tests for isolation. [TestConfig]
// returns a pre-populated [config.Config] suitable for test
// environments. Additional helpers like [CreateTestUser] and
// [CreateTestRegion] reduce boilerplate when setting up test fixtures.
package testutil
