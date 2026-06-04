package model

import (
	"fmt"
	"regexp"
	"strings"
)

type IssueID string
type RunID string
type ShortID string
type RepoID string
type ProjectID string

func (id IssueID) String() string {
	return string(id)
}

func (id RunID) String() string {
	return string(id)
}

func (id ShortID) String() string {
	return string(id)
}

func (id RepoID) String() string {
	return string(id)
}

func (id ProjectID) String() string {
	return string(id)
}

func NewIssueID(id string) IssueID {
	return IssueID(strings.TrimSpace(id))
}

func NewRunID(id string) RunID {
	return RunID(strings.TrimSpace(id))
}

func NewShortID(id string) ShortID {
	return ShortID(strings.TrimSpace(id))
}

// NewRepoID normalizes a git remote URL, SSH remote, or owner/repo-like path
// into orch's stable repo identifier format: owner-repo.
func NewRepoID(value string) (RepoID, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("empty repo ID source")
	}

	// SSH format: git@github.com:owner/repo.git
	sshPattern := regexp.MustCompile(`^git@[^:]+:([^/]+)/([^/]+?)(?:\.git)?$`)
	if matches := sshPattern.FindStringSubmatch(value); len(matches) == 3 {
		return sanitizeRepoID(matches[1], matches[2])
	}

	// HTTPS/Git/SSH URL: https://github.com/owner/repo.git
	urlPattern := regexp.MustCompile(`^(?:https?|git|ssh)://[^/]+/([^/]+)/([^/]+?)(?:\.git)?/?$`)
	if matches := urlPattern.FindStringSubmatch(value); len(matches) == 3 {
		return sanitizeRepoID(matches[1], matches[2])
	}

	cleaned := strings.TrimSuffix(value, ".git")
	parts := strings.Split(cleaned, "/")
	if len(parts) >= 2 {
		owner := parts[len(parts)-2]
		repo := parts[len(parts)-1]
		if idx := strings.LastIndex(owner, ":"); idx != -1 {
			owner = owner[idx+1:]
		}
		return sanitizeRepoID(owner, repo)
	}

	return "", fmt.Errorf("could not parse repo ID from %q", value)
}

func NewNormalizedRepoID(value string) (RepoID, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("empty repo ID")
	}
	if strings.ContainsAny(value, `/\`) {
		return "", fmt.Errorf("repo ID must be normalized, got %q", value)
	}
	if !strings.Contains(value, "-") {
		return "", fmt.Errorf("repo ID must include owner and repo components, got %q", value)
	}
	return RepoID(value), nil
}

func NewProjectID(value string) (ProjectID, error) {
	repoID, err := NewRepoID(value)
	if err != nil {
		return "", err
	}
	return ProjectID(repoID.String()), nil
}

func NewNormalizedProjectID(value string) (ProjectID, error) {
	repoID, err := NewNormalizedRepoID(value)
	if err != nil {
		return "", err
	}
	return ProjectID(repoID.String()), nil
}

func sanitizeRepoID(owner, repo string) (RepoID, error) {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	repo = strings.TrimSuffix(repo, ".git")
	repo = strings.TrimSuffix(repo, "/")

	if owner == "" || repo == "" {
		return "", fmt.Errorf("repo ID requires non-empty owner and repo components")
	}

	// Keep the historical XDG repo-id behavior because this value is the
	// on-disk data directory key under ~/.local/share/orch/{repoID}.
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	owner = re.ReplaceAllString(owner, "")
	repo = re.ReplaceAllString(repo, "")

	if owner == "" || repo == "" {
		return "", fmt.Errorf("repo ID components became empty after normalization")
	}
	return RepoID(owner + "-" + repo), nil
}
