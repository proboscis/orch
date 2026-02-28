package daemon

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/s22625/orch/internal/xdg"
)

const repoIDTokenPrefix = "repoid:"

func derivePortableRepoID(projectRoot string) string {
	repoID, err := xdg.RepoID(projectRoot)
	if err != nil || repoID == "" {
		cleaned := filepath.Clean(projectRoot)
		h := sha256.Sum256([]byte(cleaned))
		return fmt.Sprintf("repo-%x", h[:4])
	}
	return repoID
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
