// Command tech-debt-classify statically scans the opencrr Go codebase for
// tech-debt signals (annotation comments, oversized files, long functions,
// skipped tests, deep nesting) and emits a deterministic Markdown report
// classified by category and priority.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type category string

const (
	catAnnotation   category = "annotation"
	catSize         category = "size"
	catComplexity   category = "complexity"
	catTestDebt     category = "test-debt"
	catSecurityNote category = "security-note"
)

type priority string

const (
	prHigh   priority = "HIGH"
	prMedium priority = "MEDIUM"
	prLow    priority = "LOW"
)

var priorityOrder = map[priority]int{prHigh: 0, prMedium: 1, prLow: 2}

type finding struct {
	File     string
	Line     int
	Pkg      string
	Category category
	Priority priority
	Tag      string
	Snippet  string
}

type config struct {
	Root         string
	OutputPath   string
	Check        bool
	MaxFileLines int
	MaxFuncLines int
	MaxNesting   int
}

func main() {
	cfg := config{
		MaxFileLines: 500,
		MaxFuncLines: 80,
		MaxNesting:   5,
	}
	flag.StringVar(&cfg.Root, "root", ".", "Repository root to scan")
	flag.StringVar(&cfg.OutputPath, "out", "docs/TECH_DEBT.md", "Output Markdown report path")
	flag.BoolVar(&cfg.Check, "check", false, "Exit non-zero if HIGH-priority findings exceed recorded baseline in existing report header")
	flag.Parse()

	findings, err := scan(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scan failed: %v\n", err)
		os.Exit(1)
	}

	sortFindings(findings)

	highCount := countByPriority(findings, prHigh)

	if cfg.Check {
		baseline, ok := readBaselineHigh(cfg.OutputPath)
		if ok && highCount > baseline {
			fmt.Fprintf(os.Stderr, "tech-debt-classify: HIGH findings increased from %d to %d\n", baseline, highCount)
			os.Exit(2)
		}
		fmt.Printf("tech-debt-classify: HIGH=%d (baseline=%d)\n", highCount, baseline)
		return
	}

	report := renderMarkdown(findings, highCount)
	if err := os.MkdirAll(filepath.Dir(cfg.OutputPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir failed: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(cfg.OutputPath, []byte(report), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("tech-debt-classify: %d findings written to %s (HIGH=%d)\n", len(findings), cfg.OutputPath, highCount)
}

var (
	annotationRe = regexp.MustCompile(`(?i)//\s*(TODO|FIXME|XXX|HACK|DEPRECATED)\b[: ]?(.*)`)
	funcDeclRe   = regexp.MustCompile(`^\s*func\b`)
	skipRe       = regexp.MustCompile(`\bt\.Skip(Now)?\s*\(`)
)

// skipDirs lists directories we never scan.
var skipDirs = map[string]bool{
	".git":         true,
	"vendor":       true,
	"node_modules": true,
	"bin":          true,
	"tmp":          true,
	"static":       true,
	"templates":    true,
	"migrations":   true,
	"docs":         true,
	"docker":       true,
	".github":      true,
}

func scan(cfg config) ([]finding, error) {
	var findings []finding
	root := cfg.Root
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip the classifier itself to avoid self-tagging on its own annotation
		// regexes; the patterns above would otherwise show up as findings.
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(rel, filepath.Join("cmd", "tech-debt-classify")+string(filepath.Separator)) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		findings = append(findings, scanFile(rel, string(data), cfg)...)
		return nil
	})
	return findings, err
}

func scanFile(relPath, content string, cfg config) []finding {
	pkg := packageOf(relPath)
	lines := strings.Split(content, "\n")
	isTestFile := strings.HasSuffix(relPath, "_test.go")
	isMockFile := strings.Contains(relPath, "mock") || strings.Contains(relPath, "fake")

	var out []finding

	// File-size signal
	if len(lines) > cfg.MaxFileLines {
		out = append(out, finding{
			File:     relPath,
			Line:     1,
			Pkg:      pkg,
			Category: catSize,
			Priority: classifySizePriority(pkg, isTestFile),
			Tag:      "oversized-file",
			Snippet:  fmt.Sprintf("%d lines (>%d)", len(lines), cfg.MaxFileLines),
		})
	}

	// Per-line signals
	for i, raw := range lines {
		ln := i + 1
		line := strings.TrimRight(raw, "\r")

		if m := annotationRe.FindStringSubmatch(line); m != nil {
			tag := strings.ToUpper(m[1])
			body := strings.TrimSpace(m[2])
			out = append(out, finding{
				File:     relPath,
				Line:     ln,
				Pkg:      pkg,
				Category: classifyAnnotationCategory(tag),
				Priority: classifyAnnotationPriority(tag, body, pkg, isTestFile, isMockFile),
				Tag:      tag,
				Snippet:  snippet(line),
			})
		}

		if isTestFile && skipRe.MatchString(line) {
			out = append(out, finding{
				File:     relPath,
				Line:     ln,
				Pkg:      pkg,
				Category: catTestDebt,
				Priority: classifySkipPriority(pkg),
				Tag:      "t.Skip",
				Snippet:  snippet(line),
			})
		}
	}

	// Function-size and nesting signals via a brace-depth scan.
	out = append(out, scanFunctions(relPath, pkg, lines, cfg, isTestFile)...)

	return out
}

func scanFunctions(relPath, pkg string, lines []string, cfg config, isTestFile bool) []finding {
	var out []finding
	depth := 0
	inFunc := false
	funcStart := 0
	funcName := ""
	maxDepthSeen := 0

	for i, raw := range lines {
		line := stripStringsAndComments(raw)

		if !inFunc && funcDeclRe.MatchString(raw) {
			// Function declaration. It may open its brace on this line or a later one.
			funcStart = i + 1
			funcName = extractFuncName(raw)
			// Count braces on this line; if a "{" appears we're in the function body.
			opens := strings.Count(line, "{")
			closes := strings.Count(line, "}")
			depth += opens - closes
			if depth > 0 {
				inFunc = true
				maxDepthSeen = depth
			}
			continue
		}

		if !inFunc {
			continue
		}

		opens := strings.Count(line, "{")
		closes := strings.Count(line, "}")
		depth += opens
		if depth > maxDepthSeen {
			maxDepthSeen = depth
		}
		depth -= closes

		if depth <= 0 {
			funcLen := (i + 1) - funcStart + 1
			if funcLen > cfg.MaxFuncLines {
				out = append(out, finding{
					File:     relPath,
					Line:     funcStart,
					Pkg:      pkg,
					Category: catSize,
					Priority: classifyFuncSizePriority(pkg, isTestFile),
					Tag:      "long-function",
					Snippet:  fmt.Sprintf("%s: %d lines (>%d)", funcName, funcLen, cfg.MaxFuncLines),
				})
			}
			if maxDepthSeen > cfg.MaxNesting {
				out = append(out, finding{
					File:     relPath,
					Line:     funcStart,
					Pkg:      pkg,
					Category: catComplexity,
					Priority: classifyComplexityPriority(pkg, isTestFile),
					Tag:      "deep-nesting",
					Snippet:  fmt.Sprintf("%s: brace depth %d (>%d)", funcName, maxDepthSeen, cfg.MaxNesting),
				})
			}
			inFunc = false
			depth = 0
			maxDepthSeen = 0
		}
	}
	return out
}

// stripStringsAndComments removes contents of "..." strings, `...` raw strings,
// and trailing // comments so brace counting is not thrown off by braces that
// appear in string literals or comments.
func stripStringsAndComments(s string) string {
	var b strings.Builder
	inStr := false
	inRaw := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !inStr && !inRaw && c == '/' && i+1 < len(s) && s[i+1] == '/' {
			break
		}
		switch {
		case inStr:
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == '"' {
				inStr = false
			}
		case inRaw:
			if c == '`' {
				inRaw = false
			}
		default:
			if c == '"' {
				inStr = true
				continue
			}
			if c == '`' {
				inRaw = true
				continue
			}
			b.WriteByte(c)
		}
	}
	return b.String()
}

func extractFuncName(line string) string {
	// Best-effort name extraction: "func (r *T) Name(" or "func Name(".
	rest := strings.TrimPrefix(strings.TrimSpace(line), "func")
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "(") {
		// Skip receiver.
		end := strings.Index(rest, ")")
		if end == -1 {
			return "anonymous"
		}
		rest = strings.TrimSpace(rest[end+1:])
	}
	paren := strings.Index(rest, "(")
	if paren == -1 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:paren])
}

func packageOf(relPath string) string {
	dir := filepath.Dir(relPath)
	if dir == "." || dir == "" {
		return "(root)"
	}
	return filepath.ToSlash(dir)
}

func snippet(line string) string {
	s := strings.TrimSpace(line)
	if len(s) > 160 {
		s = s[:157] + "..."
	}
	// Markdown-table safety: pipes break the row layout.
	s = strings.ReplaceAll(s, "|", `\|`)
	return s
}

func classifyAnnotationCategory(tag string) category {
	switch tag {
	case "HACK", "FIXME", "XXX":
		return catSecurityNote
	default:
		return catAnnotation
	}
}

func classifyAnnotationPriority(tag, body, pkg string, isTest, isMock bool) priority {
	low := isTest || isMock
	switch tag {
	case "FIXME", "HACK", "XXX":
		if low {
			return prMedium
		}
		return prHigh
	case "DEPRECATED":
		return prLow
	default: // TODO
		_ = body
		if low {
			return prLow
		}
		return prMedium
	}
}

func classifySizePriority(pkg string, isTest bool) priority {
	if isTest {
		return prLow
	}
	if isSensitivePkg(pkg) {
		return prMedium
	}
	return prMedium
}

func classifyFuncSizePriority(pkg string, isTest bool) priority {
	if isTest {
		return prLow
	}
	return prMedium
}

func classifyComplexityPriority(pkg string, isTest bool) priority {
	if isTest {
		return prLow
	}
	if isSensitivePkg(pkg) {
		return prHigh
	}
	return prMedium
}

func classifySkipPriority(pkg string) priority {
	// Skipped tests in handlers/services indicate disabled coverage in core paths.
	if strings.Contains(pkg, "handlers") || strings.Contains(pkg, "services") {
		return prHigh
	}
	return prMedium
}

func isSensitivePkg(pkg string) bool {
	for _, key := range []string{"handlers", "services", "middleware", "auth", "security"} {
		if strings.Contains(pkg, key) {
			return true
		}
	}
	return false
}

func sortFindings(fs []finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if priorityOrder[fs[i].Priority] != priorityOrder[fs[j].Priority] {
			return priorityOrder[fs[i].Priority] < priorityOrder[fs[j].Priority]
		}
		if fs[i].Pkg != fs[j].Pkg {
			return fs[i].Pkg < fs[j].Pkg
		}
		if fs[i].File != fs[j].File {
			return fs[i].File < fs[j].File
		}
		return fs[i].Line < fs[j].Line
	})
}

func countByPriority(fs []finding, p priority) int {
	n := 0
	for _, f := range fs {
		if f.Priority == p {
			n++
		}
	}
	return n
}

// renderMarkdown produces the deterministic Markdown report. The very first
// non-header line encodes the HIGH baseline in a machine-readable form so
// `--check` can detect regressions without parsing the full report.
func renderMarkdown(fs []finding, highCount int) string {
	var b strings.Builder
	b.WriteString("# Technical Debt Report\n\n")
	fmt.Fprintf(&b, "<!-- tech-debt-classify:baseline HIGH=%d -->\n\n", highCount)
	b.WriteString("Generated by `just tech-debt` (`go run ./cmd/tech-debt-classify`). ")
	b.WriteString("Do not edit by hand — re-run the tool to refresh.\n\n")
	b.WriteString("## Rubric\n\n")
	b.WriteString("Findings are classified by **category** and **priority**.\n\n")
	b.WriteString("**Categories**\n\n")
	b.WriteString("- `annotation` — `TODO` / `DEPRECATED` comments left in source.\n")
	b.WriteString("- `security-note` — `FIXME` / `HACK` / `XXX` comments, which historically flag risky behavior.\n")
	b.WriteString("- `size` — files over 500 lines or functions over 80 lines.\n")
	b.WriteString("- `complexity` — functions with brace nesting depth greater than 5.\n")
	b.WriteString("- `test-debt` — `t.Skip` calls disabling coverage in `_test.go` files.\n\n")
	b.WriteString("**Priorities**\n\n")
	b.WriteString("- **HIGH** — `FIXME`/`HACK`/`XXX` in non-test code, deep nesting in security-sensitive packages (handlers, services, middleware, auth, security), and skipped tests in handlers/services.\n")
	b.WriteString("- **MEDIUM** — `TODO` in core packages, oversized files, long functions in core packages, and `FIXME`/`HACK`/`XXX` in tests or mocks.\n")
	b.WriteString("- **LOW** — `TODO` in tests/mocks, `DEPRECATED` annotations, oversized or complex test code.\n\n")
	b.WriteString("## Summary\n\n")
	b.WriteString(renderSummaryTable(fs))
	b.WriteString("\n")
	b.WriteString("## Findings\n\n")
	if len(fs) == 0 {
		b.WriteString("_No findings._\n")
		return b.String()
	}
	renderFindingsByPriority(&b, fs)
	return b.String()
}

func renderSummaryTable(fs []finding) string {
	cats := []category{catAnnotation, catSecurityNote, catSize, catComplexity, catTestDebt}

	counts := map[category]map[priority]int{}
	for _, c := range cats {
		counts[c] = map[priority]int{}
	}
	totalsByPrio := map[priority]int{}
	totalsByCat := map[category]int{}
	for _, f := range fs {
		counts[f.Category][f.Priority]++
		totalsByPrio[f.Priority]++
		totalsByCat[f.Category]++
	}

	var b strings.Builder
	b.WriteString("| Category | HIGH | MEDIUM | LOW | Total |\n")
	b.WriteString("|---|---:|---:|---:|---:|\n")
	for _, c := range cats {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d |\n",
			c, counts[c][prHigh], counts[c][prMedium], counts[c][prLow], totalsByCat[c])
	}
	fmt.Fprintf(&b, "| **Total** | **%d** | **%d** | **%d** | **%d** |\n",
		totalsByPrio[prHigh], totalsByPrio[prMedium], totalsByPrio[prLow], len(fs))
	return b.String()
}

func renderFindingsByPriority(b *strings.Builder, fs []finding) {
	prios := []priority{prHigh, prMedium, prLow}
	byPrio := map[priority][]finding{}
	for _, f := range fs {
		byPrio[f.Priority] = append(byPrio[f.Priority], f)
	}
	for _, p := range prios {
		group := byPrio[p]
		fmt.Fprintf(b, "### %s (%d)\n\n", p, len(group))
		if len(group) == 0 {
			b.WriteString("_None._\n\n")
			continue
		}
		// Group by package.
		byPkg := map[string][]finding{}
		var pkgs []string
		for _, f := range group {
			if _, ok := byPkg[f.Pkg]; !ok {
				pkgs = append(pkgs, f.Pkg)
			}
			byPkg[f.Pkg] = append(byPkg[f.Pkg], f)
		}
		sort.Strings(pkgs)
		for _, pkg := range pkgs {
			fmt.Fprintf(b, "#### `%s`\n\n", pkg)
			b.WriteString("| Location | Category | Tag | Detail |\n")
			b.WriteString("|---|---|---|---|\n")
			for _, f := range byPkg[pkg] {
				fmt.Fprintf(b, "| `%s:%d` | %s | %s | %s |\n",
					f.File, f.Line, f.Category, f.Tag, f.Snippet)
			}
			b.WriteString("\n")
		}
	}
}

// readBaselineHigh parses the HIGH baseline marker out of an existing report,
// returning (count, true) if present.
func readBaselineHigh(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	re := regexp.MustCompile(`tech-debt-classify:baseline HIGH=(\d+)`)
	m := re.FindStringSubmatch(string(data))
	if m == nil {
		return 0, false
	}
	var n int
	fmt.Sscanf(m[1], "%d", &n)
	return n, true
}
