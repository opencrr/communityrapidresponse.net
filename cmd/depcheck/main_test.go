package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseGoListOutput(t *testing.T) {
	input := `{
	"Path": "github.com/example/foo",
	"Version": "v1.0.0",
	"Update": {"Path": "github.com/example/foo", "Version": "v1.2.0"},
	"Indirect": false
}
{
	"Path": "github.com/example/bar",
	"Version": "v0.5.0",
	"Indirect": true
}
{
	"Path": "myproject",
	"Main": true
}
`
	modules, err := parseGoListOutput([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(modules) != 2 {
		t.Fatalf("expected 2 modules (main excluded), got %d", len(modules))
	}
	if modules[0].Path != "github.com/example/foo" {
		t.Errorf("expected foo, got %s", modules[0].Path)
	}
	if modules[0].Update == nil || modules[0].Update.Version != "v1.2.0" {
		t.Error("expected update to v1.2.0")
	}
	if modules[1].Indirect != true {
		t.Error("expected bar to be indirect")
	}
}

func TestParseGoListOutput_Deprecated(t *testing.T) {
	input := `{
	"Path": "github.com/old/pkg",
	"Version": "v1.0.0",
	"Deprecated": "Use github.com/new/pkg instead"
}
`
	modules, err := parseGoListOutput([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
	if modules[0].Deprecated != "Use github.com/new/pkg instead" {
		t.Errorf("expected deprecation message, got %q", modules[0].Deprecated)
	}
}

func TestParseGoListOutput_Retracted(t *testing.T) {
	input := `{
	"Path": "github.com/bad/pkg",
	"Version": "v1.0.0",
	"Retracted": ["security issue", "broken release"]
}
`
	modules, err := parseGoListOutput([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
	if len(modules[0].Retracted) != 2 {
		t.Errorf("expected 2 retraction messages, got %d", len(modules[0].Retracted))
	}
}

func TestParseGoListOutput_Empty(t *testing.T) {
	modules, err := parseGoListOutput([]byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(modules) != 0 {
		t.Fatalf("expected 0 modules, got %d", len(modules))
	}
}

func TestParseVulncheckOutput(t *testing.T) {
	input := `{"finding": {"osv": "GO-2024-0001", "trace": [{"module": "github.com/example/foo", "version": "v1.0.0"}]}}
{"finding": {"osv": "GO-2024-0002", "trace": [{"module": "github.com/example/foo", "version": "v1.0.0"}]}}
{"finding": {"osv": "GO-2024-0001", "trace": [{"module": "github.com/example/bar", "version": "v0.5.0"}]}}
`
	vulns, err := parseVulncheckOutput([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vulns["github.com/example/foo"]) != 2 {
		t.Errorf("expected 2 vulns for foo, got %d", len(vulns["github.com/example/foo"]))
	}
	if len(vulns["github.com/example/bar"]) != 1 {
		t.Errorf("expected 1 vuln for bar, got %d", len(vulns["github.com/example/bar"]))
	}
}

func TestParseVulncheckOutput_Empty(t *testing.T) {
	vulns, err := parseVulncheckOutput([]byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vulns) != 0 {
		t.Fatalf("expected 0 vulns, got %d", len(vulns))
	}
}

func TestParseVulncheckOutput_NoDuplicates(t *testing.T) {
	input := `{"finding": {"osv": "GO-2024-0001", "trace": [{"module": "github.com/example/foo", "version": "v1.0.0"}]}}
{"finding": {"osv": "GO-2024-0001", "trace": [{"module": "github.com/example/foo", "version": "v1.0.0"}]}}
`
	vulns, err := parseVulncheckOutput([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vulns["github.com/example/foo"]) != 1 {
		t.Errorf("expected 1 unique vuln for foo, got %d", len(vulns["github.com/example/foo"]))
	}
}

func TestAssessRisk_Vulnerability_Direct(t *testing.T) {
	mod := GoListModule{Path: "github.com/example/foo", Version: "v1.0.0", Indirect: false}
	vulns := map[string][]string{"github.com/example/foo": {"GO-2024-0001"}}
	risk := assessRisk(mod, vulns)
	if risk.RiskLevel != RiskCritical {
		t.Errorf("expected critical risk for direct dep with vuln, got %s", risk.RiskLevel)
	}
	if len(risk.Vulnerabilities) != 1 {
		t.Errorf("expected 1 vulnerability, got %d", len(risk.Vulnerabilities))
	}
}

func TestAssessRisk_Vulnerability_Indirect(t *testing.T) {
	mod := GoListModule{Path: "github.com/example/foo", Version: "v1.0.0", Indirect: true}
	vulns := map[string][]string{"github.com/example/foo": {"GO-2024-0001"}}
	risk := assessRisk(mod, vulns)
	if risk.RiskLevel != RiskHigh {
		t.Errorf("expected high risk for indirect dep with vuln, got %s", risk.RiskLevel)
	}
}

func TestAssessRisk_Deprecated(t *testing.T) {
	mod := GoListModule{Path: "github.com/old/pkg", Version: "v1.0.0", Deprecated: "use new/pkg"}
	risk := assessRisk(mod, nil)
	if risk.RiskLevel != RiskHigh {
		t.Errorf("expected high risk for deprecated dep, got %s", risk.RiskLevel)
	}
	if !risk.Deprecated {
		t.Error("expected deprecated flag")
	}
}

func TestAssessRisk_Retracted(t *testing.T) {
	mod := GoListModule{Path: "github.com/bad/pkg", Version: "v1.0.0", Retracted: []string{"broken"}}
	risk := assessRisk(mod, nil)
	if risk.RiskLevel != RiskHigh {
		t.Errorf("expected high risk for retracted dep, got %s", risk.RiskLevel)
	}
	if !risk.Retracted {
		t.Error("expected retracted flag")
	}
}

func TestAssessRisk_Outdated(t *testing.T) {
	mod := GoListModule{
		Path:    "github.com/example/foo",
		Version: "v1.0.0",
		Update:  &ModVersion{Version: "v1.2.0"},
	}
	risk := assessRisk(mod, nil)
	if risk.RiskLevel != RiskMedium {
		t.Errorf("expected medium risk for outdated dep, got %s", risk.RiskLevel)
	}
	if !risk.Outdated {
		t.Error("expected outdated flag")
	}
	if risk.LatestVersion != "v1.2.0" {
		t.Errorf("expected latest version v1.2.0, got %s", risk.LatestVersion)
	}
}

func TestAssessRisk_NoIssues(t *testing.T) {
	mod := GoListModule{Path: "github.com/good/pkg", Version: "v1.0.0"}
	risk := assessRisk(mod, nil)
	if risk.RiskLevel != RiskLow {
		t.Errorf("expected low risk for clean dep, got %s", risk.RiskLevel)
	}
	if len(risk.Reasons) != 0 {
		t.Errorf("expected no reasons, got %v", risk.Reasons)
	}
}

func TestBuildReport(t *testing.T) {
	modules := []GoListModule{
		{Path: "github.com/vuln/pkg", Version: "v1.0.0", Indirect: false},
		{Path: "github.com/old/pkg", Version: "v1.0.0", Deprecated: "use new/pkg"},
		{Path: "github.com/outdated/pkg", Version: "v1.0.0", Update: &ModVersion{Version: "v2.0.0"}},
		{Path: "github.com/good/pkg", Version: "v1.0.0", Indirect: true},
	}
	vulns := map[string][]string{"github.com/vuln/pkg": {"GO-2024-0001"}}

	report := buildReport(modules, vulns)

	if report.TotalDeps != 4 {
		t.Errorf("expected 4 total deps, got %d", report.TotalDeps)
	}
	if report.DirectDeps != 3 {
		t.Errorf("expected 3 direct deps, got %d", report.DirectDeps)
	}
	if report.IndirectDeps != 1 {
		t.Errorf("expected 1 indirect dep, got %d", report.IndirectDeps)
	}
	if report.CriticalCount != 1 {
		t.Errorf("expected 1 critical, got %d", report.CriticalCount)
	}
	if report.HighCount != 1 {
		t.Errorf("expected 1 high, got %d", report.HighCount)
	}
	if report.MediumCount != 1 {
		t.Errorf("expected 1 medium, got %d", report.MediumCount)
	}
	// good/pkg has no issues, so it's low but not in Dependencies list
	if len(report.Dependencies) != 3 {
		t.Errorf("expected 3 deps with issues, got %d", len(report.Dependencies))
	}
	// Check sort order: critical first
	if report.Dependencies[0].RiskLevel != RiskCritical {
		t.Errorf("expected first dep to be critical, got %s", report.Dependencies[0].RiskLevel)
	}
}

func TestBuildReport_NoDeps(t *testing.T) {
	report := buildReport(nil, nil)
	if report.TotalDeps != 0 {
		t.Errorf("expected 0 total deps, got %d", report.TotalDeps)
	}
	if len(report.Dependencies) != 0 {
		t.Errorf("expected 0 deps, got %d", len(report.Dependencies))
	}
}

func TestFormatHumanReport(t *testing.T) {
	report := Report{
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		TotalDeps:     2,
		DirectDeps:    1,
		IndirectDeps:  1,
		CriticalCount: 1,
		Dependencies: []DependencyRisk{
			{
				Module:         "github.com/vuln/pkg",
				CurrentVersion: "v1.0.0",
				RiskLevel:      RiskCritical,
				Reasons:        []string{"vulnerabilities: GO-2024-0001"},
			},
		},
	}

	output := formatHumanReport(report)
	if !strings.Contains(output, "Dependency Risk Report") {
		t.Error("expected report header")
	}
	if !strings.Contains(output, "[CRITICAL]") {
		t.Error("expected CRITICAL label")
	}
	if !strings.Contains(output, "github.com/vuln/pkg") {
		t.Error("expected module name in output")
	}
}

func TestFormatHumanReport_NoDeps(t *testing.T) {
	report := Report{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	output := formatHumanReport(report)
	if !strings.Contains(output, "No dependency risks found") {
		t.Error("expected 'no risks' message")
	}
}

func TestReportJSONSerialization(t *testing.T) {
	report := Report{
		GeneratedAt:   "2024-01-01T00:00:00Z",
		TotalDeps:     1,
		DirectDeps:    1,
		CriticalCount: 1,
		Dependencies: []DependencyRisk{
			{
				Module:          "github.com/example/foo",
				CurrentVersion:  "v1.0.0",
				LatestVersion:   "v1.2.0",
				Outdated:        true,
				Vulnerabilities: []string{"GO-2024-0001"},
				RiskLevel:       RiskCritical,
				Reasons:         []string{"vulnerabilities: GO-2024-0001"},
			},
		},
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("error unmarshaling: %v", err)
	}
	if decoded.CriticalCount != 1 {
		t.Errorf("expected 1 critical, got %d", decoded.CriticalCount)
	}
	if len(decoded.Dependencies) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(decoded.Dependencies))
	}
	if decoded.Dependencies[0].RiskLevel != RiskCritical {
		t.Errorf("expected critical, got %s", decoded.Dependencies[0].RiskLevel)
	}
}

func TestAppendUnique(t *testing.T) {
	s := appendUnique(nil, "a")
	s = appendUnique(s, "b")
	s = appendUnique(s, "a")
	if len(s) != 2 {
		t.Errorf("expected 2 unique items, got %d", len(s))
	}
}

func TestRiskOrdinal(t *testing.T) {
	if riskOrdinal(RiskCritical) >= riskOrdinal(RiskHigh) {
		t.Error("critical should sort before high")
	}
	if riskOrdinal(RiskHigh) >= riskOrdinal(RiskMedium) {
		t.Error("high should sort before medium")
	}
	if riskOrdinal(RiskMedium) >= riskOrdinal(RiskLow) {
		t.Error("medium should sort before low")
	}
}

func TestCalculateRiskLevel_Priority(t *testing.T) {
	// Vuln on direct dep takes highest priority -> critical
	risk := DependencyRisk{Vulnerabilities: []string{"GO-2024-0001"}, Deprecated: true, Outdated: true}
	if calculateRiskLevel(risk) != RiskCritical {
		t.Errorf("expected critical when vuln on direct dep, got %s", calculateRiskLevel(risk))
	}

	// Vuln on indirect dep -> high
	risk = DependencyRisk{Vulnerabilities: []string{"GO-2024-0001"}, Indirect: true}
	if calculateRiskLevel(risk) != RiskHigh {
		t.Errorf("expected high when vuln on indirect dep, got %s", calculateRiskLevel(risk))
	}

	// Deprecated without vuln -> high
	risk = DependencyRisk{Deprecated: true, Outdated: true}
	if calculateRiskLevel(risk) != RiskHigh {
		t.Errorf("expected high for deprecated, got %s", calculateRiskLevel(risk))
	}

	// Only outdated -> medium
	risk = DependencyRisk{Outdated: true}
	if calculateRiskLevel(risk) != RiskMedium {
		t.Errorf("expected medium for outdated-only, got %s", calculateRiskLevel(risk))
	}
}
