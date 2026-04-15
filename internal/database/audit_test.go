package database

import (
	"context"
	"testing"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

func setupAuditTest(t *testing.T) *AuditRepository {
	t.Helper()
	db := testDB(t)
	repo := NewAuditRepository(db)
	_, _ = db.ExecContext(context.Background(), "DELETE FROM audit_log")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM audit_log")
	})
	return repo
}

func ctxWithIPAndUA(ip, ua string) context.Context {
	ctx := context.Background()
	ctx = middleware.TestContextWithIPAndUA(ctx, ip, ua)
	return ctx
}

func TestAuditRepository_Log(t *testing.T) {
	repo := setupAuditTest(t)

	t.Run("inserts entry with all fields", func(t *testing.T) {
		userID := "user-123"
		resType := "region"
		resID := "region-456"
		details := map[string]string{"key": "value"}

		ctx := ctxWithIPAndUA("10.0.0.1", "TestAgent/1.0")
		err := repo.Log(ctx, &userID, "test_action", &resType, &resID, details)
		if err != nil {
			t.Fatalf("Log() error: %v", err)
		}

		result, err := repo.Query(context.Background(), models.AuditLogFilter{
			UserID: &userID,
			Page:   1,
			Limit:  10,
		})
		if err != nil {
			t.Fatalf("Query() error: %v", err)
		}
		if len(result.Logs) < 1 {
			t.Fatal("expected at least 1 log entry")
		}

		log := result.Logs[0]
		if log.Action != "test_action" {
			t.Errorf("expected action test_action, got %s", log.Action)
		}
		if log.UserID == nil || *log.UserID != userID {
			t.Errorf("expected user ID %s, got %v", userID, log.UserID)
		}
		if log.ResourceType == nil || *log.ResourceType != resType {
			t.Errorf("expected resource type %s, got %v", resType, log.ResourceType)
		}
		if log.IPAddress == nil || *log.IPAddress != "10.0.0.1" {
			t.Errorf("expected IP 10.0.0.1, got %v", log.IPAddress)
		}
		if log.UserAgent == nil || *log.UserAgent != "TestAgent/1.0" {
			t.Errorf("expected UA TestAgent/1.0, got %v", log.UserAgent)
		}
	})

	t.Run("inserts entry with nil optional fields", func(t *testing.T) {
		err := repo.Log(context.Background(), nil, "anonymous_action", nil, nil, nil)
		if err != nil {
			t.Fatalf("Log() error: %v", err)
		}

		actions := []string{"anonymous_action"}
		result, err := repo.Query(context.Background(), models.AuditLogFilter{
			Actions: actions,
			Page:    1,
			Limit:   10,
		})
		if err != nil {
			t.Fatalf("Query() error: %v", err)
		}
		if len(result.Logs) < 1 {
			t.Fatal("expected at least 1 log entry")
		}
		if result.Logs[0].UserID != nil {
			t.Errorf("expected nil user ID, got %v", result.Logs[0].UserID)
		}
	})
}

func TestAuditRepository_Query(t *testing.T) {
	repo := setupAuditTest(t)

	userA := "user-a"
	userB := "user-b"
	resType := "signal_group"
	resID := "sg-1"

	ctx := ctxWithIPAndUA("192.168.1.1", "Browser/2.0")
	_ = repo.Log(ctx, &userA, "action_one", &resType, &resID, nil)
	_ = repo.Log(ctx, &userA, "action_two", nil, nil, nil)
	_ = repo.Log(ctx, &userB, "action_one", nil, nil, nil)

	t.Run("filter by user ID", func(t *testing.T) {
		result, err := repo.Query(context.Background(), models.AuditLogFilter{
			UserID: &userA,
			Page:   1,
			Limit:  50,
		})
		if err != nil {
			t.Fatalf("Query() error: %v", err)
		}
		if result.Total != 2 {
			t.Errorf("expected 2 logs for userA, got %d", result.Total)
		}
	})

	t.Run("filter by actions", func(t *testing.T) {
		result, err := repo.Query(context.Background(), models.AuditLogFilter{
			Actions: []string{"action_one"},
			Page:    1,
			Limit:   50,
		})
		if err != nil {
			t.Fatalf("Query() error: %v", err)
		}
		if result.Total != 2 {
			t.Errorf("expected 2 action_one entries, got %d", result.Total)
		}
	})

	t.Run("filter by resource type and ID", func(t *testing.T) {
		result, err := repo.Query(context.Background(), models.AuditLogFilter{
			ResourceType: &resType,
			ResourceID:   &resID,
			Page:         1,
			Limit:        50,
		})
		if err != nil {
			t.Fatalf("Query() error: %v", err)
		}
		if result.Total != 1 {
			t.Errorf("expected 1 entry, got %d", result.Total)
		}
	})

	t.Run("filter by IP address", func(t *testing.T) {
		ip := "192.168.1.1"
		result, err := repo.Query(context.Background(), models.AuditLogFilter{
			IPAddress: &ip,
			Page:      1,
			Limit:     50,
		})
		if err != nil {
			t.Fatalf("Query() error: %v", err)
		}
		if result.Total != 3 {
			t.Errorf("expected 3 entries with IP, got %d", result.Total)
		}
	})

	t.Run("filter by date range", func(t *testing.T) {
		past := time.Now().UTC().Add(-1 * time.Hour)
		future := time.Now().UTC().Add(1 * time.Hour)
		result, err := repo.Query(context.Background(), models.AuditLogFilter{
			DateFrom: &past,
			DateTo:   &future,
			Page:     1,
			Limit:    50,
		})
		if err != nil {
			t.Fatalf("Query() error: %v", err)
		}
		if result.Total != 3 {
			t.Errorf("expected 3 entries in date range, got %d", result.Total)
		}
	})

	t.Run("pagination defaults", func(t *testing.T) {
		result, err := repo.Query(context.Background(), models.AuditLogFilter{
			Page:  0,
			Limit: 0,
		})
		if err != nil {
			t.Fatalf("Query() error: %v", err)
		}
		if result.Page != 1 {
			t.Errorf("expected page default 1, got %d", result.Page)
		}
		if result.Limit != 50 {
			t.Errorf("expected limit default 50, got %d", result.Limit)
		}
	})

	t.Run("limit capped at 100", func(t *testing.T) {
		result, err := repo.Query(context.Background(), models.AuditLogFilter{
			Page:  1,
			Limit: 500,
		})
		if err != nil {
			t.Fatalf("Query() error: %v", err)
		}
		if result.Limit != 100 {
			t.Errorf("expected limit capped at 100, got %d", result.Limit)
		}
	})

	t.Run("empty results return empty slice not nil", func(t *testing.T) {
		noMatch := "nonexistent-user-id"
		result, err := repo.Query(context.Background(), models.AuditLogFilter{
			UserID: &noMatch,
			Page:   1,
			Limit:  10,
		})
		if err != nil {
			t.Fatalf("Query() error: %v", err)
		}
		if result.Logs == nil {
			t.Error("expected empty slice, got nil")
		}
		if len(result.Logs) != 0 {
			t.Errorf("expected 0 logs, got %d", len(result.Logs))
		}
	})
}

func TestAuditRepository_DeleteOlderThan(t *testing.T) {
	repo := setupAuditTest(t)

	_ = repo.Log(context.Background(), nil, "old_action", nil, nil, nil)

	t.Run("deletes entries older than retention", func(t *testing.T) {
		deleted, err := repo.DeleteOlderThan(context.Background(), 0)
		if err != nil {
			t.Fatalf("DeleteOlderThan() error: %v", err)
		}
		if deleted < 1 {
			t.Errorf("expected at least 1 deletion, got %d", deleted)
		}
	})

	t.Run("does not delete recent entries", func(t *testing.T) {
		_ = repo.Log(context.Background(), nil, "recent_action", nil, nil, nil)
		deleted, err := repo.DeleteOlderThan(context.Background(), 24*time.Hour)
		if err != nil {
			t.Fatalf("DeleteOlderThan() error: %v", err)
		}
		if deleted != 0 {
			t.Errorf("expected 0 deletions for recent entries, got %d", deleted)
		}
	})
}

func TestAuditRepository_StartCleanupWorker(t *testing.T) {
	repo := setupAuditTest(t)

	_ = repo.Log(context.Background(), nil, "worker_test", nil, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	repo.StartCleanupWorker(ctx, 100*time.Millisecond, 0)

	time.Sleep(250 * time.Millisecond)
	cancel()

	result, err := repo.Query(context.Background(), models.AuditLogFilter{
		Actions: []string{"worker_test"},
		Page:    1,
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if result.Total != 0 {
		t.Errorf("expected cleanup worker to delete entries, got %d remaining", result.Total)
	}
}
