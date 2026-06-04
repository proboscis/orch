package model

import (
	"strings"
	"testing"
)

func TestRenderFrontmatterIncludesBaseBranch(t *testing.T) {
	iss := &Issue{
		ID:         "demo",
		Title:      "Demo issue",
		Summary:    "short",
		Status:     IssueStatusOpen,
		BaseBranch: "develop",
		Tags:       []string{"a", "b"},
	}
	fm := iss.RenderFrontmatter()

	for _, want := range []string{
		"---\n",
		"type: issue\n",
		"id: demo\n",
		"title: Demo issue\n",
		"summary: short\n",
		"status: open\n",
		"base_branch: develop\n",
		"tags: [a, b]\n",
	} {
		if !strings.Contains(fm, want) {
			t.Errorf("frontmatter missing %q\n--- got ---\n%s", want, fm)
		}
	}
	if !strings.HasPrefix(fm, "---\n") || !strings.HasSuffix(fm, "---\n") {
		t.Errorf("frontmatter not fenced by ---: %q", fm)
	}
}

func TestRenderFrontmatterOmitsEmptyOptionalFields(t *testing.T) {
	iss := &Issue{ID: "demo", Title: "T"}
	fm := iss.RenderFrontmatter()

	if strings.Contains(fm, "base_branch:") {
		t.Errorf("base_branch should be omitted when empty: %q", fm)
	}
	if strings.Contains(fm, "summary:") {
		t.Errorf("summary should be omitted when empty: %q", fm)
	}
	if strings.Contains(fm, "tags:") {
		t.Errorf("tags should be omitted when empty: %q", fm)
	}
	// Status defaults to open when unset.
	if !strings.Contains(fm, "status: open\n") {
		t.Errorf("status should default to open: %q", fm)
	}
}
