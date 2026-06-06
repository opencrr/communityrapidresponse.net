package main

import "testing"

func defaultCfg() config {
	return config{MaxFileLines: 500, MaxFuncLines: 80, MaxNesting: 5}
}

func findOne(t *testing.T, fs []finding, tag string) finding {
	t.Helper()
	var matches []finding
	for _, f := range fs {
		if f.Tag == tag {
			matches = append(matches, f)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 finding with tag %q, got %d (%+v)", tag, len(matches), fs)
	}
	return matches[0]
}

func TestErrorHandling_DiscardedErrorInHandlerIsHigh(t *testing.T) {
	src := `package handlers

func H() {
	_ = repo.Save()
}
`
	got := scanFile("internal/handlers/example.go", src, defaultCfg())
	f := findOne(t, got, "discarded-error")
	if f.Category != catErrorHandling {
		t.Errorf("category = %q, want %q", f.Category, catErrorHandling)
	}
	if f.Priority != prHigh {
		t.Errorf("priority = %q, want HIGH (handlers is a sensitive package)", f.Priority)
	}
}

func TestErrorHandling_BarePanicInServicesIsHigh(t *testing.T) {
	src := `package services

func S() {
	panic("nope")
}
`
	got := scanFile("internal/services/example.go", src, defaultCfg())
	f := findOne(t, got, "bare-panic")
	if f.Category != catErrorHandling {
		t.Errorf("category = %q, want %q", f.Category, catErrorHandling)
	}
	if f.Priority != prHigh {
		t.Errorf("priority = %q, want HIGH (services is a sensitive package)", f.Priority)
	}
}

func TestErrorHandling_DiscardedErrorInNonCoreIsMedium(t *testing.T) {
	src := `package database

func D() {
	_ = rows.Close()
}
`
	got := scanFile("internal/database/example.go", src, defaultCfg())
	f := findOne(t, got, "discarded-error")
	if f.Priority != prMedium {
		t.Errorf("priority = %q, want MEDIUM (database is not in the sensitive list)", f.Priority)
	}
}

func TestErrorHandling_IgnoredInTestFiles(t *testing.T) {
	src := `package handlers

func TestX(t *testing.T) {
	_ = repo.Save()
	panic("ok in tests")
}
`
	got := scanFile("internal/handlers/example_test.go", src, defaultCfg())
	for _, f := range got {
		if f.Category == catErrorHandling {
			t.Errorf("did not expect error-handling finding in test file, got %+v", f)
		}
	}
}

func TestErrorHandling_PlainUnderscoreAssignNotFlagged(t *testing.T) {
	// "_ = body" — no parens on RHS — should NOT be flagged as discarded-error.
	src := `package handlers

func H() {
	body := "x"
	_ = body
}
`
	got := scanFile("internal/handlers/example.go", src, defaultCfg())
	for _, f := range got {
		if f.Tag == "discarded-error" {
			t.Errorf("plain identifier assign should not be flagged, got %+v", f)
		}
	}
}
