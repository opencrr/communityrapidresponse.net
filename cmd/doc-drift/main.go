// Command doc-drift detects drift between prose docs and the real codebase.
//
// Two checks are performed against CLAUDE.md, DESIGN.md, and README.md:
//
//  1. API endpoints documented in markdown vs. routes actually registered
//     in internal/handlers/router.go.
//  2. Database tables documented in markdown vs. tables that exist after
//     applying migrations/*.up.sql in order (CREATE / DROP / RENAME).
//
// For each check we report:
//   - documented-but-missing: appears in docs, not in code.
//   - real-but-undocumented:  exists in code, not mentioned in docs.
//
// Exits non-zero when drift is found unless --warn-only is set, so it can
// be wired into CI as a soft or hard gate.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	exitClean   = 0
	exitDrift   = 1
	exitFailure = 2
)

type report struct {
	check                string
	documentedButMissing []string
	realButUndocumented  []string
}

func (r report) hasDrift() bool {
	return len(r.documentedButMissing) > 0 || len(r.realButUndocumented) > 0
}

func (r report) print(w *os.File) {
	// #nosec G705 -- w is os.Stdout in this internal CLI tool; terminal output is not an XSS sink.
	_, _ = fmt.Fprintf(w, "== %s ==\n", r.check)
	if !r.hasDrift() {
		_, _ = fmt.Fprintln(w, "  no drift detected")
		return
	}
	if len(r.documentedButMissing) > 0 {
		_, _ = fmt.Fprintln(w, "  documented but missing in code:")
		for _, s := range r.documentedButMissing {
			// #nosec G705 -- CLI stdout, not an XSS sink.
			_, _ = fmt.Fprintf(w, "    - %s\n", s)
		}
	}
	if len(r.realButUndocumented) > 0 {
		_, _ = fmt.Fprintln(w, "  in code but undocumented:")
		for _, s := range r.realButUndocumented {
			// #nosec G705 -- CLI stdout, not an XSS sink.
			_, _ = fmt.Fprintf(w, "    - %s\n", s)
		}
	}
}

func main() {
	repoRoot := flag.String("repo", ".", "Path to the repo root")
	warnOnly := flag.Bool("warn-only", false, "Exit 0 even when drift is found")
	flag.Parse()

	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "doc-drift: %v\n", err)
		os.Exit(exitFailure)
	}

	docs, err := loadDocs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "doc-drift: %v\n", err)
		os.Exit(exitFailure)
	}

	// #nosec G304 -- root is a CLI-controlled flag in this internal doc-drift tool.
	routerSrc, err := os.ReadFile(filepath.Join(root, "internal", "handlers", "router.go"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "doc-drift: %v\n", err)
		os.Exit(exitFailure)
	}

	migrations, err := loadMigrations(filepath.Join(root, "migrations"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "doc-drift: %v\n", err)
		os.Exit(exitFailure)
	}

	realExact, realPrefix := extractRoutes(string(routerSrc))
	docPaths := extractDocumentedPaths(docs)
	endpoints := diffEndpoints(realExact, realPrefix, docPaths)
	endpoints.print(os.Stdout)

	realTables := extractRealTables(migrations)
	docTables := extractDocumentedTables(docs)
	tables := diffTables(realTables, docTables)
	tables.print(os.Stdout)

	if (endpoints.hasDrift() || tables.hasDrift()) && !*warnOnly {
		os.Exit(exitDrift)
	}
	os.Exit(exitClean)
}

// loadDocs reads the prose doc files we scan. Missing files are skipped.
func loadDocs(root string) (map[string]string, error) {
	out := map[string]string{}
	for _, name := range []string{"CLAUDE.md", "DESIGN.md", "README.md"} {
		// #nosec G304 -- root is a CLI-controlled flag, name is a hardcoded literal.
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		out[name] = string(b)
	}
	return out, nil
}

// loadMigrations returns the up-migration SQL files sorted by filename.
func loadMigrations(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		// #nosec G304 -- m is from filepath.Glob over the CLI-supplied migrations dir in this internal tool.
		b, err := os.ReadFile(m)
		if err != nil {
			return nil, err
		}
		out = append(out, string(b))
	}
	return out, nil
}

// --- Route extraction ----------------------------------------------------

var handleFuncRe = regexp.MustCompile(`(?m)r\.mux\.HandleFunc\(\s*"([^"]+)"`)

// extractRoutes returns the exact paths and prefix paths registered with
// the router. A prefix path is one whose registration ends in a trailing
// slash — ServeMux treats those as sub-tree handlers.
func extractRoutes(src string) (exact, prefix map[string]struct{}) {
	exact = map[string]struct{}{}
	prefix = map[string]struct{}{}
	for _, m := range handleFuncRe.FindAllStringSubmatch(src, -1) {
		path := m[1]
		if !strings.HasPrefix(path, "/api/v1/") {
			continue
		}
		if strings.HasSuffix(path, "/") {
			prefix[path] = struct{}{}
		} else {
			exact[path] = struct{}{}
		}
	}
	return
}

// --- Documented endpoint extraction --------------------------------------

// pathInBackticks matches `/api/v1/...` enclosed in backticks. Trailing
// punctuation inside the backticks is intentionally not stripped here —
// we normalize below.
var pathInBackticks = regexp.MustCompile("`(/api/v1/[A-Za-z0-9_:{}/.-]+)`")

// extractDocumentedPaths returns the set of distinct documented API
// paths across all docs, normalized so :id and {id} forms match.
func extractDocumentedPaths(docs map[string]string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, body := range docs {
		for _, m := range pathInBackticks.FindAllStringSubmatch(body, -1) {
			out[normalizeDocPath(m[1])] = struct{}{}
		}
	}
	return out
}

var pathParamRe = regexp.MustCompile(`\{[^}]+\}|:[A-Za-z_][A-Za-z0-9_]*`)

// normalizeDocPath canonicalizes path parameters and trims trailing
// punctuation/slashes so docs and code can be compared.
func normalizeDocPath(p string) string {
	p = strings.TrimRight(p, ".,;:/")
	p = pathParamRe.ReplaceAllString(p, ":id")
	return p
}

// --- Endpoint diff -------------------------------------------------------

// diffEndpoints reconciles documented paths against real routes,
// accounting for ServeMux prefix routes.
func diffEndpoints(realExact, realPrefix, doc map[string]struct{}) report {
	r := report{check: "API endpoints (docs vs internal/handlers/router.go)"}

	for d := range doc {
		if _, ok := realExact[d]; ok {
			continue
		}
		if matchesPrefix(d, realPrefix) {
			continue
		}
		r.documentedButMissing = append(r.documentedButMissing, d)
	}

	for e := range realExact {
		if _, ok := doc[e]; ok {
			continue
		}
		// docs may write the prefix form with an :id
		if _, ok := doc[e+"/:id"]; ok {
			continue
		}
		r.realButUndocumented = append(r.realButUndocumented, e)
	}
	for p := range realPrefix {
		if documentedUnderPrefix(p, doc) {
			continue
		}
		r.realButUndocumented = append(r.realButUndocumented, p+":id")
	}

	sort.Strings(r.documentedButMissing)
	sort.Strings(r.realButUndocumented)
	return r
}

// matchesPrefix reports whether the documented path d falls under any
// prefix registered with the router.
func matchesPrefix(d string, prefixes map[string]struct{}) bool {
	for p := range prefixes {
		if strings.HasPrefix(d+"/", p) || strings.HasPrefix(d, p) {
			return true
		}
	}
	return false
}

// documentedUnderPrefix reports whether any documented path starts with
// the given prefix, indicating the prefix's sub-tree is covered.
func documentedUnderPrefix(prefix string, doc map[string]struct{}) bool {
	for d := range doc {
		if strings.HasPrefix(d+"/", prefix) {
			return true
		}
	}
	return false
}

// --- Table extraction ----------------------------------------------------

var (
	createTableRe = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?` + "`?" + `([A-Za-z_][A-Za-z0-9_]*)` + "`?")
	dropTableRe   = regexp.MustCompile(`(?i)DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?` + "`?" + `([A-Za-z_][A-Za-z0-9_]*)` + "`?")
	renameTableRe = regexp.MustCompile(`(?i)RENAME\s+TABLE\s+` + "`?" + `([A-Za-z_][A-Za-z0-9_]*)` + "`?" + `\s+TO\s+` + "`?" + `([A-Za-z_][A-Za-z0-9_]*)` + "`?")
	alterRenameRe = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+` + "`?" + `([A-Za-z_][A-Za-z0-9_]*)` + "`?" + `\s+RENAME\s+TO\s+` + "`?" + `([A-Za-z_][A-Za-z0-9_]*)` + "`?")
)

// extractRealTables applies CREATE/DROP/RENAME statements from each
// migration in order and returns the resulting set of table names.
func extractRealTables(migrations []string) map[string]struct{} {
	tables := map[string]struct{}{}
	for _, sql := range migrations {
		for _, m := range createTableRe.FindAllStringSubmatch(sql, -1) {
			tables[m[1]] = struct{}{}
		}
		for _, m := range renameTableRe.FindAllStringSubmatch(sql, -1) {
			delete(tables, m[1])
			tables[m[2]] = struct{}{}
		}
		for _, m := range alterRenameRe.FindAllStringSubmatch(sql, -1) {
			delete(tables, m[1])
			tables[m[2]] = struct{}{}
		}
		for _, m := range dropTableRe.FindAllStringSubmatch(sql, -1) {
			delete(tables, m[1])
		}
	}
	return tables
}

// --- Documented table extraction -----------------------------------------

// keyTablesLineRe finds the "Key tables:" enumeration that CLAUDE.md/DESIGN.md
// use to summarize the schema. The list itself is a sequence of backtick
// identifiers we extract with identifierRe below.
var keyTablesLineRe = regexp.MustCompile(`(?i)Key tables:\s*(.+)`)
var identifierRe = regexp.MustCompile("`([a-z_][a-z0-9_]*)`")

// extractDocumentedTables pulls the table names listed in any "Key tables:"
// line across the docs. We intentionally don't scan every backticked
// identifier in the prose — many of those are columns or unrelated terms
// and would produce noise. The "Key tables:" enumeration is the single
// authoritative spot CLAUDE.md uses for the table list.
func extractDocumentedTables(docs map[string]string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, body := range docs {
		for _, m := range keyTablesLineRe.FindAllStringSubmatch(body, -1) {
			for _, id := range identifierRe.FindAllStringSubmatch(m[1], -1) {
				out[id[1]] = struct{}{}
			}
		}
	}
	return out
}

// --- Table diff ----------------------------------------------------------

// ignoredTables are infra/bookkeeping tables we don't expect docs to list.
var ignoredTables = map[string]struct{}{
	"schema_migrations": {},
}

func diffTables(real, doc map[string]struct{}) report {
	r := report{check: "Database tables (docs vs migrations/*.up.sql)"}
	for d := range doc {
		if _, ok := real[d]; !ok {
			r.documentedButMissing = append(r.documentedButMissing, d)
		}
	}
	for t := range real {
		if _, ignored := ignoredTables[t]; ignored {
			continue
		}
		if _, ok := doc[t]; !ok {
			r.realButUndocumented = append(r.realButUndocumented, t)
		}
	}
	sort.Strings(r.documentedButMissing)
	sort.Strings(r.realButUndocumented)
	return r
}
