// Package main is the entry point for the Community Rapid Response API server.
//
// It loads configuration, connects to MariaDB, runs pending database
// migrations, initializes all services and middleware, and starts the
// HTTP server. Graceful shutdown is handled on SIGINT/SIGTERM.
package main
