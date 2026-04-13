package main

import (
	"testing"
)

func TestExtractCodeRoutes(t *testing.T) {
	content := `
	r.mux.HandleFunc("/api/v1/auth/register", r.methodHandler(http.MethodPost, r.auth.Register))
	r.mux.HandleFunc("/api/v1/auth/login", r.methodHandler(http.MethodPost, r.auth.Login))
	r.mux.HandleFunc("/api/v1/communities", r.handleRegions)
	r.mux.HandleFunc("/api/v1/communities/", r.handleRegionByID)
	r.mux.HandleFunc("/api/v1/health", healthHandler)
`
	routes := ExtractCodeRoutes(content)

	expected := []string{
		"/api/v1/auth/login",
		"/api/v1/auth/register",
		"/api/v1/communities",
		"/api/v1/communities/",
	}

	if len(routes) != len(expected) {
		t.Fatalf("expected %d routes, got %d: %v", len(expected), len(routes), routes)
	}
	for i, r := range routes {
		if r != expected[i] {
			t.Errorf("route[%d] = %q, want %q", i, r, expected[i])
		}
	}
}

func TestExtractCodeRoutes_Dedup(t *testing.T) {
	content := `
	r.mux.HandleFunc("/api/v1/foo", handler1)
	r.mux.HandleFunc("/api/v1/foo", handler2)
`
	routes := ExtractCodeRoutes(content)
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d: %v", len(routes), routes)
	}
}

func TestExtractDocRoutes(t *testing.T) {
	content := `
- GET /api/v1/regions - list regions
- GET /api/v1/regions/:id - get region
- /api/v1/auth/* - authentication endpoints
- POST /api/v1/schools/:id/join
`
	routes := ExtractDocRoutes(content)

	// /api/v1/auth/* - the regex stops before *, so /api/v1/auth is extracted
	// This is expected: wildcard-suffixed routes still produce the base path

	found := make(map[string]bool)
	for _, r := range routes {
		found[r] = true
	}
	if !found["/api/v1/regions"] {
		t.Error("missing /api/v1/regions")
	}
	if !found["/api/v1/regions/:id"] {
		t.Error("missing /api/v1/regions/:id")
	}
	if !found["/api/v1/schools/:id/join"] {
		t.Error("missing /api/v1/schools/:id/join")
	}
}

func TestNormalizeRoute(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/api/v1/regions/:id", "/api/v1/regions/*"},
		{"/api/v1/regions/", "/api/v1/regions"},
		{"/api/v1/schools/{id}/join", "/api/v1/schools/*/join"},
		{"/api/v1/auth/login", "/api/v1/auth/login"},
	}
	for _, tt := range tests {
		got := NormalizeRoute(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeRoute(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCompareRoutes_ExactMatch(t *testing.T) {
	code := []string{"/api/v1/auth/login", "/api/v1/auth/register"}
	docs := map[string]bool{
		"/api/v1/auth/login":    true,
		"/api/v1/auth/register": true,
	}

	undoc, stale := CompareRoutes(code, docs)
	if len(undoc) != 0 {
		t.Errorf("expected no undocumented, got %v", undoc)
	}
	if len(stale) != 0 {
		t.Errorf("expected no stale, got %v", stale)
	}
}

func TestCompareRoutes_PrefixMatching(t *testing.T) {
	// Code has /api/v1/communities/ (prefix handler)
	// Docs have /api/v1/communities/:id and /api/v1/communities/:id/join
	code := []string{"/api/v1/communities/"}
	docs := map[string]bool{
		"/api/v1/communities/:id":      true,
		"/api/v1/communities/:id/join": true,
	}

	undoc, stale := CompareRoutes(code, docs)
	if len(undoc) != 0 {
		t.Errorf("prefix route should be covered by doc params, got undocumented: %v", undoc)
	}
	if len(stale) != 0 {
		t.Errorf("doc param routes should be covered by code prefix, got stale: %v", stale)
	}
}

func TestCompareRoutes_Undocumented(t *testing.T) {
	code := []string{"/api/v1/auth/login", "/api/v1/new-endpoint"}
	docs := map[string]bool{
		"/api/v1/auth/login": true,
	}

	undoc, _ := CompareRoutes(code, docs)
	if len(undoc) != 1 || undoc[0] != "/api/v1/new-endpoint" {
		t.Errorf("expected [/api/v1/new-endpoint], got %v", undoc)
	}
}

func TestCompareRoutes_Stale(t *testing.T) {
	code := []string{"/api/v1/auth/login"}
	docs := map[string]bool{
		"/api/v1/auth/login":  true,
		"/api/v1/old-endpoint": true,
	}

	_, stale := CompareRoutes(code, docs)
	if len(stale) != 1 || stale[0] != "/api/v1/old-endpoint" {
		t.Errorf("expected [/api/v1/old-endpoint], got %v", stale)
	}
}

func TestScanTerminology(t *testing.T) {
	content := `
Some prose about regions that should not be flagged.
- GET /api/v1/regions/:id - get region
- /api/v1/blacklist-proposals - propose blacklisting
- /api/v1/signal-groups/:id/invite-link-proposals - propose invite link
- /api/v1/blocklist-proposals - this is the current term
`
	issues := ScanTerminology("TEST.md", content)

	// Should find: regions in route, blacklist in route, invite-link-proposals in route
	// Should NOT find: "regions" in prose, "blocklist" (current term)
	if len(issues) != 3 {
		t.Errorf("expected 3 terminology issues, got %d:", len(issues))
		for _, i := range issues {
			t.Logf("  %s:%d: '%s' should be '%s' - %s", i.File, i.Line, i.Term, i.Current, i.Context)
		}
		return
	}

	// Verify the issues found
	terms := make(map[string]bool)
	for _, i := range issues {
		terms[i.Term] = true
	}
	if !terms["regions"] {
		t.Error("expected 'regions' terminology issue")
	}
	if !terms["blacklist"] {
		t.Error("expected 'blacklist' terminology issue")
	}
	if !terms["invite-link-proposals"] {
		t.Error("expected 'invite-link-proposals' terminology issue")
	}
}

func TestScanTerminology_NoFalsePositives(t *testing.T) {
	content := `
This document talks about geographic regions and community blocklists.
The blocklist-proposals endpoint handles blocking.
No API routes here.
`
	issues := ScanTerminology("TEST.md", content)
	if len(issues) != 0 {
		t.Errorf("expected no issues for prose without API routes, got %d:", len(issues))
		for _, i := range issues {
			t.Logf("  %s:%d: '%s' - %s", i.File, i.Line, i.Term, i.Context)
		}
	}
}

func TestContainsPathSegment(t *testing.T) {
	tests := []struct {
		path    string
		segment string
		want    bool
	}{
		{"/api/v1/regions/:id", "regions", true},
		{"/api/v1/communities/:id", "regions", false},
		{"/api/v1/blacklist-proposals", "blacklist", true},
		{"/api/v1/blocklist-proposals", "blacklist", false},
	}
	for _, tt := range tests {
		got := containsPathSegment(tt.path, tt.segment)
		if got != tt.want {
			t.Errorf("containsPathSegment(%q, %q) = %v, want %v", tt.path, tt.segment, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate short = %q", got)
	}
	if got := truncate("this is a very long string", 10); got != "this is..." {
		t.Errorf("truncate long = %q", got)
	}
}
