package model

import (
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"testing/quick"
)

type repoIDPart string

func (repoIDPart) Generate(r *rand.Rand, size int) reflect.Value {
	const safeChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
	const chars = safeChars + " .'!@#$%^&()+={}[]日本語"
	length := 1 + r.Intn(12)
	safeRunes := []rune(safeChars)
	runes := []rune(chars)
	buf := make([]rune, length)
	for i := range buf {
		buf[i] = runes[r.Intn(len(runes))]
	}
	buf[r.Intn(length)] = safeRunes[r.Intn(len(safeRunes))]
	return reflect.ValueOf(repoIDPart(string(buf)))
}

func TestNewRepoIDNormalizesEquivalentInputsAndRoundTrips(t *testing.T) {
	property := func(ownerPart repoIDPart, repoPart repoIDPart) bool {
		owner := string(ownerPart)
		repo := string(repoPart)
		inputs := []string{
			"https://github.com/" + owner + "/" + repo + ".git",
			"https://github.com/" + owner + "/" + repo,
			"git@github.com:" + owner + "/" + repo + ".git",
			"ssh://git@github.com/" + owner + "/" + repo + ".git",
			"/work/repos/" + owner + "/" + repo,
		}

		for _, input := range inputs {
			want, err := legacyNormalizeRepoIDForTest(input)
			if err != nil {
				return false
			}
			got, err := NewRepoID(input)
			if err != nil || got != want {
				return false
			}
			roundTrip, err := NewRepoID(got.String())
			if err != nil || roundTrip != got {
				return false
			}
		}

		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatal(err)
	}
}

func TestNewRepoIDMatchesLegacyNormalizationForUnsafeInputs(t *testing.T) {
	tests := []string{
		"https://github.com/acme.inc/repo.name.git",
		"https://github.com/O'Reilly/my repo.git",
		"git@github.com:dotted.owner/repo_name.git",
		"ssh://git@github.com/space owner/repo+plus.git",
		"/work/repos/emoji-😀-owner/repo#hash",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			want, err := legacyNormalizeRepoIDForTest(input)
			if err != nil {
				t.Fatalf("legacyNormalizeRepoIDForTest() error = %v", err)
			}

			got, err := NewRepoID(input)
			if err != nil {
				t.Fatalf("NewRepoID() error = %v", err)
			}
			if got != want {
				t.Fatalf("NewRepoID() = %q, want legacy-compatible %q", got, want)
			}

			roundTrip, err := NewRepoID(got.String())
			if err != nil {
				t.Fatalf("NewRepoID(%q) round-trip error = %v", got, err)
			}
			if roundTrip != got {
				t.Fatalf("NewRepoID(%q) = %q, want %q", got, roundTrip, got)
			}
		})
	}
}

func TestNewRepoIDRejectsDegenerateSanitizedInputs(t *testing.T) {
	tests := []string{
		"https://github.com/.../repo.git",
		"https://github.com/acme/!!!.git",
		"/work/repos/日本語/repo",
		"/work/repos/acme/日本語",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := NewRepoID(input); err == nil {
				t.Fatal("NewRepoID() error = nil, want degenerate sanitized id rejection")
			}
		})
	}
}

func TestNewProjectIDNormalizesLikeRepoID(t *testing.T) {
	got, err := NewProjectID("https://github.com/acme.inc/repo.name.git")
	if err != nil {
		t.Fatalf("NewProjectID() error = %v", err)
	}
	if got != ProjectID("acmeinc-reponame") {
		t.Fatalf("NewProjectID() = %q, want %q", got, "acmeinc-reponame")
	}
}

func TestNewRepoIDRejectsRawStringAtCompileTime(t *testing.T) {
	runCompileFailTest(t, "raw-string-repo-id", `package compilefail

import "github.com/s22625/orch/internal/model"

func acceptsRepoID(model.RepoID) {}

func bad() {
	rawURL := "https://github.com/acme/repo.git"
	acceptsRepoID(rawURL)
}
`, "cannot use rawURL", "model.RepoID")
}

func TestProjectIDIsDistinctFromRepoIDAtCompileTime(t *testing.T) {
	runCompileFailTest(t, "repo-id-is-not-project-id", `package compilefail

import "github.com/s22625/orch/internal/model"

func acceptsProjectID(model.ProjectID) {}

func bad() {
	repoID, _ := model.NewRepoID("https://github.com/acme/repo.git")
	acceptsProjectID(repoID)
}
`, "cannot use repoID", "model.ProjectID")
}

func runCompileFailTest(t *testing.T, name, badSource string, wantFragments ...string) {
	t.Helper()

	repoRoot := repoRootForTest(t)
	tmp := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(tmp, 0755); err != nil {
		t.Fatalf("create temp module: %v", err)
	}

	goMod := strings.Join([]string{
		"module github.com/s22625/orch/compilefail",
		"",
		"go 1.25.3",
		"",
		"require github.com/s22625/orch v0.0.0",
		"",
		"replace github.com/s22625/orch => " + filepath.ToSlash(repoRoot),
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmp, "bad.go"), []byte(badSource), 0644); err != nil {
		t.Fatalf("write bad.go: %v", err)
	}

	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = tmp
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected compile failure when passing string to RepoID, got success; output:\n%s", output)
	}

	out := string(output)
	for _, want := range wantFragments {
		if !strings.Contains(out, want) {
			t.Fatalf("compile failure did not mention %q; output:\n%s", want, out)
		}
	}
}

func legacyNormalizeRepoIDForTest(input string) (RepoID, error) {
	target := strings.TrimSpace(input)
	if target == "" {
		return "", os.ErrInvalid
	}

	if matches := legacyRepoIDSshPattern.FindStringSubmatch(target); len(matches) == 3 {
		return legacySanitizeRepoIDForTest(matches[1], matches[2])
	}

	if matches := legacyRepoIDURLPattern.FindStringSubmatch(target); len(matches) == 3 {
		return legacySanitizeRepoIDForTest(matches[1], matches[2])
	}

	cleaned := strings.TrimSuffix(target, ".git")
	parts := strings.Split(cleaned, "/")
	if len(parts) >= 2 {
		owner := parts[len(parts)-2]
		repo := parts[len(parts)-1]
		if idx := strings.LastIndex(owner, ":"); idx != -1 {
			owner = owner[idx+1:]
		}
		return legacySanitizeRepoIDForTest(owner, repo)
	}

	return "", os.ErrInvalid
}

func legacySanitizeRepoIDForTest(owner, repo string) (RepoID, error) {
	safeOwner := legacyRepoIDSafePattern.ReplaceAllString(owner, "")
	safeRepo := legacyRepoIDSafePattern.ReplaceAllString(repo, "")
	if safeOwner == "" || safeRepo == "" {
		return "", os.ErrInvalid
	}
	return RepoID(safeOwner + "-" + safeRepo), nil
}

var (
	legacyRepoIDSshPattern  = regexp.MustCompile(`^git@[^:]+:([^/]+)/([^/]+?)(?:\.git)?$`)
	legacyRepoIDURLPattern  = regexp.MustCompile(`^(?:https?|git|ssh)://[^/]+/([^/]+)/([^/]+?)(?:\.git)?/?$`)
	legacyRepoIDSafePattern = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
)

func repoRootForTest(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}
