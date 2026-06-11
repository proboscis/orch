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

var (
	repoIDSshPattern        = regexp.MustCompile(`^git@[^:]+:([^/]+)/([^/]+?)(?:\.git)?$`)
	repoIDURLPattern        = regexp.MustCompile(`^(?:https?|git|ssh)://[^/]+/([^/]+)/([^/]+?)(?:\.git)?/?$`)
	repoIDSafePattern       = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	normalizedRepoIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+-[a-zA-Z0-9_-]+$`)
)

// NewRepoID normalizes a git remote, repo path, or already-normalized repo id
// into the portable owner-repo identifier used by daemon routing.
func NewRepoID(input string) (RepoID, error) {
	target := strings.TrimSpace(input)
	if target == "" {
		return "", fmt.Errorf("empty repo identity")
	}

	if normalizedRepoIDPattern.MatchString(target) && !strings.Contains(target, "://") {
		return RepoID(target), nil
	}

	if matches := repoIDSshPattern.FindStringSubmatch(target); len(matches) == 3 {
		return newSanitizedRepoID(matches[1], matches[2], input)
	}

	if matches := repoIDURLPattern.FindStringSubmatch(target); len(matches) == 3 {
		return newSanitizedRepoID(matches[1], matches[2], input)
	}

	cleaned := strings.TrimSuffix(target, ".git")
	parts := strings.Split(cleaned, "/")
	if len(parts) >= 2 {
		owner := parts[len(parts)-2]
		repo := parts[len(parts)-1]
		if idx := strings.LastIndex(owner, ":"); idx != -1 {
			owner = owner[idx+1:]
		}
		return newSanitizedRepoID(owner, repo, input)
	}

	return "", fmt.Errorf("unable to parse repo identity: %s", input)
}

// NewProjectID normalizes a project identity using the same byte-compatible
// repo-id normalization as NewRepoID while keeping a distinct domain type.
func NewProjectID(input string) (ProjectID, error) {
	repoID, err := NewRepoID(input)
	if err != nil {
		return "", err
	}
	return ProjectID(repoID.String()), nil
}

func newSanitizedRepoID(owner, repo, input string) (RepoID, error) {
	id := sanitizeRepoID(owner, repo)
	if id == "" {
		return "", fmt.Errorf("unable to parse repo identity: %s", input)
	}
	return id, nil
}

func sanitizeRepoID(owner, repo string) RepoID {
	safeOwner := repoIDSafePattern.ReplaceAllString(owner, "")
	safeRepo := repoIDSafePattern.ReplaceAllString(repo, "")
	if safeOwner == "" || safeRepo == "" {
		return ""
	}
	return RepoID(safeOwner + "-" + safeRepo)
}
