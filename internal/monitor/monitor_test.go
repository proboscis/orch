package monitor

import (
	"strings"
	"testing"
)

func TestSessionNameForVault(t *testing.T) {
	tests := []struct {
		name      string
		vaultPath string
		wantStart string
		wantLen   int // approximate expected length
	}{
		{
			name:      "empty path returns default",
			vaultPath: "",
			wantStart: defaultSessionName,
			wantLen:   len(defaultSessionName),
		},
		{
			name:      "simple path",
			vaultPath: "/home/user/projects/myproject",
			wantStart: "orch-myproject-",
			wantLen:   len("orch-myproject-") + 6,
		},
		{
			name:      "path with dots replaced",
			vaultPath: "/home/user/.vault",
			wantStart: "orch--vault-",
			wantLen:   len("orch--vault-") + 6,
		},
		{
			name:      "path with spaces replaced",
			vaultPath: "/home/user/my project",
			wantStart: "orch-my-project-",
			wantLen:   len("orch-my-project-") + 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sessionNameForVault(tt.vaultPath)

			if tt.vaultPath == "" {
				if result != defaultSessionName {
					t.Errorf("sessionNameForVault(%q) = %q, want %q", tt.vaultPath, result, defaultSessionName)
				}
				return
			}

			if !strings.HasPrefix(result, tt.wantStart) {
				t.Errorf("sessionNameForVault(%q) = %q, want prefix %q", tt.vaultPath, result, tt.wantStart)
			}

			// Check that it has a 6-char hash suffix
			parts := strings.Split(result, "-")
			if len(parts) < 2 {
				t.Errorf("sessionNameForVault(%q) = %q, expected at least 2 parts separated by -", tt.vaultPath, result)
				return
			}
			hash := parts[len(parts)-1]
			if len(hash) != 6 {
				t.Errorf("sessionNameForVault(%q) hash suffix = %q (len %d), want len 6", tt.vaultPath, hash, len(hash))
			}
		})
	}
}

func TestSessionNameForVaultConsistency(t *testing.T) {
	// Same path should always produce same session name
	path := "/home/user/projects/test"
	result1 := sessionNameForVault(path)
	result2 := sessionNameForVault(path)

	if result1 != result2 {
		t.Errorf("sessionNameForVault produced inconsistent results: %q vs %q", result1, result2)
	}
}

func TestSessionNameForVaultUniqueness(t *testing.T) {
	// Different paths should produce different session names
	path1 := "/home/user/project1"
	path2 := "/home/user/project2"

	result1 := sessionNameForVault(path1)
	result2 := sessionNameForVault(path2)

	if result1 == result2 {
		t.Errorf("different paths produced same session name: %q", result1)
	}
}
