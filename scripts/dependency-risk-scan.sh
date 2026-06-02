#!/usr/bin/env bash
#
# Dependency Risk Scanner
#
# Combines `go list -m -u all` (outdated/replaced detection), govulncheck
# (CVEs against the Go vulnerability database), and simple heuristics about
# maintenance state (release age, indirect/direct, replace directives).
#
# Writes a Markdown report to stdout. Designed to be uploaded as a CI artifact
# or piped to a file locally.
#
# Usage:
#   bash scripts/dependency-risk-scan.sh > report.md
#   just deps-risk
#
# Exit codes:
#   0 - scan completed (report written to stdout)
#   1 - tooling missing or repo not detected
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ ! -f "$REPO_ROOT/go.mod" ]; then
    echo "error: go.mod not found at $REPO_ROOT" >&2
    exit 1
fi

cd "$REPO_ROOT"

if ! command -v go >/dev/null 2>&1; then
    echo "error: 'go' is required but not installed" >&2
    exit 1
fi

if ! command -v govulncheck >/dev/null 2>&1; then
    echo "info: installing govulncheck..." >&2
    go install golang.org/x/vuln/cmd/govulncheck@latest
    # Ensure GOBIN / GOPATH/bin is on PATH for the rest of the script.
    GOBIN="$(go env GOBIN)"
    if [ -z "$GOBIN" ]; then
        GOBIN="$(go env GOPATH)/bin"
    fi
    export PATH="$GOBIN:$PATH"
fi

TIMESTAMP="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
GO_VERSION="$(go version | awk '{print $3}')"
MODULE_PATH="$(go list -m 2>/dev/null || echo unknown)"

# Stale threshold (days) for highlighting modules whose latest release is old.
STALE_DAYS="${STALE_DAYS:-365}"
STALE_SECS=$((STALE_DAYS * 86400))
NOW_EPOCH="$(date -u +%s)"

# Capture module listing once with template formatting so the rest of the
# script can parse it with pure shell tools (no jq dependency).
# Fields: path|kind|version|latest|released|outdated|replace
MODULES_FILE="$(mktemp)"
trap 'rm -f "$MODULES_FILE" "$VULN_TEXT_FILE" "$VULN_JSON_FILE"' EXIT

go list -m -u -f '{{if not .Main}}{{.Path}}|{{if .Indirect}}indirect{{else}}direct{{end}}|{{.Version}}|{{if .Update}}{{.Update.Version}}{{else}}-{{end}}|{{if .Time}}{{.Time.Format "2006-01-02"}}{{else}}-{{end}}|{{if .Update}}yes{{else}}no{{end}}|{{if .Replace}}{{.Replace.Path}}@{{.Replace.Version}}{{else}}-{{end}}{{end}}' all > "$MODULES_FILE" 2>/dev/null || true

# Run govulncheck in both human and JSON modes once each.
VULN_TEXT_FILE="$(mktemp)"
VULN_JSON_FILE="$(mktemp)"
govulncheck ./... > "$VULN_TEXT_FILE" 2>&1 || true
govulncheck -format json ./... > "$VULN_JSON_FILE" 2>/dev/null || true

# --- Summary counts ----------------------------------------------------------

TOTAL_MODULES=0
DIRECT_MODULES=0
INDIRECT_MODULES=0
OUTDATED_MODULES=0
STALE_MODULES=0
REPLACED_MODULES=0

while IFS='|' read -r path kind version latest released outdated replace; do
    [ -z "${path:-}" ] && continue
    TOTAL_MODULES=$((TOTAL_MODULES + 1))
    case "$kind" in
        direct) DIRECT_MODULES=$((DIRECT_MODULES + 1));;
        indirect) INDIRECT_MODULES=$((INDIRECT_MODULES + 1));;
    esac
    if [ "$outdated" = "yes" ]; then
        OUTDATED_MODULES=$((OUTDATED_MODULES + 1))
    fi
    if [ "$replace" != "-" ] && [ -n "$replace" ]; then
        REPLACED_MODULES=$((REPLACED_MODULES + 1))
    fi
    if [ "$released" != "-" ] && [ -n "$released" ]; then
        if released_epoch=$(date -u -d "$released" +%s 2>/dev/null); then
            age=$((NOW_EPOCH - released_epoch))
            if [ "$age" -gt "$STALE_SECS" ]; then
                STALE_MODULES=$((STALE_MODULES + 1))
            fi
        fi
    fi
done < "$MODULES_FILE"

# Vulnerability counts come from the govulncheck text summary line(s).
# Flatten newlines so the summary sentence (which wraps mid-line) can be matched.
VULN_TEXT_FLAT="$(tr '\n' ' ' < "$VULN_TEXT_FILE")"
ACTIVE_VULNS="$(printf '%s' "$VULN_TEXT_FLAT" | grep -oE 'affected by [0-9]+ vulnerabilit(y|ies)' | grep -oE '[0-9]+' | awk '{s+=$1} END {print s+0}' || echo 0)"
LATENT_VULNS_PKGS="$(printf '%s' "$VULN_TEXT_FLAT" | grep -oE 'found [0-9]+ vulnerabilit(y|ies) in packages you import' | grep -oE '[0-9]+' | head -1 || echo 0)"
LATENT_VULNS_MODS="$(printf '%s' "$VULN_TEXT_FLAT" | grep -oE '[0-9]+ vulnerabilities in modules you require' | grep -oE '[0-9]+' | head -1 || echo 0)"
ACTIVE_VULNS="${ACTIVE_VULNS:-0}"
LATENT_VULNS_PKGS="${LATENT_VULNS_PKGS:-0}"
LATENT_VULNS_MODS="${LATENT_VULNS_MODS:-0}"

# --- Report ------------------------------------------------------------------

cat <<EOF
# Dependency Risk Scan

**Generated:** ${TIMESTAMP}
**Repository:** ${MODULE_PATH}
**Go toolchain:** ${GO_VERSION}
**Stale-release threshold:** ${STALE_DAYS} days

## Summary

| Metric | Count |
|--------|-------|
| Modules tracked | ${TOTAL_MODULES} |
| Direct dependencies | ${DIRECT_MODULES} |
| Indirect dependencies | ${INDIRECT_MODULES} |
| Modules with newer versions available | ${OUTDATED_MODULES} |
| Modules whose latest release is > ${STALE_DAYS}d old | ${STALE_MODULES} |
| \`replace\` directives in effect | ${REPLACED_MODULES} |
| Active vulnerabilities (your code calls them) | ${ACTIVE_VULNS} |
| Latent vulnerabilities in imported packages | ${LATENT_VULNS_PKGS} |
| Latent vulnerabilities in required modules | ${LATENT_VULNS_MODS} |

## Module Inventory

Source: \`go list -m -u all\`. The "Latest release" column reflects when the
*currently selected* version was published, not the most recent version.

| Module | Kind | Current | Latest available | Current released | Outdated | Replace |
|--------|------|---------|------------------|------------------|----------|---------|
EOF

while IFS='|' read -r path kind version latest released outdated replace; do
    [ -z "${path:-}" ] && continue
    printf '| `%s` | %s | `%s` | `%s` | %s | %s | %s |\n' \
        "$path" "$kind" "$version" "$latest" "$released" "$outdated" "$replace"
done < "$MODULES_FILE"

cat <<EOF

## Stale Releases

Modules whose currently selected version was released more than ${STALE_DAYS}
days ago. Old releases are not automatically risky, but indicate either a
mature stable dependency or an abandoned one — worth a manual review.

| Module | Version | Released | Age (days) |
|--------|---------|----------|------------|
EOF

while IFS='|' read -r path kind version latest released outdated replace; do
    [ -z "${path:-}" ] && continue
    [ "$released" = "-" ] && continue
    released_epoch=$(date -u -d "$released" +%s 2>/dev/null || echo 0)
    [ "$released_epoch" -eq 0 ] && continue
    age_secs=$((NOW_EPOCH - released_epoch))
    if [ "$age_secs" -gt "$STALE_SECS" ]; then
        age_days=$((age_secs / 86400))
        printf '| `%s` | `%s` | %s | %s |\n' "$path" "$version" "$released" "$age_days"
    fi
done < "$MODULES_FILE"

cat <<EOF

## Replace Directives

EOF

REPLACED_LINES="$(awk '/^[[:space:]]*replace[[:space:]]/{print; in_block=0} /^replace[[:space:]]*\(/{in_block=1; next} in_block && /^\)/{in_block=0; next} in_block{print}' go.mod | grep -v '^[[:space:]]*$' || true)"
if [ -n "$REPLACED_LINES" ]; then
    echo '```'
    echo "$REPLACED_LINES"
    echo '```'
else
    echo "No \`replace\` directives in go.mod."
fi

cat <<EOF

## Vulnerability Scan

Source: \`govulncheck ./...\` (Go vulnerability database).

\`\`\`
EOF

cat "$VULN_TEXT_FILE"

cat <<EOF
\`\`\`

## Interpretation

- **Active vulnerabilities** are the ones to prioritize: govulncheck has traced
  a path from your code to a vulnerable symbol.
- **Latent vulnerabilities** exist in modules you depend on but your code does
  not call. These are lower priority but should still be tracked, since a
  future code change could activate them.
- **Outdated modules** are not vulnerable by definition, but updating them
  reduces future drift and patch debt.
- **Stale releases** combined with no available update usually mean a mature
  dependency; combined with a much newer available version it suggests
  intentional pinning that may need a re-evaluation.

---
*Generated by \`scripts/dependency-risk-scan.sh\`. To re-run locally: \`just deps-risk\`.*
EOF
