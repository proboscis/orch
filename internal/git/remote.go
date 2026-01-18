package git

import (
	"os/exec"
	"regexp"
	"strings"
)

var (
	sshRemoteRegex   = regexp.MustCompile(`^git@[^:]+:([^/]+)/([^/.]+)(?:\.git)?$`)
	httpsRemoteRegex = regexp.MustCompile(`^https?://[^/]+/([^/]+)/([^/.]+)(?:\.git)?$`)
)

type RepoInfo struct {
	Owner string
	Repo  string
}

func (r RepoInfo) ID() string {
	if r.Owner == "" && r.Repo == "" {
		return ""
	}
	if r.Owner == "" {
		return r.Repo
	}
	return r.Owner + "-" + r.Repo
}

func GetRepoInfo(repoRoot string) (*RepoInfo, error) {
	if repoRoot == "" {
		var err error
		repoRoot, err = FindRepoRoot("")
		if err != nil {
			return nil, err
		}
	}

	cmd := exec.Command("git", "-C", repoRoot, "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return ParseRemoteURL(strings.TrimSpace(string(output))), nil
}

func ParseRemoteURL(url string) *RepoInfo {
	if matches := sshRemoteRegex.FindStringSubmatch(url); len(matches) == 3 {
		return &RepoInfo{Owner: matches[1], Repo: matches[2]}
	}

	if matches := httpsRemoteRegex.FindStringSubmatch(url); len(matches) == 3 {
		return &RepoInfo{Owner: matches[1], Repo: matches[2]}
	}

	return &RepoInfo{}
}
