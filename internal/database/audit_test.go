package database

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// ctxWithRequest builds a context populated by middleware.RequestContext with
// the given client IP and user agent. RequestContext is the only public way to
// set the ip / user-agent context keys, which are private to the middleware
// package.
func ctxWithRequest(t *testing.T, ip, userAgent string) context.Context {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip + ":1234"
	req.Header.Set("User-Agent", userAgent)

	var captured context.Context
	handler := middleware.RequestContext(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		captured = r.Context()
	}))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if captured == nil {
		t.Fatal("RequestContext middleware did not run handler")
	}
	return captured
}

// uniqueResourceID returns a UUID-shaped value that scopes a test's inserts so
// we can clean them up without trampling other tests' rows.
func uniqueResourceID() string {
	return uuid.New().String()
}

// cleanupAuditByResource removes rows scoped to a single test's resource_id.
func cleanupAuditByResource(t *testing.T, db *DB, resourceID string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		"DELETE FROM audit_log WHERE resource_id = ?", resourceID); err != nil {
		t.Logf("cleanup failed for resource_id=%s: %v", resourceID, err)
	}
}

// createAuditTestUser inserts a real users row whose id can be used as a FK
// target for audit_log.user_id. The user is removed in t.Cleanup. The email
// and username are scoped with a UUID suffix to avoid collisions between
// concurrently running tests.
func createAuditTestUser(t *testing.T, db *DB, label string) *models.User {
	t.Helper()
	repo := NewUserRepository(db)
	suffix := uuid.New().String()
	user := &models.User{
		Username:     "audit_" + label + "_" + suffix[:8],
		Email:        "audit_" + label + "_" + suffix[:8] + "@example.test",
		PasswordHash: "hash",
	}
	if err := repo.Create(context.Background(), user); err != nil {
		t.Fatalf("failed to create test user (%s): %v", label, err)
	}
	t.Cleanup(func() {
		// audit_log rows that reference this user are cleaned by
		// resource-scoped DELETE in each test; the FK is ON DELETE SET NULL
		// so deleting the user is safe even if any stragglers remain.
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", user.ID)
	})
	return user
}

func TestNewAuditRepository(t *testing.T) {
	db := testDB(t)
	repo := NewAuditRepository(db)
	if repo == nil {
		t.Fatal("NewAuditRepository returned nil")
	}
	if repo.db != db {
		t.Error("repository did not retain the provided DB handle")
	}
}

func TestAuditRepository_Log_Success(t *testing.T) {
	db := testDB(t)
	repo := NewAuditRepository(db)
	ctx := ctxWithRequest(t, "10.0.0.5", "test-agent/1.0")

	user := createAuditTestUser(t, db, "logsuccess")
	resourceID := uniqueResourceID()
	t.Cleanup(func() { cleanupAuditByResource(t, db, resourceID) })

	details := map[string]any{"foo": "bar", "n": 7}
	resourceType := "test_resource"

	if err := repo.Log(ctx, &user.ID, "test_action", &resourceType, &resourceID, details); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}

	var (
		gotUserID, gotAction, gotResourceType, gotResourceID string
		gotDetails                                           []byte
		gotIP, gotUA                                         *string
	)
	row := db.QueryRowContext(context.Background(), `
		SELECT user_id, action, resource_type, resource_id, details, ip_address, user_agent
		FROM audit_log WHERE resource_id = ?`, resourceID)
	if err := row.Scan(&gotUserID, &gotAction, &gotResourceType, &gotResourceID, &gotDetails, &gotIP, &gotUA); err != nil {
		t.Fatalf("failed to read back row: %v", err)
	}
	if gotUserID != user.ID {
		t.Errorf("user_id = %q, want %q", gotUserID, user.ID)
	}
	if gotAction != "test_action" {
		t.Errorf("action = %q, want %q", gotAction, "test_action")
	}
	if gotResourceType != resourceType {
		t.Errorf("resource_type = %q, want %q", gotResourceType, resourceType)
	}
	if gotIP == nil || *gotIP != "10.0.0.5" {
		t.Errorf("ip_address = %v, want 10.0.0.5", gotIP)
	}
	if gotUA == nil || *gotUA != "test-agent/1.0" {
		t.Errorf("user_agent = %v, want test-agent/1.0", gotUA)
	}

	var decoded map[string]any
	if err := json.Unmarshal(gotDetails, &decoded); err != nil {
		t.Fatalf("details JSON did not round-trip: %v (raw=%s)", err, string(gotDetails))
	}
	if decoded["foo"] != "bar" {
		t.Errorf("details.foo = %v, want bar", decoded["foo"])
	}
}

func TestAuditRepository_Log_NilDetails(t *testing.T) {
	db := testDB(t)
	repo := NewAuditRepository(db)
	ctx := context.Background()

	resourceID := uniqueResourceID()
	t.Cleanup(func() { cleanupAuditByResource(t, db, resourceID) })

	resourceType := "nil_details_resource"
	if err := repo.Log(ctx, nil, "nil_details_action", &resourceType, &resourceID, nil); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}

	var detailsIsNull bool
	if err := db.QueryRowContext(context.Background(),
		"SELECT details IS NULL FROM audit_log WHERE resource_id = ?", resourceID).Scan(&detailsIsNull); err != nil {
		t.Fatalf("failed to read details column: %v", err)
	}
	if !detailsIsNull {
		t.Error("expected details column to be NULL when details argument is nil")
	}
}

func TestAuditRepository_Log_TruncatesLongUserAgent(t *testing.T) {
	db := testDB(t)
	repo := NewAuditRepository(db)
	longUA := strings.Repeat("A", 1024)
	ctx := ctxWithRequest(t, "10.0.0.6", longUA)

	resourceID := uniqueResourceID()
	t.Cleanup(func() { cleanupAuditByResource(t, db, resourceID) })

	resourceType := "long_ua_resource"
	if err := repo.Log(ctx, nil, "long_ua_action", &resourceType, &resourceID, nil); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}

	var storedUA string
	if err := db.QueryRowContext(context.Background(),
		"SELECT user_agent FROM audit_log WHERE resource_id = ?", resourceID).Scan(&storedUA); err != nil {
		t.Fatalf("failed to read user_agent: %v", err)
	}
	if len(storedUA) != 512 {
		t.Fatalf("user_agent length = %d, want 512", len(storedUA))
	}
	// Ensure truncation preserved the original bytes rather than silently
	// filling with zeroes or some other placeholder.
	if want := strings.Repeat("A", 512); storedUA != want {
		t.Errorf("user_agent content mismatch: got %q (first 32 bytes), want 512 'A's",
			storedUA[:32])
	}
}

func TestAuditRepository_Log_EmptyContextOmitsIPAndUA(t *testing.T) {
	db := testDB(t)
	repo := NewAuditRepository(db)

	resourceID := uniqueResourceID()
	t.Cleanup(func() { cleanupAuditByResource(t, db, resourceID) })

	resourceType := "empty_ctx"
	if err := repo.Log(context.Background(), nil, "empty_ctx_action", &resourceType, &resourceID, nil); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}

	var (
		ipNull bool
		uaNull bool
	)
	if err := db.QueryRowContext(context.Background(),
		"SELECT ip_address IS NULL, user_agent IS NULL FROM audit_log WHERE resource_id = ?",
		resourceID).Scan(&ipNull, &uaNull); err != nil {
		t.Fatalf("failed to read row: %v", err)
	}
	if !ipNull {
		t.Error("expected ip_address to be NULL when context carries no IP")
	}
	if !uaNull {
		t.Error("expected user_agent to be NULL when context carries no UA")
	}
}

// auditTestSeed inserts rows scoped to a unique resource_id; the test should
// use the returned resource_id to filter Query() calls and trust cleanup to
// hit only this test's rows.
type auditTestSeed struct {
	resourceID   string
	resourceType string
	userIDs      []string
	actions      []string
	ips          []string
}

func seedAuditRows(t *testing.T, db *DB, repo *AuditRepository, count int) auditTestSeed {
	t.Helper()
	seed := auditTestSeed{
		resourceID:   uniqueResourceID(),
		resourceType: "seeded_resource",
	}
	for i := 0; i < count; i++ {
		ip := "10.1.1." + strconv.Itoa(i+1)
		ua := "agent-" + strconv.Itoa(i)
		// Each row references a real user so the FK constraint
		// (audit_log.user_id -> users.id) is satisfied.
		user := createAuditTestUser(t, db, "seed"+strconv.Itoa(i))
		action := "action_" + strconv.Itoa(i)
		ctx := ctxWithRequest(t, ip, ua)
		if err := repo.Log(ctx, &user.ID, action, &seed.resourceType, &seed.resourceID, map[string]int{"i": i}); err != nil {
			t.Fatalf("seed Log #%d failed: %v", i, err)
		}
		seed.userIDs = append(seed.userIDs, user.ID)
		seed.actions = append(seed.actions, action)
		seed.ips = append(seed.ips, ip)
	}
	return seed
}

func TestAuditRepository_Query_Filtering(t *testing.T) {
	db := testDB(t)
	repo := NewAuditRepository(db)
	ctx := context.Background()

	seed := seedAuditRows(t, db, repo, 4)
	t.Cleanup(func() { cleanupAuditByResource(t, db, seed.resourceID) })

	t.Run("filters by resource id", func(t *testing.T) {
		resp, err := repo.Query(ctx, models.AuditLogFilter{
			ResourceID: &seed.resourceID,
		})
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if resp.Total != 4 {
			t.Errorf("Total = %d, want 4", resp.Total)
		}
		if len(resp.Logs) != 4 {
			t.Errorf("len(Logs) = %d, want 4", len(resp.Logs))
		}
	})

	t.Run("filters by user id and resource id", func(t *testing.T) {
		resp, err := repo.Query(ctx, models.AuditLogFilter{
			UserID:     &seed.userIDs[1],
			ResourceID: &seed.resourceID,
		})
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if resp.Total != 1 {
			t.Fatalf("Total = %d, want 1", resp.Total)
		}
		if resp.Logs[0].UserID == nil || *resp.Logs[0].UserID != seed.userIDs[1] {
			t.Errorf("Logs[0].UserID = %v, want %q", resp.Logs[0].UserID, seed.userIDs[1])
		}
	})

	t.Run("filters by actions list", func(t *testing.T) {
		resp, err := repo.Query(ctx, models.AuditLogFilter{
			Actions:    []string{seed.actions[0], seed.actions[2]},
			ResourceID: &seed.resourceID,
		})
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if resp.Total != 2 {
			t.Errorf("Total = %d, want 2", resp.Total)
		}
	})

	t.Run("filters by resource_type", func(t *testing.T) {
		resp, err := repo.Query(ctx, models.AuditLogFilter{
			ResourceType: &seed.resourceType,
			ResourceID:   &seed.resourceID,
		})
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if resp.Total != 4 {
			t.Errorf("Total = %d, want 4", resp.Total)
		}
	})

	t.Run("filters by ip_address", func(t *testing.T) {
		resp, err := repo.Query(ctx, models.AuditLogFilter{
			IPAddress:  &seed.ips[0],
			ResourceID: &seed.resourceID,
		})
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if resp.Total != 1 {
			t.Errorf("Total = %d, want 1", resp.Total)
		}
	})

	t.Run("filters by date_from in the future returns no rows", func(t *testing.T) {
		future := time.Now().UTC().Add(48 * time.Hour)
		resp, err := repo.Query(ctx, models.AuditLogFilter{
			ResourceID: &seed.resourceID,
			DateFrom:   &future,
		})
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if resp.Total != 0 {
			t.Errorf("Total = %d, want 0", resp.Total)
		}
		if resp.Logs == nil {
			t.Error("Logs should be an empty slice, not nil")
		}
	})

	t.Run("filters by date_to in the past returns no rows", func(t *testing.T) {
		past := time.Now().UTC().Add(-48 * time.Hour)
		resp, err := repo.Query(ctx, models.AuditLogFilter{
			ResourceID: &seed.resourceID,
			DateTo:     &past,
		})
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if resp.Total != 0 {
			t.Errorf("Total = %d, want 0", resp.Total)
		}
	})
}

func TestAuditRepository_Query_PaginationDefaults(t *testing.T) {
	db := testDB(t)
	repo := NewAuditRepository(db)
	ctx := context.Background()

	seed := seedAuditRows(t, db, repo, 3)
	t.Cleanup(func() { cleanupAuditByResource(t, db, seed.resourceID) })

	t.Run("Page<1 defaults to 1", func(t *testing.T) {
		resp, err := repo.Query(ctx, models.AuditLogFilter{
			ResourceID: &seed.resourceID,
			Page:       0,
			Limit:      10,
		})
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if resp.Page != 1 {
			t.Errorf("Page = %d, want 1", resp.Page)
		}
	})

	t.Run("Limit<1 defaults to 50", func(t *testing.T) {
		resp, err := repo.Query(ctx, models.AuditLogFilter{
			ResourceID: &seed.resourceID,
			Limit:      0,
		})
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if resp.Limit != 50 {
			t.Errorf("Limit = %d, want 50", resp.Limit)
		}
	})

	t.Run("Limit>100 clamps to 100", func(t *testing.T) {
		resp, err := repo.Query(ctx, models.AuditLogFilter{
			ResourceID: &seed.resourceID,
			Limit:      500,
		})
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if resp.Limit != 100 {
			t.Errorf("Limit = %d, want 100", resp.Limit)
		}
	})

	t.Run("HasMore true when there are more pages", func(t *testing.T) {
		resp, err := repo.Query(ctx, models.AuditLogFilter{
			ResourceID: &seed.resourceID,
			Page:       1,
			Limit:      2,
		})
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if !resp.HasMore {
			t.Errorf("HasMore = false; want true (total=%d, page=%d, limit=%d)", resp.Total, resp.Page, resp.Limit)
		}
		if len(resp.Logs) != 2 {
			t.Errorf("len(Logs) on page 1 = %d, want 2", len(resp.Logs))
		}
	})

	t.Run("HasMore false on last page", func(t *testing.T) {
		resp, err := repo.Query(ctx, models.AuditLogFilter{
			ResourceID: &seed.resourceID,
			Page:       2,
			Limit:      2,
		})
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if resp.HasMore {
			t.Errorf("HasMore = true; want false (total=%d, page=%d, limit=%d)", resp.Total, resp.Page, resp.Limit)
		}
	})
}

func TestAuditRepository_Query_EmptyResultIsNonNilSlice(t *testing.T) {
	db := testDB(t)
	repo := NewAuditRepository(db)
	ctx := context.Background()

	unused := uniqueResourceID()
	resp, err := repo.Query(ctx, models.AuditLogFilter{ResourceID: &unused})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("Total = %d, want 0", resp.Total)
	}
	if resp.Logs == nil {
		t.Error("expected Logs to be an empty slice, got nil")
	}
}

func TestAuditRepository_DeleteOlderThan(t *testing.T) {
	db := testDB(t)
	repo := NewAuditRepository(db)
	ctx := context.Background()

	resourceID := uniqueResourceID()
	resourceType := "retention_test"
	t.Cleanup(func() { cleanupAuditByResource(t, db, resourceID) })

	// Seed one "old" row by writing directly with a backdated created_at, and
	// one "new" row through the repo so it gets a current timestamp. Both rows
	// reference a real user so the FK constraint is satisfied.
	oldUser := createAuditTestUser(t, db, "retentionold")
	newUser := createAuditTestUser(t, db, "retentionnew")

	oldID := uuid.New().String()
	oldTime := time.Now().UTC().Add(-200 * 24 * time.Hour) // 200 days ago
	if _, err := db.ExecContext(ctx, `
		INSERT INTO audit_log (id, user_id, action, resource_type, resource_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		oldID, oldUser.ID, "old_action", resourceType, resourceID, oldTime,
	); err != nil {
		t.Fatalf("failed to seed old row: %v", err)
	}

	if err := repo.Log(ctx, &newUser.ID, "new_action", &resourceType, &resourceID, nil); err != nil {
		t.Fatalf("failed to seed new row via repo: %v", err)
	}

	deleted, err := repo.DeleteOlderThan(ctx, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("DeleteOlderThan returned error: %v", err)
	}
	if deleted < 1 {
		t.Errorf("expected at least 1 row deleted, got %d", deleted)
	}

	// The "new" row must still be present; the "old" row must be gone.
	var remaining int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_log WHERE resource_id = ?", resourceID).Scan(&remaining); err != nil {
		t.Fatalf("failed to count remaining rows: %v", err)
	}
	if remaining != 1 {
		t.Errorf("remaining rows = %d, want 1", remaining)
	}

	var oldStillThere int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_log WHERE id = ?", oldID).Scan(&oldStillThere); err != nil {
		t.Fatalf("failed to check for old row: %v", err)
	}
	if oldStillThere != 0 {
		t.Error("old row should have been deleted")
	}
}

func TestAuditRepository_DeleteOlderThan_Batching(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping batch deletion test in short mode")
	}
	db := testDB(t)
	repo := NewAuditRepository(db)
	ctx := context.Background()

	resourceID := uniqueResourceID()
	resourceType := "retention_batch"
	t.Cleanup(func() { cleanupAuditByResource(t, db, resourceID) })

	// Seed >1000 old rows in a single multi-value insert to exercise the
	// batched delete loop (batchSize = 1000). user_id is NULL so we don't
	// need to create 1050 real users — the FK accepts NULL.
	const total = 1050
	cutoff := time.Now().UTC().Add(-200 * 24 * time.Hour)

	var sb strings.Builder
	sb.WriteString("INSERT INTO audit_log (id, action, resource_type, resource_id, created_at) VALUES ")
	args := make([]any, 0, total*5)
	for i := 0; i < total; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("(?, ?, ?, ?, ?)")
		args = append(args, uuid.New().String(), "batch_action", resourceType, resourceID, cutoff)
	}
	if _, err := db.ExecContext(ctx, sb.String(), args...); err != nil {
		t.Fatalf("failed to bulk-insert seed rows: %v", err)
	}

	deleted, err := repo.DeleteOlderThan(ctx, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("DeleteOlderThan returned error: %v", err)
	}
	if deleted < total {
		t.Errorf("deleted = %d, want at least %d (all old rows)", deleted, total)
	}

	var remaining int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_log WHERE resource_id = ?", resourceID).Scan(&remaining); err != nil {
		t.Fatalf("failed to count remaining rows: %v", err)
	}
	if remaining != 0 {
		t.Errorf("remaining rows = %d, want 0", remaining)
	}
}
