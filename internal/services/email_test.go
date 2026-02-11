package services

import (
	"testing"
)

func TestNormalizeEmail_RejectsPlus(t *testing.T) {
	testCases := []struct {
		email string
	}{
		{"user+test@gmail.com"},
		{"john+alias@example.com"},
		{"test+123@yahoo.com"},
	}

	for _, tc := range testCases {
		_, err := NormalizeEmail(tc.email)
		if err != ErrAliasedEmailNotAllowed {
			t.Errorf("NormalizeEmail(%q) expected ErrAliasedEmailNotAllowed, got %v", tc.email, err)
		}
	}
}

func TestNormalizeEmail_NormalizesGmailDots(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"j.o.h.n@gmail.com", "john@gmail.com"},
		{"john.doe@gmail.com", "johndoe@gmail.com"},
		{"J.D.O.E@Gmail.com", "jdoe@gmail.com"},
		{"a.b.c.d@GMAIL.COM", "abcd@gmail.com"},
		{"test..user@gmail.com", "testuser@gmail.com"},
	}

	for _, tc := range testCases {
		result, err := NormalizeEmail(tc.input)
		if err != nil {
			t.Errorf("NormalizeEmail(%q) unexpected error: %v", tc.input, err)
			continue
		}
		if result != tc.expected {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestNormalizeEmail_NormalizesGooglemail(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"user@googlemail.com", "user@gmail.com"},
		{"j.o.h.n@googlemail.com", "john@gmail.com"},
		{"User@GoogleMail.com", "user@gmail.com"},
		{"Test.User@GOOGLEMAIL.COM", "testuser@gmail.com"},
	}

	for _, tc := range testCases {
		result, err := NormalizeEmail(tc.input)
		if err != nil {
			t.Errorf("NormalizeEmail(%q) unexpected error: %v", tc.input, err)
			continue
		}
		if result != tc.expected {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestNormalizeEmail_PreservesDotsForOtherDomains(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"j.doe@yahoo.com", "j.doe@yahoo.com"},
		{"john.smith@outlook.com", "john.smith@outlook.com"},
		{"user.name@example.org", "user.name@example.org"},
		{"J.Doe@YAHOO.COM", "j.doe@yahoo.com"},
	}

	for _, tc := range testCases {
		result, err := NormalizeEmail(tc.input)
		if err != nil {
			t.Errorf("NormalizeEmail(%q) unexpected error: %v", tc.input, err)
			continue
		}
		if result != tc.expected {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestNormalizeEmail_InvalidEmails(t *testing.T) {
	testCases := []struct {
		email string
	}{
		{""},
		{"invalid"},
		{"no-at-sign.com"},
		{"@nodomain.com"},
		{"two@@ats.com"},
	}

	for _, tc := range testCases {
		_, err := NormalizeEmail(tc.email)
		if err == nil {
			t.Errorf("NormalizeEmail(%q) expected error, got nil", tc.email)
		}
	}
}

func TestContainsEmailAlias(t *testing.T) {
	testCases := []struct {
		email    string
		expected bool
	}{
		{"user+test@gmail.com", true},
		{"user@gmail.com", false},
		{"john+@example.com", true},
		{"+test@example.com", true},
		{"noplussign@example.com", false},
	}

	for _, tc := range testCases {
		result := ContainsEmailAlias(tc.email)
		if result != tc.expected {
			t.Errorf("ContainsEmailAlias(%q) = %v, want %v", tc.email, result, tc.expected)
		}
	}
}
