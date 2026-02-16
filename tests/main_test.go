package tests

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

func TestMain(m *testing.M) {
	host := os.Getenv("TEST_DB_HOST")
	if host != "" {
		truncateTestDatabase(host)
	}

	os.Exit(m.Run())
}

func truncateTestDatabase(host string) {
	port := 3306
	if p := os.Getenv("TEST_DB_PORT"); p != "" {
		_, _ = fmt.Sscanf(p, "%d", &port)
	}

	user := getEnvOrDefault("TEST_DB_USER", "test")
	password := getEnvOrDefault("TEST_DB_PASSWORD", "testpassword")
	dbName := getEnvOrDefault("TEST_DB_NAME", "communityrapidresponse_test")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true", user, password, host, port, dbName)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("TestMain: failed to connect to test database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("TestMain: failed to ping test database: %v", err)
	}

	tables := []string{
		"proposal_wrapped_keys",
		"secret_update_votes",
		"secret_update_proposals",
		"encrypted_secret_keys",
		"encrypted_secrets",
		"user_encryption_keys",
		"meshtastic_channels",
		"school_vouches",
		"school_blocked_users",
		"user_schools",
		"user_reports",
		"deletion_votes",
		"deletion_proposals",
		"invite_link_update_votes",
		"invite_link_update_proposals",
		"blocklist_votes",
		"blocked_users",
		"blocked_addresses",
		"blocklist_proposals",
		"sub_region_membership_votes",
		"sub_region_membership_requests",
		"email_notifications",
		"password_reset_tokens",
		"rate_limits",
		"signal_groups",
		"vouches",
		"verification_requests",
		"user_regions",
		"admin_boundaries",
		"audit_log",
		"schools",
		"school_districts",
		"geographic_regions",
		"users",
	}

	if _, err := db.Exec("SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		log.Fatalf("TestMain: failed to disable foreign key checks: %v", err)
	}

	for _, table := range tables {
		if _, err := db.Exec("TRUNCATE TABLE " + table); err != nil {
			log.Printf("TestMain: truncate %s: %v (may not exist yet)", table, err)
		}
	}

	if _, err := db.Exec("SET FOREIGN_KEY_CHECKS = 1"); err != nil {
		log.Fatalf("TestMain: failed to re-enable foreign key checks: %v", err)
	}

	log.Println("TestMain: test database truncated")
}
