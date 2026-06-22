package main

import (
	"reflect"
	"sort"
	"testing"
)

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestExtractRoutes(t *testing.T) {
	src := `
		r.mux.HandleFunc("/api/v1/auth/login", r.methodHandler(http.MethodPost, r.auth.Login))
		r.mux.HandleFunc("/api/v1/communities", r.handleRegions)
		r.mux.HandleFunc("/api/v1/communities/", r.handleRegionByID)
		// non-API route should be ignored
		r.mux.HandleFunc("/health", healthHandler)
	`
	exact, prefix := extractRoutes(src)

	wantExact := []string{"/api/v1/auth/login", "/api/v1/communities"}
	wantPrefix := []string{"/api/v1/communities/"}

	if got := keys(exact); !reflect.DeepEqual(got, wantExact) {
		t.Errorf("exact routes mismatch: got %v want %v", got, wantExact)
	}
	if got := keys(prefix); !reflect.DeepEqual(got, wantPrefix) {
		t.Errorf("prefix routes mismatch: got %v want %v", got, wantPrefix)
	}
}

func TestExtractDocumentedPaths(t *testing.T) {
	docs := map[string]string{
		"README.md": "| GET | `/api/v1/regions/:id` | get region |",
		"CLAUDE.md": "see `/api/v1/auth/login` and `/api/v1/schools/{id}/join`",
	}
	got := extractDocumentedPaths(docs)
	want := map[string]struct{}{
		"/api/v1/regions/:id":      {},
		"/api/v1/auth/login":       {},
		"/api/v1/schools/:id/join": {},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("documented paths mismatch:\n got %v\nwant %v", keys(got), keys(want))
	}
}

func TestNormalizeDocPath(t *testing.T) {
	cases := map[string]string{
		"/api/v1/foo/:id":       "/api/v1/foo/:id",
		"/api/v1/foo/{id}":      "/api/v1/foo/:id",
		"/api/v1/foo/{user_id}": "/api/v1/foo/:id",
		"/api/v1/foo/:user_id":  "/api/v1/foo/:id",
		"/api/v1/foo/":          "/api/v1/foo",
		"/api/v1/foo,":          "/api/v1/foo",
	}
	for in, want := range cases {
		if got := normalizeDocPath(in); got != want {
			t.Errorf("normalizeDocPath(%q) = %q want %q", in, got, want)
		}
	}
}

func TestDiffEndpoints_PrefixCoversSubPath(t *testing.T) {
	realExact := map[string]struct{}{"/api/v1/schools": {}}
	realPrefix := map[string]struct{}{"/api/v1/schools/": {}}
	doc := map[string]struct{}{
		"/api/v1/schools":         {},
		"/api/v1/schools/:id":     {}, // covered by prefix
		"/api/v1/schools/:id/foo": {}, // covered by prefix
	}
	r := diffEndpoints(realExact, realPrefix, doc)
	if len(r.documentedButMissing) != 0 {
		t.Errorf("expected no documented-but-missing, got %v", r.documentedButMissing)
	}
	if len(r.realButUndocumented) != 0 {
		t.Errorf("expected no real-but-undocumented, got %v", r.realButUndocumented)
	}
}

func TestDiffEndpoints_FlagsRenameDrift(t *testing.T) {
	// Simulates the regions→communities rename: docs still say "regions",
	// router actually serves "communities".
	realExact := map[string]struct{}{"/api/v1/communities": {}}
	realPrefix := map[string]struct{}{"/api/v1/communities/": {}}
	doc := map[string]struct{}{
		"/api/v1/regions":     {},
		"/api/v1/regions/:id": {},
	}
	r := diffEndpoints(realExact, realPrefix, doc)
	if got := r.documentedButMissing; !reflect.DeepEqual(got, []string{"/api/v1/regions", "/api/v1/regions/:id"}) {
		t.Errorf("documented-but-missing mismatch: got %v", got)
	}
	wantUndoc := []string{"/api/v1/communities", "/api/v1/communities/:id"}
	if got := r.realButUndocumented; !reflect.DeepEqual(got, wantUndoc) {
		t.Errorf("real-but-undocumented mismatch: got %v want %v", got, wantUndoc)
	}
}

func TestExtractRealTables_CreateDropRename(t *testing.T) {
	migrations := []string{
		"CREATE TABLE IF NOT EXISTS users (id INT);",
		"CREATE TABLE blacklist_proposals (id INT);\nCREATE TABLE blacklist_votes (id INT);",
		// later migration renames + drops
		"RENAME TABLE blacklist_proposals TO blocklist_proposals;\nDROP TABLE blacklist_votes;",
		"ALTER TABLE users RENAME TO accounts;",
	}
	real := extractRealTables(migrations)
	got := keys(real)
	want := []string{"accounts", "blocklist_proposals"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("real tables mismatch: got %v want %v", got, want)
	}
}

func TestExtractDocumentedTables(t *testing.T) {
	docs := map[string]string{
		"CLAUDE.md": "Key tables: `users`, `geographic_regions`, `blacklist_proposals`, `vouches`",
		"DESIGN.md": "see `users` column descriptions", // not under Key tables, ignored
	}
	got := extractDocumentedTables(docs)
	want := map[string]struct{}{
		"users":               {},
		"geographic_regions":  {},
		"blacklist_proposals": {},
		"vouches":             {},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("documented tables mismatch:\n got %v\nwant %v", keys(got), keys(want))
	}
}

func TestDiffTables_DetectsRenameDrift(t *testing.T) {
	// Docs still list the pre-rename names; code reflects post-rename.
	real := map[string]struct{}{
		"users":               {},
		"blocklist_proposals": {},
		"meshtastic_channels": {},
		"schema_migrations":   {}, // ignored
	}
	doc := map[string]struct{}{
		"users":               {},
		"blacklist_proposals": {},
	}
	r := diffTables(real, doc)
	if got := r.documentedButMissing; !reflect.DeepEqual(got, []string{"blacklist_proposals"}) {
		t.Errorf("documented-but-missing mismatch: got %v", got)
	}
	wantUndoc := []string{"blocklist_proposals", "meshtastic_channels"}
	if got := r.realButUndocumented; !reflect.DeepEqual(got, wantUndoc) {
		t.Errorf("real-but-undocumented mismatch: got %v want %v", got, wantUndoc)
	}
}
