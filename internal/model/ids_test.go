package model

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
	"testing/quick"
)

func TestNewRepoIDNormalizesRemoteForms(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  RepoID
	}{
		{
			name:  "https remote",
			input: "https://github.com/openai/orch.git",
			want:  "openai-orch",
		},
		{
			name:  "ssh scp remote",
			input: "git@github.com:openai/orch.git",
			want:  "openai-orch",
		},
		{
			name:  "ssh url remote",
			input: "ssh://git@github.com/openai/orch.git",
			want:  "openai-orch",
		},
		{
			name:  "owner repo path",
			input: "openai/orch",
			want:  "openai-orch",
		},
		{
			name:  "filesystem path",
			input: "/tmp/openai/orch.git",
			want:  "openai-orch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewRepoID(tt.input)
			if err != nil {
				t.Fatalf("NewRepoID(%q) returned error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("NewRepoID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewRepoIDPreservesLegacyOnDiskNormalization(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  RepoID
	}{
		{
			name:  "dotted owner and repo",
			input: "git@github.com:my.org/my.repo.git",
			want:  "myorg-myrepo",
		},
		{
			name:  "apostrophe",
			input: "https://github.com/O'Brien/repo.git",
			want:  "OBrien-repo",
		},
		{
			name:  "space",
			input: "https://github.com/owner/repo bad.git",
			want:  "owner-repobad",
		},
		{
			name:  "unicode",
			input: "https://github.com/ownér/répo.git",
			want:  "ownr-rpo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewRepoID(tt.input)
			if err != nil {
				t.Fatalf("NewRepoID(%q) returned error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("NewRepoID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRepoIDRoundTripProperty(t *testing.T) {
	prop := func(ownerRaw, repoRaw string) bool {
		owner := noisyRepoPart("owner", ownerRaw)
		repo := noisyRepoPart("repo", repoRaw)

		id, err := NewRepoID("https://github.com/" + owner + "/" + repo + ".git")
		if err != nil {
			return false
		}
		if strings.ContainsAny(id.String(), `/\`) {
			return false
		}

		roundTripped, err := NewNormalizedRepoID(id.String())
		return err == nil && roundTripped == id && roundTripped.String() == id.String()
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatal(err)
	}
}

func TestRepoIDNormalizationIsIdempotent(t *testing.T) {
	first, err := NewRepoID("https://github.com/my.org/my repo.git")
	if err != nil {
		t.Fatalf("NewRepoID() returned error: %v", err)
	}
	second, err := NewNormalizedRepoID(first.String())
	if err != nil {
		t.Fatalf("NewNormalizedRepoID(first) returned error: %v", err)
	}
	third, err := NewNormalizedRepoID(second.String())
	if err != nil {
		t.Fatalf("NewNormalizedRepoID(second) returned error: %v", err)
	}
	if first != second || second != third {
		t.Fatalf("normalization is not idempotent: first=%q second=%q third=%q", first, second, third)
	}
}

func TestNewRepoIDRejectsDegenerateComponents(t *testing.T) {
	tests := []string{
		"https://github.com/owner/.git",
		"https://github.com/./repo.git",
		"https://github.com/owner/!!!.git",
		"owner/.git",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if got, err := NewRepoID(input); err == nil {
				t.Fatalf("NewRepoID(%q) = %q, want error", input, got)
			}
		})
	}
}

func TestNewNormalizedRepoIDRejectsUnnormalizedValues(t *testing.T) {
	tests := []string{
		"",
		"openai",
		"openai/orch",
		`openai\orch`,
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if got, err := NewNormalizedRepoID(input); err == nil {
				t.Fatalf("NewNormalizedRepoID(%q) = %q, want error", input, got)
			}
		})
	}
}

func TestRepoIDRejectsRawStringAtCompileTime(t *testing.T) {
	fset := token.NewFileSet()
	idsFile, err := parser.ParseFile(fset, "ids.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse ids.go: %v", err)
	}

	const source = `package model

func compileCheckNeedsRepoID(id RepoID) {}

func compileCheckRawURL() {
	rawURL := "https://github.com/openai/orch.git"
	compileCheckNeedsRepoID(rawURL)
}
`
	compileCheckFile, err := parser.ParseFile(fset, "repo_id_compilecheck.go", source, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse compile check source: %v", err)
	}

	var typeErrors []error
	config := types.Config{
		Importer: importer.Default(),
		Error: func(err error) {
			typeErrors = append(typeErrors, err)
		},
	}
	_, _ = config.Check("github.com/s22625/orch/internal/model", fset, []*ast.File{idsFile, compileCheckFile}, nil)

	if len(typeErrors) == 0 {
		t.Fatal("expected raw string to be rejected as RepoID, got no type errors")
	}

	foundRepoIDTypeError := false
	for _, err := range typeErrors {
		msg := err.Error()
		if strings.Contains(msg, "(variable of type string)") && strings.Contains(msg, "as RepoID value") {
			foundRepoIDTypeError = true
			continue
		}
		t.Fatalf("unexpected type error: %v", err)
	}

	if !foundRepoIDTypeError {
		t.Fatalf("expected raw string to be rejected as RepoID, got errors: %v", typeErrors)
	}
}

func noisyRepoPart(prefix string, value string) string {
	var b strings.Builder
	b.WriteString(prefix)
	for _, r := range value {
		if r == '/' || r == '\\' {
			continue
		}
		b.WriteRune(r)
		if b.Len() >= 24 {
			break
		}
	}
	if b.String() == prefix {
		b.WriteString(" bad.name")
	}
	return b.String()
}
