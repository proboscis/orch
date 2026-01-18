package git

import "testing"

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		url       string
		wantOwner string
		wantRepo  string
		wantID    string
	}{
		{"git@github.com:proboscis/orch.git", "proboscis", "orch", "proboscis-orch"},
		{"git@github.com:proboscis/orch", "proboscis", "orch", "proboscis-orch"},
		{"https://github.com/proboscis/orch.git", "proboscis", "orch", "proboscis-orch"},
		{"https://github.com/proboscis/orch", "proboscis", "orch", "proboscis-orch"},
		{"git@gitlab.com:user/repo.git", "user", "repo", "user-repo"},
		{"https://gitlab.com/user/repo", "user", "repo", "user-repo"},
		{"invalid-url", "", "", ""},
		{"", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			info := ParseRemoteURL(tt.url)
			if info.Owner != tt.wantOwner {
				t.Errorf("Owner = %q, want %q", info.Owner, tt.wantOwner)
			}
			if info.Repo != tt.wantRepo {
				t.Errorf("Repo = %q, want %q", info.Repo, tt.wantRepo)
			}
			if info.ID() != tt.wantID {
				t.Errorf("ID() = %q, want %q", info.ID(), tt.wantID)
			}
		})
	}
}
