package model

import (
	"crypto/sha256"
	"encoding/hex"
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

// ADR-0001 grammar partition: 2-6 lowercase hex chars are run short IDs,
// 7-64 are issue hex refs. Both patterns are anchored to keep the two
// namespaces disjoint at the syntax level.
var (
	issueHexRefPattern    = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
	hexLikeIssueIDPattern = regexp.MustCompile(`^[0-9a-f]{2,64}$`)
)

// IssueHexID returns the derived hex ID of an issue (ADR-0001): the
// lowercase hex sha256 of the issue ID string. It is never stored — any
// party holding the issue ID can compute it.
func IssueHexID(issueID IssueID) string {
	h := sha256.Sum256([]byte(issueID))
	return hex.EncodeToString(h[:])
}

// IssueShortHexID returns the 8-char display form of the issue hex ID.
func IssueShortHexID(issueID IssueID) string {
	return IssueHexID(issueID)[:8]
}

// IsIssueHexRef reports whether s is syntactically an issue hex reference
// (unique-prefix lookups accept 7 to 64 hex chars).
func IsIssueHexRef(s string) bool {
	return issueHexRefPattern.MatchString(s)
}

// IsHexLikeIssueID reports whether id would be shadowed by the run short ID
// grammar or collide with issue hex refs, and must therefore be rejected as
// a new issue ID (ADR-0001 creation guard).
func IsHexLikeIssueID(id string) bool {
	return hexLikeIssueIDPattern.MatchString(id)
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
