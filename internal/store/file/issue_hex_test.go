package file

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/store"
)

func createHexTestIssue(t *testing.T, vaultPath, issueID string) {
	t.Helper()
	createTestIssue(t, vaultPath, issueID, fmt.Sprintf(`---
type: issue
id: %q
title: "Hex test %s"
status: open
---
body
`, issueID, issueID))
}

// ADR-0001: an issue resolves by full hex ID and by any unique hex prefix of
// at least 7 chars.
func TestResolveIssueByHexRef(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()
	createHexTestIssue(t, vault, "hello-world")
	createHexTestIssue(t, vault, "another-issue")

	s, err := New(vault)
	if err != nil {
		t.Fatal(err)
	}

	full := model.IssueHexID(model.IssueID("hello-world"))
	for _, ref := range []string{full, full[:8], full[:7]} {
		issue, err := s.ResolveIssue(model.IssueID(ref))
		if err != nil {
			t.Fatalf("ResolveIssue(%s) error = %v", ref, err)
		}
		if issue.ID != "hello-world" {
			t.Fatalf("ResolveIssue(%s) = %s, want hello-world", ref, issue.ID)
		}
	}
}

// Exact name match always beats hex-prefix interpretation, so pre-existing
// issues with hex-lookalike names keep resolving to themselves.
func TestResolveIssueExactNameBeatsHexRef(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()
	createHexTestIssue(t, vault, "hello-world")
	// An issue literally named like hello-world's short hex ID (written
	// directly to disk — hand-authored files bypass the creation guard).
	shadow := model.IssueShortHexID(model.IssueID("hello-world"))
	createHexTestIssue(t, vault, shadow)

	s, err := New(vault)
	if err != nil {
		t.Fatal(err)
	}

	issue, err := s.ResolveIssue(model.IssueID(shadow))
	if err != nil {
		t.Fatalf("ResolveIssue(%s) error = %v", shadow, err)
	}
	if string(issue.ID) != shadow {
		t.Fatalf("ResolveIssue(%s) = %s, want exact-name match %s", shadow, issue.ID, shadow)
	}
}

// An ambiguous hex prefix fails loud and names the candidates.
func TestResolveIssueAmbiguousHexRef(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	// Birthday-search two issue names whose hex IDs share a 7-char prefix.
	seen := make(map[string]string)
	var nameA, nameB, prefix string
	for i := 0; ; i++ {
		name := fmt.Sprintf("iss-%d", i)
		p := model.IssueHexID(model.IssueID(name))[:7]
		if prev, ok := seen[p]; ok {
			nameA, nameB, prefix = prev, name, p
			break
		}
		seen[p] = name
	}
	createHexTestIssue(t, vault, nameA)
	createHexTestIssue(t, vault, nameB)

	s, err := New(vault)
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.ResolveIssue(model.IssueID(prefix))
	if err == nil {
		t.Fatalf("ResolveIssue(%s) succeeded, want ambiguity error (issues %s and %s)", prefix, nameA, nameB)
	}
	msg := err.Error()
	if !strings.Contains(msg, nameA) || !strings.Contains(msg, nameB) {
		t.Fatalf("ambiguity error must name both candidates %s and %s, got: %v", nameA, nameB, err)
	}
}

// Run lookups accept issue hex refs for their issue-identifying inputs.
func TestRunLookupsAcceptIssueHexRef(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()
	createHexTestIssue(t, vault, "hex-run-issue")

	s, err := New(vault)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRun(model.IssueID("hex-run-issue"), model.RunID("20260709-000001"), nil); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	hex8 := model.IssueShortHexID(model.IssueID("hex-run-issue"))

	run, err := s.GetRun(&model.RunRef{IssueID: model.IssueID(hex8), RunID: model.RunID("20260709-000001")})
	if err != nil {
		t.Fatalf("GetRun(hex ref) error = %v", err)
	}
	if run.IssueID != "hex-run-issue" {
		t.Fatalf("GetRun(hex ref) issue = %s, want hex-run-issue", run.IssueID)
	}

	latest, err := s.GetLatestRun(model.IssueID(hex8))
	if err != nil {
		t.Fatalf("GetLatestRun(hex ref) error = %v", err)
	}
	if latest.RunID != "20260709-000001" {
		t.Fatalf("GetLatestRun(hex ref) = %s, want 20260709-000001", latest.RunID)
	}

	runs, err := s.ListRuns(&store.ListRunsFilter{IssueID: model.IssueID(hex8)})
	if err != nil {
		t.Fatalf("ListRuns(hex ref) error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("ListRuns(hex ref) = %d runs, want 1", len(runs))
	}
}

// CreateRun with a hex ref must store the run under the canonical issue name,
// never under the hex string.
func TestCreateRunWithHexRefUsesCanonicalIssueDir(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()
	createHexTestIssue(t, vault, "hex-create-issue")

	s, err := New(vault)
	if err != nil {
		t.Fatal(err)
	}

	hex8 := model.IssueShortHexID(model.IssueID("hex-create-issue"))
	run, err := s.CreateRun(model.IssueID(hex8), model.RunID("20260709-000002"), nil)
	if err != nil {
		t.Fatalf("CreateRun(hex ref) error = %v", err)
	}
	if run.IssueID != "hex-create-issue" {
		t.Fatalf("CreateRun(hex ref) issue = %s, want hex-create-issue", run.IssueID)
	}

	if _, err := os.Stat(filepath.Join(vault, "runs", "hex-create-issue")); err != nil {
		t.Fatalf("canonical runs dir missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault, "runs", hex8)); !os.IsNotExist(err) {
		t.Fatalf("runs dir was created under the hex ref %s; it must not be", hex8)
	}
}

// ADR-0001 creation guard: hex-lookalike issue IDs are rejected at creation.
func TestCreateIssueRejectsHexLikeIDs(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	s, err := New(vault)
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"0001", "beef", "abc1234"} {
		err := s.CreateIssue(&model.Issue{ID: model.IssueID(id), Title: "t", Status: model.IssueStatusOpen})
		if err == nil {
			t.Errorf("CreateIssue(%q) succeeded, want hex-lookalike rejection", id)
		}
	}

	if err := s.CreateIssue(&model.Issue{ID: model.IssueID("issue-0001"), Title: "t", Status: model.IssueStatusOpen}); err != nil {
		t.Errorf("CreateIssue(issue-0001) error = %v, want success", err)
	}
}
