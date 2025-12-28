package monitor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultUISettings(t *testing.T) {
	settings := DefaultUISettings()
	if settings.RunSort != SortByUpdated {
		t.Errorf("expected RunSort=%v, got %v", SortByUpdated, settings.RunSort)
	}
	if settings.IssueSort != SortByName {
		t.Errorf("expected IssueSort=%v, got %v", SortByName, settings.IssueSort)
	}
	if settings.ShowResolved != false {
		t.Errorf("expected ShowResolved=false, got %v", settings.ShowResolved)
	}
	if settings.ShowClosed != true {
		t.Errorf("expected ShowClosed=true, got %v", settings.ShowClosed)
	}
}

func TestLoadUISettings_NoFile(t *testing.T) {
	dir := t.TempDir()
	settings := LoadUISettings(dir)

	// Should return defaults when file doesn't exist
	if settings.RunSort != SortByUpdated {
		t.Errorf("expected RunSort=%v, got %v", SortByUpdated, settings.RunSort)
	}
	if settings.IssueSort != SortByName {
		t.Errorf("expected IssueSort=%v, got %v", SortByName, settings.IssueSort)
	}
}

func TestLoadUISettings_EmptyDir(t *testing.T) {
	settings := LoadUISettings("")

	// Should return defaults when dir is empty
	if settings.RunSort != SortByUpdated {
		t.Errorf("expected RunSort=%v, got %v", SortByUpdated, settings.RunSort)
	}
}

func TestSaveAndLoadUISettings(t *testing.T) {
	dir := t.TempDir()

	// Save custom settings
	original := &UISettings{
		RunSort:      SortByStatus,
		IssueSort:    SortByUpdated,
		ShowResolved: true,
		ShowClosed:   false,
	}
	if err := SaveUISettings(dir, original); err != nil {
		t.Fatalf("SaveUISettings failed: %v", err)
	}

	// Verify file exists
	path := filepath.Join(dir, uiSettingsFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("settings file was not created at %s", path)
	}

	// Load and verify
	loaded := LoadUISettings(dir)
	if loaded.RunSort != original.RunSort {
		t.Errorf("expected RunSort=%v, got %v", original.RunSort, loaded.RunSort)
	}
	if loaded.IssueSort != original.IssueSort {
		t.Errorf("expected IssueSort=%v, got %v", original.IssueSort, loaded.IssueSort)
	}
	if loaded.ShowResolved != original.ShowResolved {
		t.Errorf("expected ShowResolved=%v, got %v", original.ShowResolved, loaded.ShowResolved)
	}
	if loaded.ShowClosed != original.ShowClosed {
		t.Errorf("expected ShowClosed=%v, got %v", original.ShowClosed, loaded.ShowClosed)
	}
}

func TestSaveUISettings_CreatesDir(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "subdir", ".orch")

	settings := &UISettings{
		RunSort:      SortByName,
		IssueSort:    SortByStatus,
		ShowResolved: false,
		ShowClosed:   true,
	}

	if err := SaveUISettings(dir, settings); err != nil {
		t.Fatalf("SaveUISettings failed: %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("directory was not created at %s", dir)
	}

	// Verify file was created
	path := filepath.Join(dir, uiSettingsFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("settings file was not created at %s", path)
	}
}

func TestSaveUISettings_EmptyDir(t *testing.T) {
	settings := &UISettings{
		RunSort:   SortByName,
		IssueSort: SortByStatus,
	}

	// Should not error with empty dir
	if err := SaveUISettings("", settings); err != nil {
		t.Errorf("SaveUISettings with empty dir should not error, got: %v", err)
	}
}

func TestLoadUISettings_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, uiSettingsFile)

	// Write invalid YAML
	if err := os.WriteFile(path, []byte("invalid: yaml: content:"), 0644); err != nil {
		t.Fatalf("failed to write invalid yaml: %v", err)
	}

	// Should return defaults on parse error
	settings := LoadUISettings(dir)
	if settings.RunSort != SortByUpdated {
		t.Errorf("expected default RunSort on parse error, got %v", settings.RunSort)
	}
}

func TestLoadUISettings_InvalidSortKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, uiSettingsFile)

	// Write YAML with invalid sort key
	content := `run_sort: invalid_key
issue_sort: name
show_resolved: true
show_closed: false
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write yaml: %v", err)
	}

	// Should use default for invalid key but preserve valid values
	settings := LoadUISettings(dir)
	if settings.RunSort != SortByUpdated {
		t.Errorf("expected default RunSort for invalid key, got %v", settings.RunSort)
	}
	if settings.IssueSort != SortByName {
		t.Errorf("expected IssueSort=name, got %v", settings.IssueSort)
	}
	if settings.ShowResolved != true {
		t.Errorf("expected ShowResolved=true, got %v", settings.ShowResolved)
	}
	if settings.ShowClosed != false {
		t.Errorf("expected ShowClosed=false, got %v", settings.ShowClosed)
	}
}

func TestGetOrchDir(t *testing.T) {
	tests := []struct {
		name      string
		vaultPath string
		expected  string
	}{
		{
			name:      "empty path",
			vaultPath: "",
			expected:  "",
		},
		{
			name:      "typical vault path",
			vaultPath: "/project/.orch/vault",
			expected:  "/project/.orch",
		},
		{
			name:      "nested path",
			vaultPath: "/home/user/projects/myapp/.orch/vault",
			expected:  "/home/user/projects/myapp/.orch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetOrchDir(tt.vaultPath)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
