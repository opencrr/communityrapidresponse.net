// Package main implements a documentation drift detector for the opencrr project.
// It compares API routes in code and DB tables in migrations against what's
// documented in CLAUDE.md, README.md, and DESIGN.md, reporting mismatches.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// extractCodeRoutes parses router.go and returns all registered API route patterns.
// It handles both HandleFunc("/path", ...) registrations and sub-route patterns
// found in handler functions (e.g., TrimPrefix-based path routing).
func extractCodeRoutes(routerContent string) []string {
	routes := make(map[string]bool)

	// Match HandleFunc("/api/v1/...", ...)
	handleFuncRe := regexp.MustCompile(`HandleFunc\("(/api/v1/[^"]+)"`)
	for _, m := range handleFuncRe.FindAllStringSubmatch(routerContent, -1) {
		route := m[1]
		// Trailing slash routes are prefix-match handlers, not literal endpoints
		if !strings.HasSuffix(route, "/") {
			routes[route] = true
		}
	}

	// Extract sub-routes from TrimPrefix-based handlers.
	// These define nested paths like /api/v1/communities/:id/membership-requests
	trimPrefixRe := regexp.MustCompile(`TrimPrefix\(req\.URL\.Path, "(/api/v1/[^"]+/)"`)
	subRouteRe := regexp.MustCompile(`parts\[1\] == "([^"]+)"`)

	// Split into function blocks to associate sub-routes with their parent prefix
	funcBlocks := splitFuncBlocks(routerContent)
	for _, block := range funcBlocks {
		prefixMatches := trimPrefixRe.FindStringSubmatch(block)
		if prefixMatches == nil {
			continue
		}
		prefix := prefixMatches[1] // e.g., "/api/v1/communities/"
		baseRoute := strings.TrimSuffix(prefix, "/")
		routes[baseRoute+"/:id"] = true

		for _, sm := range subRouteRe.FindAllStringSubmatch(block, -1) {
			subPath := sm[1]
			routes[baseRoute+"/:id/"+subPath] = true
		}

		// Detect HasSuffix-based sub-routes (e.g., admin users)
		suffixRe := regexp.MustCompile(`HasSuffix\(path, "/([^"]+)"\)`)
		for _, sm := range suffixRe.FindAllStringSubmatch(block, -1) {
			routes[baseRoute+"/:id/"+sm[1]] = true
		}
	}

	result := make([]string, 0, len(routes))
	for r := range routes {
		result = append(result, r)
	}
	sort.Strings(result)
	return result
}

// splitFuncBlocks splits Go source into top-level function bodies.
func splitFuncBlocks(src string) []string {
	var blocks []string
	funcRe := regexp.MustCompile(`(?m)^func `)
	indices := funcRe.FindAllStringIndex(src, -1)
	for i, idx := range indices {
		start := idx[0]
		end := len(src)
		if i+1 < len(indices) {
			end = indices[i+1][0]
		}
		blocks = append(blocks, src[start:end])
	}
	return blocks
}

// extractDocRoutes extracts /api/v1/ route patterns from documentation content.
func extractDocRoutes(content string) []string {
	routes := make(map[string]bool)
	// Match /api/v1/... patterns, allowing :param placeholders
	re := regexp.MustCompile(`/api/v1/[\w/:_-]+`)
	for _, m := range re.FindAllString(content, -1) {
		// Normalize: strip trailing punctuation that may have been captured
		route := strings.TrimRight(m, ".,;:)")
		routes[route] = true
	}
	result := make([]string, 0, len(routes))
	for r := range routes {
		result = append(result, r)
	}
	sort.Strings(result)
	return result
}

// extractMigrationTables parses all .up.sql files and returns the current set of
// DB table names, accounting for renames and drops.
func extractMigrationTables(migrationsDir string) ([]string, error) {
	tables := make(map[string]bool)

	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.up.sql"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files) // process in order

	createRe := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)`)
	renameRe := regexp.MustCompile(`(?i)RENAME\s+TABLE\s+(\w+)\s+TO\s+(\w+)`)
	dropRe := regexp.MustCompile(`(?i)DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?(\w+)`)

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		content := string(data)

		for _, m := range createRe.FindAllStringSubmatch(content, -1) {
			tables[m[1]] = true
		}
		for _, m := range renameRe.FindAllStringSubmatch(content, -1) {
			delete(tables, m[1])
			tables[m[2]] = true
		}
		for _, m := range dropRe.FindAllStringSubmatch(content, -1) {
			delete(tables, m[1])
		}
	}

	// schema_migrations is infrastructure, not application
	delete(tables, "schema_migrations")

	result := make([]string, 0, len(tables))
	for t := range tables {
		result = append(result, t)
	}
	sort.Strings(result)
	return result, nil
}

// extractDocTables extracts table names referenced in documentation.
// Looks for backtick-quoted names and "Key tables:" lists.
func extractDocTables(content string) []string {
	tables := make(map[string]bool)

	// Match backtick-quoted table names that look like DB tables (snake_case, lowercase)
	backtickRe := regexp.MustCompile("`([a-z][a-z0-9_]+)`")
	for _, m := range backtickRe.FindAllStringSubmatch(content, -1) {
		name := m[1]
		// Filter out things that aren't table names
		if isLikelyTableName(name) {
			tables[name] = true
		}
	}

	return mapKeys(tables)
}

// isLikelyTableName heuristically identifies table-like identifiers.
func isLikelyTableName(name string) bool {
	// Skip common non-table identifiers
	skipPrefixes := []string{"http_", "utf8", "idx_", "uk_", "chk_", "fk_"}
	for _, p := range skipPrefixes {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	skipExact := map[string]bool{
		"true": true, "false": true, "null": true, "select": true,
		"insert": true, "update": true, "delete": true, "create": true,
		"table": true, "index": true, "varchar": true, "timestamp": true,
		"boolean": true, "integer": true, "text": true, "char": true,
		"json": true, "uuid": true, "primary": true, "foreign": true,
		"references": true, "cascade": true, "default": true, "not": true,
		"constraint": true, "engine": true, "innodb": true, "utf8mb4_unicode_ci": true,
		"uint8": true, "int64": true, "string": true, "bool": true, "error": true,
		"go_mod": true, "go_sum": true,
	}
	return !skipExact[name]
}

func mapKeys(m map[string]bool) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}

// staleTerminology defines old→new terminology renames to detect in docs.
var staleTerminology = []struct {
	Old, New, Context string
}{
	{"/api/v1/regions", "/api/v1/communities", "routes were renamed from regions to communities"},
	{"blacklist_proposals", "blocklist_proposals", "table renamed in migration 017"},
	{"blacklist_votes", "blocklist_votes", "table renamed in migration 017"},
	{"blacklisted_users", "blocked_users", "table renamed in migration 017"},
	{"blacklisted_addresses", "blocked_addresses", "table renamed in migration 017"},
	{"invite_link_update_proposals", "secret_update_proposals", "table replaced in migration 026"},
	{"invite_link_update_votes", "secret_update_votes", "table replaced in migration 026"},
	{"/api/v1/blacklist-proposals", "/api/v1/blocklist-proposals", "endpoint uses blocklist terminology"},
	{"invite-link-proposals", "secret-proposals", "endpoint renamed to secret-proposals"},
}

type driftIssue struct {
	Category string
	Message  string
}

func main() {
	repoRoot := "."
	if len(os.Args) > 1 {
		repoRoot = os.Args[1]
	}

	var issues []driftIssue

	// Read source files
	routerPath := filepath.Join(repoRoot, "internal", "handlers", "router.go")
	routerContent, err := os.ReadFile(routerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading router.go: %v\n", err)
		os.Exit(2)
	}

	// Read documentation files
	docFiles := []string{"CLAUDE.md", "README.md", "DESIGN.md"}
	var allDocContent strings.Builder
	docsRead := 0
	for _, name := range docFiles {
		data, err := os.ReadFile(filepath.Join(repoRoot, name))
		if err != nil {
			continue
		}
		allDocContent.Write(data)
		allDocContent.WriteByte('\n')
		docsRead++
	}
	if docsRead == 0 {
		fmt.Fprintf(os.Stderr, "Error: no documentation files found (checked: %s)\n", strings.Join(docFiles, ", "))
		os.Exit(2)
	}
	docContent := allDocContent.String()

	// --- Route drift ---
	codeRoutes := extractCodeRoutes(string(routerContent))
	docRoutes := extractDocRoutes(docContent)

	codeRouteSet := toSet(codeRoutes)
	docRouteSet := toSet(docRoutes)

	// Routes in code but not in docs
	for _, r := range codeRoutes {
		if !docRouteSet[r] {
			// Check if this is a parameterized variant of a documented route
			if !hasParameterizedMatch(r, docRouteSet) {
				issues = append(issues, driftIssue{
					Category: "Undocumented Route",
					Message:  fmt.Sprintf("Route in code but not in docs: %s", r),
				})
			}
		}
	}

	// Routes in docs but not in code
	for _, r := range docRoutes {
		if !codeRouteSet[r] {
			if !hasParameterizedMatch(r, codeRouteSet) {
				issues = append(issues, driftIssue{
					Category: "Stale Documented Route",
					Message:  fmt.Sprintf("Route in docs but not in code: %s", r),
				})
			}
		}
	}

	// --- DB table drift ---
	migrationsDir := filepath.Join(repoRoot, "migrations")
	migrationTables, err := extractMigrationTables(migrationsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading migrations: %v\n", err)
		os.Exit(2)
	}

	docTables := extractDocTables(docContent)
	docTableSet := toSet(docTables)
	migrationTableSet := toSet(migrationTables)

	for _, t := range migrationTables {
		if !docTableSet[t] {
			issues = append(issues, driftIssue{
				Category: "Undocumented Table",
				Message:  fmt.Sprintf("Table in migrations but not in docs: %s", t),
			})
		}
	}

	for _, t := range docTables {
		if !migrationTableSet[t] {
			// Only flag if it looks like it was once a real table (check stale terms)
			if isKnownStaleTable(t) {
				issues = append(issues, driftIssue{
					Category: "Stale Documented Table",
					Message:  fmt.Sprintf("Table in docs but not in migrations (renamed/dropped): %s", t),
				})
			}
		}
	}

	// --- Terminology drift ---
	for _, term := range staleTerminology {
		if strings.Contains(docContent, term.Old) {
			issues = append(issues, driftIssue{
				Category: "Stale Terminology",
				Message:  fmt.Sprintf("Docs reference %q which should be %q (%s)", term.Old, term.New, term.Context),
			})
		}
	}

	// --- Report ---
	if len(issues) == 0 {
		fmt.Println("No documentation drift detected.")
		os.Exit(0)
	}

	// Group by category
	grouped := make(map[string][]string)
	var categories []string
	for _, issue := range issues {
		if _, exists := grouped[issue.Category]; !exists {
			categories = append(categories, issue.Category)
		}
		grouped[issue.Category] = append(grouped[issue.Category], issue.Message)
	}
	sort.Strings(categories)

	fmt.Printf("Documentation drift detected: %d issue(s)\n\n", len(issues))
	for _, cat := range categories {
		msgs := grouped[cat]
		fmt.Printf("## %s (%d)\n", cat, len(msgs))
		for _, msg := range msgs {
			fmt.Printf("  - %s\n", msg)
		}
		fmt.Println()
	}

	os.Exit(1)
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}

// hasParameterizedMatch checks if a route matches when :param segments are treated as wildcards.
func hasParameterizedMatch(route string, routeSet map[string]bool) bool {
	// Direct match already checked by caller
	parts := strings.Split(route, "/")
	for candidate := range routeSet {
		candidateParts := strings.Split(candidate, "/")
		if len(candidateParts) != len(parts) {
			continue
		}
		match := true
		for i := range parts {
			if parts[i] == candidateParts[i] {
				continue
			}
			// Either side can be a :param placeholder
			if strings.HasPrefix(parts[i], ":") || strings.HasPrefix(candidateParts[i], ":") {
				continue
			}
			match = false
			break
		}
		if match {
			return true
		}
	}
	return false
}

// isKnownStaleTable returns true if the name matches a table that was renamed or dropped.
func isKnownStaleTable(name string) bool {
	stale := map[string]bool{
		"blacklist_proposals":           true,
		"blacklist_votes":               true,
		"blacklisted_users":             true,
		"blacklisted_addresses":         true,
		"invite_link_update_proposals":  true,
		"invite_link_update_votes":      true,
	}
	return stale[name]
}
