package postgres

import "testing"

func TestNormalizeEmail(t *testing.T) {
	got, err := normalizeEmail("  Admin@Example.COM ")
	if err != nil || got != "admin@example.com" {
		t.Fatalf("normalizeEmail() = %q, %v", got, err)
	}
}

func TestNormalizeEmailRejectsDisplayName(t *testing.T) {
	if _, err := normalizeEmail("Admin <admin@example.com>"); err == nil {
		t.Fatal("normalizeEmail() error = nil")
	}
}
