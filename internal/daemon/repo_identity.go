package daemon

import (
	"path/filepath"
	"strings"

	"github.com/s22625/orch/internal/xdg"
)

const repoIDTokenPrefix = "repoid:"

func derivePortableRepoID(projectRoot string) string {
	repoID, err := xdg.RepoIDStrict(projectRoot)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(repoID)
}

func repoIDFromProjectSelector(value string) string {
	target := strings.TrimSpace(value)
	if target == "" {
		return ""
	}
	if repoID, ok := decodeRepoIDToken(target); ok {
		return repoID
	}
	if repoID := derivePortableRepoID(target); repoID != "" {
		return repoID
	}
	if looksLikeRepoURL(target) {
		repoID, err := xdg.ParseRepoID(target)
		if err == nil {
			return strings.TrimSpace(repoID)
		}
		return ""
	}
	if looksLikeFilesystemPath(target) {
		return ""
	}
	return target
}

func looksLikeFilesystemPath(value string) bool {
	target := strings.TrimSpace(value)
	if target == "" {
		return false
	}
	if filepath.IsAbs(target) {
		return true
	}
	if target == "." || target == ".." {
		return true
	}
	return strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../") || strings.Contains(target, "/") || strings.Contains(target, "\\")
}

func encodeRepoIDToken(repoID string) string {
	id := strings.TrimSpace(repoID)
	if id == "" {
		return ""
	}
	return repoIDTokenPrefix + id
}

func decodeRepoIDToken(value string) (string, bool) {
	v := strings.TrimSpace(value)
	if !strings.HasPrefix(v, repoIDTokenPrefix) {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimPrefix(v, repoIDTokenPrefix))
	if id == "" {
		return "", false
	}
	return id, true
}
