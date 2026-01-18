package monitor

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// UISettings holds persistent UI settings for the monitor.
type UISettings struct {
	RunSort      SortKey       `yaml:"run_sort,omitempty"`
	RunSortDir   SortDirection `yaml:"run_sort_dir,omitempty"`
	IssueSort    SortKey       `yaml:"issue_sort,omitempty"`
	IssueSortDir SortDirection `yaml:"issue_sort_dir,omitempty"`
	ShowResolved bool          `yaml:"show_resolved"`
	ShowClosed   bool          `yaml:"show_closed"`
}

const uiSettingsFile = "monitor-settings.yaml"

// DefaultUISettings returns the default UI settings.
func DefaultUISettings() *UISettings {
	return &UISettings{
		RunSort:      SortByUpdated,
		RunSortDir:   DefaultSortDirection(SortByUpdated),
		IssueSort:    SortByName,
		IssueSortDir: DefaultSortDirection(SortByName),
		ShowResolved: false,
		ShowClosed:   true,
	}
}

// LoadUISettings loads UI settings from the .orch directory.
// Falls back to defaults if the file doesn't exist.
func LoadUISettings(orchDir string) *UISettings {
	settings := DefaultUISettings()
	if orchDir == "" {
		return settings
	}

	path := filepath.Join(orchDir, uiSettingsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return settings
	}

	var loaded UISettings
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		return settings
	}

	// Merge loaded settings (only override if valid)
	if IsValidSortKey(loaded.RunSort) {
		settings.RunSort = loaded.RunSort
		settings.RunSortDir = loaded.RunSortDir
	}
	if IsValidSortKey(loaded.IssueSort) {
		settings.IssueSort = loaded.IssueSort
		settings.IssueSortDir = loaded.IssueSortDir
	}
	settings.ShowResolved = loaded.ShowResolved
	settings.ShowClosed = loaded.ShowClosed

	return settings
}

// SaveUISettings saves UI settings to the .orch directory.
func SaveUISettings(orchDir string, settings *UISettings) error {
	if orchDir == "" {
		return nil
	}

	// Ensure .orch directory exists
	if err := os.MkdirAll(orchDir, 0755); err != nil {
		return err
	}

	path := filepath.Join(orchDir, uiSettingsFile)
	data, err := yaml.Marshal(settings)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// GetOrchDir returns the .orch directory path from a project root.
func GetOrchDir(projectRoot string) string {
	if projectRoot == "" {
		return ""
	}
	return filepath.Join(projectRoot, ".orch")
}
