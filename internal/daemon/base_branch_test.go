package daemon

import (
	"testing"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/model"
)

func TestResolveBaseBranch(t *testing.T) {
	issueWith := &model.Issue{ID: "i", BaseBranch: "develop"}
	issueWithout := &model.Issue{ID: "i"}
	cfgWith := &config.Config{BaseBranch: "trunk"}
	cfgEmpty := &config.Config{}

	tests := []struct {
		name     string
		explicit string
		issue    *model.Issue
		cfg      *config.Config
		want     string
	}{
		{"explicit flag wins", "feature", issueWith, cfgWith, "feature"},
		{"issue level over config", "", issueWith, cfgWith, "develop"},
		{"config when issue empty", "", issueWithout, cfgWith, "trunk"},
		{"main as final fallback", "", issueWithout, cfgEmpty, "main"},
		{"nil issue falls through to config", "", nil, cfgWith, "trunk"},
		{"whitespace explicit ignored", "   ", issueWith, cfgWith, "develop"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveBaseBranch(tc.explicit, tc.issue, tc.cfg); got != tc.want {
				t.Fatalf("resolveBaseBranch(%q, issue=%v, cfg=%v) = %q, want %q",
					tc.explicit, tc.issue, tc.cfg, got, tc.want)
			}
		})
	}
}

func TestResolvePRTargetBranch(t *testing.T) {
	tests := []struct {
		name       string
		explicit   string
		baseBranch string
		want       string
	}{
		{"explicit wins", "release", "develop", "release"},
		{"follows base branch", "", "develop", "develop"},
		{"strips remote prefix from base", "", "origin/develop", "develop"},
		{"main when nothing set", "", "", "main"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvePRTargetBranch(tc.explicit, tc.baseBranch); got != tc.want {
				t.Fatalf("resolvePRTargetBranch(%q, %q) = %q, want %q",
					tc.explicit, tc.baseBranch, got, tc.want)
			}
		})
	}
}
