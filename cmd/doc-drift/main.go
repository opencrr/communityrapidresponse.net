// Command doc-drift detects documentation that has drifted out of sync with code.
// It compares API routes registered in router.go against routes documented in
// CLAUDE.md, DESIGN.md, and README.md, and scans for stale terminology in API
// route references.
package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// TermMapping defines a stale API path segment and its current replacement.
type TermMapping struct {
	Stale   string
	Current string
}

// DriftReport holds all detected drift issues.
type DriftReport struct {
	UndocumentedRoutes []string
	StaleDocRoutes     []string
	TerminologyDrift   []TerminologyIssue
}

// TerminologyIssue records a stale term found in a specific file.
type TerminologyIssue struct {
	File    string
	Term    string
	Current string
	Line    int
	Context string
}

// staleTerms are API path segments that have been renamed in code but may
// still appear in documentation route references.
var staleTerms = []TermMapping{
	{Stale: "blacklist", Current: "blocklist"},
	{Stale: "regions", Current: "communities"},
	{Stale: "invite-link-proposals", Current: "secret-proposals"},
}

// routerPattern matches HandleFunc route registrations in router.go.
var routerPattern = regexp.MustCompile(`HandleFunc\("(/api/v1/[^"]+)"`)

// docRoutePattern matches /api/v1/ paths in documentation.
// Captures letters, digits, hyphens, underscores, colons, slashes.
// Stops before *, whitespace, backtick, quote, paren, or end of string.
var docRoutePattern = regexp.MustCompile(`(/api/v1/[:a-zA-Z0-9/_-]+)`)

func main() {
	rootDir := "."
	warnOnly := false

	for _, arg := range os.Args[1:] {
		if arg == "--warn-only" {
			warnOnly = true
		} else {
			rootDir = arg
		}
	}

	report, err := DetectDrift(rootDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}

	PrintReport(report)

	hasDrift := len(report.UndocumentedRoutes) > 0 || len(report.StaleDocRoutes) > 0 || len(report.TerminologyDrift) > 0
	if hasDrift && !warnOnly {
		os.Exit(1)
	}
}

// DetectDrift runs the full drift detection against the given root directory.
func DetectDrift(rootDir string) (*DriftReport, error) {
	routerPath := rootDir + "/internal/handlers/router.go"
	routerContent, err := os.ReadFile(routerPath)
	if err != nil {
		return nil, fmt.Errorf("reading router.go: %w", err)
	}

	codeRoutes := ExtractCodeRoutes(string(routerContent))

	docFiles := []string{"CLAUDE.md", "DESIGN.md", "README.md"}
	allDocRoutes := make(map[string]bool)
	var terminologyIssues []TerminologyIssue

	for _, f := range docFiles {
		path := rootDir + "/" + f
		content, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", f, err)
		}

		docRoutes := ExtractDocRoutes(string(content))
		for _, r := range docRoutes {
			allDocRoutes[r] = true
		}

		issues := ScanTerminology(f, string(content))
		terminologyIssues = append(terminologyIssues, issues...)
	}

	undocumented, stale := CompareRoutes(codeRoutes, allDocRoutes)

	return &DriftReport{
		UndocumentedRoutes: undocumented,
		StaleDocRoutes:     stale,
		TerminologyDrift:   terminologyIssues,
	}, nil
}

// ExtractCodeRoutes pulls API route paths from router.go content.
func ExtractCodeRoutes(content string) []string {
	matches := routerPattern.FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool)
	var routes []string
	for _, m := range matches {
		route := m[1]
		if route == "/api/v1/health" {
			continue
		}
		if !seen[route] {
			seen[route] = true
			routes = append(routes, route)
		}
	}
	sort.Strings(routes)
	return routes
}

// ExtractDocRoutes pulls API route paths from documentation content.
// Filters out wildcard patterns like /api/v1/auth/*.
func ExtractDocRoutes(content string) []string {
	matches := docRoutePattern.FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool)
	var routes []string
	for _, m := range matches {
		route := strings.TrimRight(m[1], "/")
		if strings.HasSuffix(route, "/*") || strings.HasSuffix(route, "/**") {
			continue
		}
		if !seen[route] {
			seen[route] = true
			routes = append(routes, route)
		}
	}
	sort.Strings(routes)
	return routes
}

// NormalizeRoute converts a route to a canonical form for comparison.
// Trailing slashes are removed and :param/{param} segments become *.
func NormalizeRoute(route string) string {
	route = strings.TrimRight(route, "/")
	parts := strings.Split(route, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") || strings.HasPrefix(p, "{") {
			parts[i] = "*"
		}
	}
	return strings.Join(parts, "/")
}

// CompareRoutes finds routes in code but not docs, and routes in docs but not code.
//
// Matching rules:
//   - Exact normalized match
//   - A code prefix route ending in / covers any doc route with the same base
//   - A doc route with :id params covers the code prefix route
func CompareRoutes(codeRoutes []string, docRoutes map[string]bool) (undocumented, stale []string) {
	normalizedCodeSet := make(map[string]bool)
	codePrefixBases := make(map[string]bool)
	for _, r := range codeRoutes {
		normalizedCodeSet[NormalizeRoute(r)] = true
		if strings.HasSuffix(r, "/") {
			codePrefixBases[strings.TrimRight(r, "/")] = true
		}
	}

	normalizedDocSet := make(map[string]bool)
	docBasePrefixes := make(map[string]bool)
	for r := range docRoutes {
		norm := NormalizeRoute(r)
		normalizedDocSet[norm] = true
		parts := strings.Split(r, "/")
		for i, p := range parts {
			if strings.HasPrefix(p, ":") || strings.HasPrefix(p, "{") {
				prefix := strings.Join(parts[:i], "/")
				docBasePrefixes[prefix] = true
				break
			}
		}
	}

	// Find undocumented code routes
	for _, r := range codeRoutes {
		norm := NormalizeRoute(r)
		if normalizedDocSet[norm] {
			continue
		}
		base := strings.TrimRight(r, "/")
		if strings.HasSuffix(r, "/") && docBasePrefixes[base] {
			continue
		}
		if normalizedDocSet[NormalizeRoute(base)] {
			continue
		}
		undocumented = append(undocumented, r)
	}

	// Find stale doc routes
	var docList []string
	for r := range docRoutes {
		docList = append(docList, r)
	}
	sort.Strings(docList)

	for _, r := range docList {
		norm := NormalizeRoute(r)
		if normalizedCodeSet[norm] {
			continue
		}
		covered := false
		for prefix := range codePrefixBases {
			if strings.HasPrefix(NormalizeRoute(r), NormalizeRoute(prefix)) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		stale = append(stale, r)
	}

	return undocumented, stale
}

// ScanTerminology checks for stale API path terminology in documentation.
// Only flags occurrences within API route paths (lines containing /api/v1/).
func ScanTerminology(filename, content string) []TerminologyIssue {
	var issues []TerminologyIssue
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		// Extract API routes from this line and check for stale path segments
		routeMatches := docRoutePattern.FindAllString(line, -1)
		for _, route := range routeMatches {
			lowerRoute := strings.ToLower(route)
			for _, term := range staleTerms {
				// Check if the stale term appears as a path segment
				if containsPathSegment(lowerRoute, term.Stale) {
					issues = append(issues, TerminologyIssue{
						File:    filename,
						Term:    term.Stale,
						Current: term.Current,
						Line:    i + 1,
						Context: truncate(strings.TrimSpace(line), 120),
					})
				}
			}
		}
	}
	return issues
}

// containsPathSegment checks if a URL path contains the given term as a
// path segment (between slashes or at start/end of path).
func containsPathSegment(path, segment string) bool {
	parts := strings.Split(path, "/")
	for _, p := range parts {
		// Check exact segment match or if the segment contains the term
		// as a component (e.g., "blacklist-proposals" contains "blacklist")
		if p == segment || strings.Contains(p, segment) {
			return true
		}
	}
	return false
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// PrintReport outputs the drift report to stdout.
func PrintReport(r *DriftReport) {
	fmt.Println("=== Doc Drift Report ===")
	fmt.Println()

	fmt.Printf("## Routes in code but missing from docs (%d)\n", len(r.UndocumentedRoutes))
	if len(r.UndocumentedRoutes) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, route := range r.UndocumentedRoutes {
			fmt.Printf("  - %s\n", route)
		}
	}
	fmt.Println()

	fmt.Printf("## Routes in docs but missing from code (%d)\n", len(r.StaleDocRoutes))
	if len(r.StaleDocRoutes) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, route := range r.StaleDocRoutes {
			fmt.Printf("  - %s\n", route)
		}
	}
	fmt.Println()

	fmt.Printf("## Terminology drift (%d)\n", len(r.TerminologyDrift))
	if len(r.TerminologyDrift) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, issue := range r.TerminologyDrift {
			fmt.Printf("  - %s:%d: '%s' should be '%s'\n", issue.File, issue.Line, issue.Term, issue.Current)
			fmt.Printf("    %s\n", issue.Context)
		}
	}
}
