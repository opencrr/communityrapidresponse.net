package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// RiskLevel represents the severity of a dependency risk.
type RiskLevel string

const (
	RiskCritical RiskLevel = "critical"
	RiskHigh     RiskLevel = "high"
	RiskMedium   RiskLevel = "medium"
	RiskLow      RiskLevel = "low"
)

// GoListModule represents the JSON output of `go list -m -u -json all`.
type GoListModule struct {
	Path       string       `json:"Path"`
	Version    string       `json:"Version"`
	Update     *ModVersion  `json:"Update,omitempty"`
	Indirect   bool         `json:"Indirect"`
	Main       bool         `json:"Main,omitempty"`
	Deprecated string       `json:"Deprecated,omitempty"`
	Retracted  []string     `json:"Retracted,omitempty"`
	Replace    *GoListModule `json:"Replace,omitempty"`
	Time       *time.Time   `json:"Time,omitempty"`
}

// ModVersion represents a module version.
type ModVersion struct {
	Path    string     `json:"Path"`
	Version string     `json:"Version"`
	Time    *time.Time `json:"Time,omitempty"`
}

// VulncheckOutput represents the JSON output of govulncheck.
type VulncheckOutput struct {
	Vulns []VulnEntry `json:"vulns,omitempty"`
}

// VulnEntry represents a single vulnerability from govulncheck.
type VulnEntry struct {
	OSV    OSVEntry    `json:"osv"`
	Modules []VulnModule `json:"modules,omitempty"`
}

// OSVEntry represents the OSV data for a vulnerability.
type OSVEntry struct {
	ID               string            `json:"id"`
	Summary          string            `json:"summary"`
	DatabaseSpecific *DatabaseSpecific `json:"database_specific,omitempty"`
}

// DatabaseSpecific holds the severity from the OSV database.
type DatabaseSpecific struct {
	Severity string `json:"severity,omitempty"`
}

// VulnModule represents an affected module in a vulnerability.
type VulnModule struct {
	Path     string   `json:"path"`
	Versions []string `json:"versions,omitempty"`
}

// DependencyRisk represents the assessed risk for a single dependency.
type DependencyRisk struct {
	Module          string    `json:"module"`
	CurrentVersion  string    `json:"current_version"`
	LatestVersion   string    `json:"latest_version,omitempty"`
	Outdated        bool      `json:"outdated"`
	Deprecated      bool      `json:"deprecated"`
	DeprecationMsg  string    `json:"deprecation_message,omitempty"`
	Retracted       bool      `json:"retracted"`
	RetractedMsgs   []string  `json:"retracted_messages,omitempty"`
	Vulnerabilities []string  `json:"vulnerabilities,omitempty"`
	Indirect        bool      `json:"indirect"`
	RiskLevel       RiskLevel `json:"risk_level"`
	Reasons         []string  `json:"reasons"`
}

// Report is the complete dependency risk report.
type Report struct {
	GeneratedAt    string           `json:"generated_at"`
	TotalDeps      int              `json:"total_deps"`
	DirectDeps     int              `json:"direct_deps"`
	IndirectDeps   int              `json:"indirect_deps"`
	CriticalCount  int              `json:"critical_count"`
	HighCount      int              `json:"high_count"`
	MediumCount    int              `json:"medium_count"`
	LowCount       int              `json:"low_count"`
	Dependencies   []DependencyRisk `json:"dependencies"`
}

// parseGoListOutput parses the streaming JSON output from `go list -m -u -json all`.
func parseGoListOutput(data []byte) ([]GoListModule, error) {
	var modules []GoListModule
	dec := json.NewDecoder(strings.NewReader(string(data)))
	for dec.More() {
		var m GoListModule
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("parsing go list output: %w", err)
		}
		if m.Main {
			continue
		}
		modules = append(modules, m)
	}
	return modules, nil
}

// parseVulncheckOutput parses govulncheck JSON output.
// govulncheck outputs streaming JSON messages; we extract vulnerability findings.
func parseVulncheckOutput(data []byte) (map[string][]string, error) {
	vulns := make(map[string][]string)
	if len(data) == 0 {
		return vulns, nil
	}

	// govulncheck -json outputs newline-delimited JSON messages.
	// Each message has a "finding" field when a vulnerability is found.
	dec := json.NewDecoder(strings.NewReader(string(data)))
	for dec.More() {
		var msg map[string]json.RawMessage
		if err := dec.Decode(&msg); err != nil {
			// Skip malformed entries
			continue
		}

		// Check for "osv" message type (vulnerability metadata)
		if osvRaw, ok := msg["osv"]; ok {
			var osv OSVEntry
			if err := json.Unmarshal(osvRaw, &osv); err == nil {
				// Store the OSV ID; we'll map it to modules from "finding" messages
				_ = osv
			}
		}

		// Check for "finding" message type (affected module)
		if findingRaw, ok := msg["finding"]; ok {
			var finding struct {
				OSV   string `json:"osv"`
				Trace []struct {
					Module  string `json:"module"`
					Version string `json:"version"`
				} `json:"trace"`
			}
			if err := json.Unmarshal(findingRaw, &finding); err == nil {
				for _, t := range finding.Trace {
					if t.Module != "" {
						vulns[t.Module] = appendUnique(vulns[t.Module], finding.OSV)
					}
				}
			}
		}
	}
	return vulns, nil
}

// appendUnique appends s to the slice only if not already present.
func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

// assessRisk determines the risk level for a dependency.
func assessRisk(mod GoListModule, vulns map[string][]string) DependencyRisk {
	risk := DependencyRisk{
		Module:         mod.Path,
		CurrentVersion: mod.Version,
		Indirect:       mod.Indirect,
	}

	if mod.Update != nil {
		risk.Outdated = true
		risk.LatestVersion = mod.Update.Version
		risk.Reasons = append(risk.Reasons, fmt.Sprintf("outdated: %s available (current: %s)", mod.Update.Version, mod.Version))
	}

	if mod.Deprecated != "" {
		risk.Deprecated = true
		risk.DeprecationMsg = mod.Deprecated
		risk.Reasons = append(risk.Reasons, fmt.Sprintf("deprecated: %s", mod.Deprecated))
	}

	if len(mod.Retracted) > 0 {
		risk.Retracted = true
		risk.RetractedMsgs = mod.Retracted
		risk.Reasons = append(risk.Reasons, fmt.Sprintf("retracted: %s", strings.Join(mod.Retracted, "; ")))
	}

	if modVulns, ok := vulns[mod.Path]; ok && len(modVulns) > 0 {
		risk.Vulnerabilities = modVulns
		risk.Reasons = append(risk.Reasons, fmt.Sprintf("vulnerabilities: %s", strings.Join(modVulns, ", ")))
	}

	risk.RiskLevel = calculateRiskLevel(risk)
	return risk
}

// calculateRiskLevel assigns a risk level based on the dependency's issues.
func calculateRiskLevel(risk DependencyRisk) RiskLevel {
	if len(risk.Vulnerabilities) > 0 {
		if risk.Indirect {
			return RiskHigh
		}
		return RiskCritical
	}
	if risk.Retracted {
		return RiskHigh
	}
	if risk.Deprecated {
		return RiskHigh
	}
	if risk.Outdated {
		return RiskMedium
	}
	return RiskLow
}

// buildReport creates a risk report from modules and vulnerability data.
func buildReport(modules []GoListModule, vulns map[string][]string) Report {
	report := Report{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	for _, mod := range modules {
		risk := assessRisk(mod, vulns)
		report.TotalDeps++
		if mod.Indirect {
			report.IndirectDeps++
		} else {
			report.DirectDeps++
		}

		switch risk.RiskLevel {
		case RiskCritical:
			report.CriticalCount++
		case RiskHigh:
			report.HighCount++
		case RiskMedium:
			report.MediumCount++
		case RiskLow:
			report.LowCount++
		}

		// Only include dependencies with issues in the report
		if len(risk.Reasons) > 0 {
			report.Dependencies = append(report.Dependencies, risk)
		}
	}

	// Sort: critical first, then high, medium, low
	sort.Slice(report.Dependencies, func(i, j int) bool {
		return riskOrdinal(report.Dependencies[i].RiskLevel) < riskOrdinal(report.Dependencies[j].RiskLevel)
	})

	return report
}

func riskOrdinal(r RiskLevel) int {
	switch r {
	case RiskCritical:
		return 0
	case RiskHigh:
		return 1
	case RiskMedium:
		return 2
	case RiskLow:
		return 3
	default:
		return 4
	}
}

// formatHumanReport produces a human-readable summary.
func formatHumanReport(r Report) string {
	var sb strings.Builder
	sb.WriteString("=== Dependency Risk Report ===\n")
	sb.WriteString(fmt.Sprintf("Generated: %s\n", r.GeneratedAt))
	sb.WriteString(fmt.Sprintf("Total dependencies: %d (direct: %d, indirect: %d)\n\n", r.TotalDeps, r.DirectDeps, r.IndirectDeps))

	sb.WriteString(fmt.Sprintf("Risk Summary:\n"))
	sb.WriteString(fmt.Sprintf("  Critical: %d\n", r.CriticalCount))
	sb.WriteString(fmt.Sprintf("  High:     %d\n", r.HighCount))
	sb.WriteString(fmt.Sprintf("  Medium:   %d\n", r.MediumCount))
	sb.WriteString(fmt.Sprintf("  Low:      %d\n\n", r.LowCount))

	if len(r.Dependencies) == 0 {
		sb.WriteString("No dependency risks found.\n")
		return sb.String()
	}

	for _, dep := range r.Dependencies {
		sb.WriteString(fmt.Sprintf("[%s] %s @ %s\n", strings.ToUpper(string(dep.RiskLevel)), dep.Module, dep.CurrentVersion))
		for _, reason := range dep.Reasons {
			sb.WriteString(fmt.Sprintf("  - %s\n", reason))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func main() {
	jsonOutput := flag.Bool("json", false, "Output report in JSON format")
	outputFile := flag.String("output", "", "Write report to file (default: stdout)")
	skipVuln := flag.Bool("skip-vuln", false, "Skip govulncheck (faster, less complete)")
	flag.Parse()

	fmt.Fprintln(os.Stderr, "Analyzing dependencies...")

	// Run go list -m -u -json all
	goListCmd := exec.Command("go", "list", "-m", "-u", "-json", "all")
	goListOut, err := goListCmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running go list: %v\n", err)
		os.Exit(1)
	}

	modules, err := parseGoListOutput(goListOut)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing go list output: %v\n", err)
		os.Exit(1)
	}

	// Run govulncheck
	vulns := make(map[string][]string)
	if !*skipVuln {
		fmt.Fprintln(os.Stderr, "Running govulncheck...")
		vulnCmd := exec.Command("govulncheck", "-json", "./...")
		vulnOut, err := vulnCmd.Output()
		if err != nil {
			// govulncheck returns non-zero when vulns are found; check if we got output
			if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 && len(vulnOut) == 0 {
				fmt.Fprintf(os.Stderr, "Warning: govulncheck failed: %s\n", string(exitErr.Stderr))
			}
		}
		if len(vulnOut) > 0 {
			vulns, _ = parseVulncheckOutput(vulnOut)
		}
	}

	report := buildReport(modules, vulns)

	var output string
	if *jsonOutput {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling report: %v\n", err)
			os.Exit(1)
		}
		output = string(data)
	} else {
		output = formatHumanReport(report)
	}

	if *outputFile != "" {
		if err := os.WriteFile(*outputFile, []byte(output), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing to file: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Report written to %s\n", *outputFile)
	} else {
		fmt.Print(output)
	}

	// Exit with non-zero status if critical risks found
	if report.CriticalCount > 0 {
		os.Exit(2)
	}
}
