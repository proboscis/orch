package model

import (
	"strings"
	"testing"
)

// ADR-0001: the issue hex ID is derived — lowercase hex sha256 of the issue ID
// string — never stored. The golden value pins the derivation forever.
func TestIssueHexIDGolden(t *testing.T) {
	got := IssueHexID(IssueID("hello-world"))
	want := "afa27b44d43b02a9fea41d13cedc2e4016cfcf87c5dbf990e593669aa8ce286d"
	if got != want {
		t.Fatalf("IssueHexID(hello-world) = %s, want %s", got, want)
	}
}

func TestIssueShortHexID(t *testing.T) {
	short := IssueShortHexID(IssueID("hello-world"))
	if short != "afa27b44" {
		t.Fatalf("IssueShortHexID(hello-world) = %s, want afa27b44", short)
	}
	if len(short) != 8 {
		t.Fatalf("short hex ID length = %d, want 8", len(short))
	}
	if !strings.HasPrefix(IssueHexID(IssueID("hello-world")), short) {
		t.Fatal("short hex ID must be a prefix of the full hex ID")
	}
}

// ADR-0001 grammar partition: 2-6 hex chars mean run short ID, 7-64 mean
// issue hex ref. IsIssueHexRef accepts only the issue side.
func TestIsIssueHexRef(t *testing.T) {
	valid := []string{
		"abc1234",
		"afa27b44",
		"0123456",
		strings.Repeat("a", 64),
	}
	for _, s := range valid {
		if !IsIssueHexRef(s) {
			t.Errorf("IsIssueHexRef(%q) = false, want true", s)
		}
	}

	invalid := []string{
		"",
		"3f2a91",  // 6 chars: run short ID territory
		"ab",      // 2 chars: run short ID territory
		"ABC1234", // uppercase is not a hex ref
		"xyz1234",
		"hello-world",
		strings.Repeat("a", 65),
	}
	for _, s := range invalid {
		if IsIssueHexRef(s) {
			t.Errorf("IsIssueHexRef(%q) = true, want false", s)
		}
	}
}

// ValidateNewIssueID is the single creation-guard entry point every issue
// write site must call (store, daemon cores, CLI local/editor paths).
func TestValidateNewIssueID(t *testing.T) {
	if err := ValidateNewIssueID("beef"); err == nil {
		t.Fatal("ValidateNewIssueID(beef) = nil, want hex-lookalike rejection")
	}
	if err := ValidateNewIssueID("issue-0001"); err != nil {
		t.Fatalf("ValidateNewIssueID(issue-0001) = %v, want nil", err)
	}
}

// ADR-0001 creation guard: new issue IDs that consist purely of 2-64 hex
// chars would be shadowed by the run short ID grammar or collide with issue
// hex refs, so they are rejected at creation time.
func TestIsHexLikeIssueID(t *testing.T) {
	hexLike := []string{
		"0001",
		"beef",
		"12",
		"abc1234",
		strings.Repeat("f", 64),
	}
	for _, s := range hexLike {
		if !IsHexLikeIssueID(s) {
			t.Errorf("IsHexLikeIssueID(%q) = false, want true", s)
		}
	}

	allowed := []string{
		"",
		"a", // single char never collides (run grammar starts at 2)
		"my-001",
		"issue-0001",
		"hello-world",
		"gh-123",
		strings.Repeat("f", 65),
	}
	for _, s := range allowed {
		if IsHexLikeIssueID(s) {
			t.Errorf("IsHexLikeIssueID(%q) = true, want false", s)
		}
	}
}
