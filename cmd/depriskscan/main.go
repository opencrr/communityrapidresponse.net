package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type RiskLevel string

const (
	RiskCritical RiskLevel = "critical"
	RiskHigh     RiskLevel = "high"
	RiskMedium   RiskLevel = "medium"
	RiskLow      RiskLevel = "low"
)

type ModuleInfo struct {
	Path       string     `json:"Path"`
	Version    string     `json:"Version"`
	Update     *UpdateRef `json:"Update,omitempty"`
	Deprecated string     `json:"Deprecated,omitempty"`
	Indirect   bool       `json:"Indirect"`
	Main       bool       `json:"Main"`
	Time       *time.Time `json:"Time,omitempty"`
}

type UpdateRef struct {
	Path    string `json:"Path"`
	Version string `json:"Version"`
}

type Finding struct {
	Module   string    `json:"module"`
	Version  string    `json:"version"`
	Risk     RiskLevel `json:"risk"`
	Category string    `json:"category"`
	Detail   string    `json:"detail"`
}

type Report struct {
	Timestamp string    `json:"timestamp"`
	Findings  []Finding `json:"findings"`
	Summary   Summary   `json:"summary"`
}

type Summary struct {
	Total    int `json:"total"`
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
}

func main() {
	modules, err := listModules()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing modules: %v\n", err)
		os.Exit(2)
	}

	vulns := runGovulncheck()

	var findings []Finding

	for _, mod := range modules {
		if mod.Main {
			continue
		}

		if _, hasVuln := vulns[mod.Path]; hasVuln {
			risk := RiskCritical
			if mod.Indirect {
				risk = RiskLow
			}
			findings = append(findings, Finding{
				Module:   mod.Path,
				Version:  mod.Version,
				Risk:     risk,
				Category: "vulnerability",
				Detail:   fmt.Sprintf("Known vulnerability detected via govulncheck: %s", vulns[mod.Path]),
			})
		}

		if mod.Deprecated != "" {
			risk := RiskHigh
			if mod.Indirect {
				risk = RiskLow
			}
			findings = append(findings, Finding{
				Module:   mod.Path,
				Version:  mod.Version,
				Risk:     risk,
				Category: "deprecated",
				Detail:   fmt.Sprintf("Module deprecated: %s", mod.Deprecated),
			})
		}

		if mod.Update != nil {
			age := estimateAge(mod.Version, mod.Update.Version)
			if age == "old" {
				risk := RiskMedium
				if mod.Indirect {
					risk = RiskLow
				}
				findings = append(findings, Finding{
					Module:   mod.Path,
					Version:  mod.Version,
					Risk:     risk,
					Category: "outdated",
					Detail:   fmt.Sprintf("Update available: %s -> %s", mod.Version, mod.Update.Version),
				})
			}
		}
	}

	summary := Summary{Total: len(findings)}
	for _, f := range findings {
		switch f.Risk {
		case RiskCritical:
			summary.Critical++
		case RiskHigh:
			summary.High++
		case RiskMedium:
			summary.Medium++
		case RiskLow:
			summary.Low++
		}
	}

	report := Report{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Findings:  findings,
		Summary:   summary,
	}

	out, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(out))

	printHumanSummary(report)

	if summary.Critical > 0 || summary.High > 0 {
		os.Exit(1)
	}
}

func listModules() ([]ModuleInfo, error) {
	cmd := exec.Command("go", "list", "-m", "-json", "-u", "all")
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}

	var modules []ModuleInfo
	dec := json.NewDecoder(strings.NewReader(string(data)))
	for dec.More() {
		var m ModuleInfo
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("parsing module info: %w", err)
		}
		modules = append(modules, m)
	}
	return modules, nil
}

type govulncheckOutput struct {
	Finding *vulnFinding `json:"finding,omitempty"`
}

type vulnFinding struct {
	OSV   string      `json:"osv"`
	Trace []traceElem `json:"trace"`
}

type traceElem struct {
	Module string `json:"module"`
}

func runGovulncheck() map[string]string {
	vulns := make(map[string]string)

	cmd := exec.Command("govulncheck", "-json", "./...")
	data, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(data) > 0 {
			_ = exitErr
		} else {
			fmt.Fprintf(os.Stderr, "Warning: govulncheck failed: %v (install with: go install golang.org/x/vuln/cmd/govulncheck@latest)\n", err)
			return vulns
		}
	}

	dec := json.NewDecoder(strings.NewReader(string(data)))
	for dec.More() {
		var entry govulncheckOutput
		if err := dec.Decode(&entry); err != nil {
			continue
		}
		if entry.Finding != nil && len(entry.Finding.Trace) > 0 {
			mod := entry.Finding.Trace[len(entry.Finding.Trace)-1].Module
			if mod != "" {
				vulns[mod] = entry.Finding.OSV
			}
		}
	}
	return vulns
}

func estimateAge(current, latest string) string {
	curParts := parseVersion(current)
	latParts := parseVersion(latest)
	if curParts == nil || latParts == nil {
		return "unknown"
	}
	majorDiff := latParts[0] - curParts[0]
	minorDiff := latParts[1] - curParts[1]
	if majorDiff > 0 || minorDiff >= 3 {
		return "old"
	}
	return "current"
}

func parseVersion(v string) []int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return nil
	}
	result := make([]int, 0, 3)
	for _, p := range parts {
		p = strings.SplitN(p, "-", 2)[0]
		var n int
		for _, c := range p {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			} else {
				break
			}
		}
		result = append(result, n)
	}
	return result
}

func printHumanSummary(report Report) {
	fmt.Fprintf(os.Stderr, "\n=== Dependency Risk Summary ===\n")
	fmt.Fprintf(os.Stderr, "Total findings: %d (critical: %d, high: %d, medium: %d, low: %d)\n",
		report.Summary.Total, report.Summary.Critical, report.Summary.High,
		report.Summary.Medium, report.Summary.Low)

	if len(report.Findings) == 0 {
		fmt.Fprintf(os.Stderr, "No dependency risks found.\n")
		return
	}

	for _, level := range []RiskLevel{RiskCritical, RiskHigh, RiskMedium, RiskLow} {
		for _, f := range report.Findings {
			if f.Risk != level {
				continue
			}
			fmt.Fprintf(os.Stderr, "  [%s] %s@%s: %s\n",
				strings.ToUpper(string(f.Risk)), f.Module, f.Version, f.Detail)
		}
	}

	if report.Summary.Critical > 0 || report.Summary.High > 0 {
		fmt.Fprintf(os.Stderr, "\nResult: FAIL (critical or high risks found)\n")
	} else {
		fmt.Fprintf(os.Stderr, "\nResult: PASS\n")
	}
}
