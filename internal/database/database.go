package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
)

// DB wraps the sql.DB with additional methods
type DB struct {
	*sql.DB
}

// New creates a new database connection
func New(cfg *config.DatabaseConfig) (*DB, error) {
	db, err := sql.Open("mysql", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool from config
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{db}, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.DB.Close()
}

// Transaction executes a function within a database transaction
func (db *DB) Transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("transaction error: %v, rollback error: %v", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ExecContext wraps sql.DB.ExecContext with a Sentry span.
func (db *DB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if span := appSentry.StartSpan(ctx, "db.sql.exec", truncateQuery(query)); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}
	return db.DB.ExecContext(ctx, query, args...)
}

// QueryContext wraps sql.DB.QueryContext with a Sentry span.
func (db *DB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	if span := appSentry.StartSpan(ctx, "db.sql.query", truncateQuery(query)); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}
	return db.DB.QueryContext(ctx, query, args...)
}

// QueryRowContext wraps sql.DB.QueryRowContext with a Sentry span.
func (db *DB) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	if span := appSentry.StartSpan(ctx, "db.sql.query", truncateQuery(query)); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}
	return db.DB.QueryRowContext(ctx, query, args...)
}

// truncateQuery normalizes whitespace and truncates a SQL query for use as a span description.
func truncateQuery(query string) string {
	q := strings.Join(strings.Fields(query), " ")
	if len(q) > 100 {
		return q[:100]
	}
	return q
}

// Querier interface for both *sql.DB and *sql.Tx
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}
