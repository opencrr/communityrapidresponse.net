package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractCodeRoutes(t *testing.T) {
	src := `
func (r *Router) Setup() http.Handler {
	r.mux.HandleFunc("/api/v1/auth/register", r.methodHandler(http.MethodPost, r.auth.Register))
	r.mux.HandleFunc("/api/v1/auth/login", r.methodHandler(http.MethodPost, r.auth.Login))
	r.mux.HandleFunc("/api/v1/communities", r.handleRegions)
	r.mux.HandleFunc("/api/v1/communities/", r.handleRegionByID)
}

func (r *Router) handleRegionByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/communities/")
	if len(parts) >= 2 && parts[1] == "membership-requests" {
		return
	}
	if len(parts) >= 2 && parts[1] == "invitations" {
		return
	}
}
`
	routes := extractCodeRoutes(src)
	routeSet := toSet(routes)

	expected := []string{
		"/api/v1/auth/register",
		"/api/v1/auth/login",
		"/api/v1/communities",
		"/api/v1/communities/:id",
		"/api/v1/communities/:id/membership-requests",
		"/api/v1/communities/:id/invitations",
	}

	for _, e := range expected {
		if !routeSet[e] {
			t.Errorf("expected route %q not found in extracted routes: %v", e, routes)
		}
	}

	// Should NOT include trailing-slash routes as literal endpoints
	if routeSet["/api/v1/communities/"] {
		t.Error("trailing-slash route should not be included as a literal endpoint")
	}
}

func TestExtractCodeRoutes_HasSuffix(t *testing.T) {
	src := `
func (r *Router) handleAdminUserByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/v1/admin/users/")
	if strings.HasSuffix(path, "/grant-vouch") {
		return
	}
	if strings.HasSuffix(path, "/revoke-vouch") {
		return
	}
}
`
	routes := extractCodeRoutes(src)
	routeSet := toSet(routes)

	if !routeSet["/api/v1/admin/users/:id/grant-vouch"] {
		t.Errorf("expected /api/v1/admin/users/:id/grant-vouch, got: %v", routes)
	}
	if !routeSet["/api/v1/admin/users/:id/revoke-vouch"] {
		t.Errorf("expected /api/v1/admin/users/:id/revoke-vouch, got: %v", routes)
	}
}

func TestExtractDocRoutes(t *testing.T) {
	doc := `
## API Endpoints
- GET /api/v1/auth/register - register user
- POST /api/v1/auth/login - login
- GET /api/v1/regions/:id - get region
- DELETE /api/v1/regions/:id - delete region
`
	routes := extractDocRoutes(doc)
	routeSet := toSet(routes)

	if !routeSet["/api/v1/auth/register"] {
		t.Errorf("expected /api/v1/auth/register, got: %v", routes)
	}
	if !routeSet["/api/v1/regions/:id"] {
		t.Errorf("expected /api/v1/regions/:id, got: %v", routes)
	}
}

func TestExtractMigrationTables(t *testing.T) {
	dir := t.TempDir()

	// Migration 1: create tables
	writeFile(t, filepath.Join(dir, "001_init.up.sql"), `
CREATE TABLE IF NOT EXISTS users (
    id CHAR(36) PRIMARY KEY
);
CREATE TABLE IF NOT EXISTS old_table (
    id CHAR(36) PRIMARY KEY
);
`)

	// Migration 2: rename old_table
	writeFile(t, filepath.Join(dir, "002_rename.up.sql"), `
RENAME TABLE old_table TO new_table;
`)

	// Migration 3: drop something
	writeFile(t, filepath.Join(dir, "003_drop.up.sql"), `
DROP TABLE IF EXISTS new_table;
`)

	tables, err := extractMigrationTables(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tableSet := toSet(tables)

	if !tableSet["users"] {
		t.Error("expected 'users' table")
	}
	if tableSet["old_table"] {
		t.Error("'old_table' should have been renamed away")
	}
	if tableSet["new_table"] {
		t.Error("'new_table' should have been dropped")
	}
	if tableSet["schema_migrations"] {
		t.Error("schema_migrations should be excluded")
	}
}

func TestExtractDocTables(t *testing.T) {
	doc := "Key tables: `users`, `signal_groups`, `geographic_regions`"
	tables := extractDocTables(doc)
	tableSet := toSet(tables)

	for _, expected := range []string{"users", "signal_groups", "geographic_regions"} {
		if !tableSet[expected] {
			t.Errorf("expected table %q not found in: %v", expected, tables)
		}
	}
}

func TestHasParameterizedMatch(t *testing.T) {
	routeSet := map[string]bool{
		"/api/v1/regions/:id": true,
	}

	if !hasParameterizedMatch("/api/v1/regions/:id", routeSet) {
		t.Error("exact match should return true")
	}

	// Different param name shouldn't matter since both use :param
	otherSet := map[string]bool{
		"/api/v1/schools/:school_id": true,
	}
	if !hasParameterizedMatch("/api/v1/schools/:id", otherSet) {
		t.Error("different :param names should still match")
	}

	emptySet := map[string]bool{}
	if hasParameterizedMatch("/api/v1/foo", emptySet) {
		t.Error("empty set should return false")
	}
}

func TestIsKnownStaleTable(t *testing.T) {
	if !isKnownStaleTable("blacklist_proposals") {
		t.Error("blacklist_proposals should be stale")
	}
	if isKnownStaleTable("users") {
		t.Error("users should not be stale")
	}
}

func TestIsLikelyTableName(t *testing.T) {
	if isLikelyTableName("idx_user_region") {
		t.Error("index names should not be table names")
	}
	if isLikelyTableName("varchar") {
		t.Error("SQL keywords should not be table names")
	}
	if !isLikelyTableName("users") {
		t.Error("users should be a table name")
	}
	if !isLikelyTableName("signal_groups") {
		t.Error("signal_groups should be a table name")
	}
}

func TestSplitFuncBlocks(t *testing.T) {
	src := `package main

func foo() {
    // block 1
}

func bar() {
    // block 2
}
`
	blocks := splitFuncBlocks(src)
	if len(blocks) != 2 {
		t.Errorf("expected 2 func blocks, got %d", len(blocks))
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}
