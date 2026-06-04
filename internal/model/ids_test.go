package model

import (
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/quick"
)

type repoIDPart string

func (repoIDPart) Generate(r *rand.Rand, size int) reflect.Value {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
	length := 1 + r.Intn(12)
	buf := make([]byte, length)
	for i := range buf {
		buf[i] = chars[r.Intn(len(chars))]
	}
	return reflect.ValueOf(repoIDPart(buf))
}

func TestNewRepoIDNormalizesEquivalentInputsAndRoundTrips(t *testing.T) {
	property := func(ownerPart repoIDPart, repoPart repoIDPart) bool {
		owner := string(ownerPart)
		repo := string(repoPart)
		want := RepoID(owner + "-" + repo)
		inputs := []string{
			"https://github.com/" + owner + "/" + repo + ".git",
			"https://github.com/" + owner + "/" + repo,
			"git@github.com:" + owner + "/" + repo + ".git",
			"ssh://git@github.com/" + owner + "/" + repo + ".git",
			"/work/repos/" + owner + "/" + repo,
		}

		for _, input := range inputs {
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

func TestNewRepoIDRejectsRawStringAtCompileTime(t *testing.T) {
	repoRoot := repoRootForTest(t)
	tmp := t.TempDir()

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

	badSource := `package compilefail

import "github.com/s22625/orch/internal/model"

func acceptsRepoID(model.RepoID) {}

func bad() {
	rawURL := "https://github.com/acme/repo.git"
	acceptsRepoID(rawURL)
}
`
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
	if !strings.Contains(out, "cannot use rawURL") || !strings.Contains(out, "model.RepoID") {
		t.Fatalf("compile failure did not mention rawURL/model.RepoID; output:\n%s", out)
	}
}

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
