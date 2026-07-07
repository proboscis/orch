package daemon

import (
	"path/filepath"
	"strings"

	"github.com/proboscis/orch/internal/xdg"
)

func derivePortableRepoID(projectRoot string) string {
	repoID, err := xdg.RepoIDStrict(projectRoot)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(repoID))
}

func repoIDFromProjectSelector(value string) string {
	target := strings.TrimSpace(value)
	if target == "" {
		return ""
	}
	if repoID := derivePortableRepoID(target); repoID != "" {
		return repoID
	}
	if looksLikeRepoURL(target) {
		repoID, err := xdg.ParseRepoID(target)
		if err == nil {
			return strings.TrimSpace(string(repoID))
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
