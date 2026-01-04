package monitor

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// UISettings holds persistent UI settings for the monitor.
type UISettings struct {
	RunSort        SortKey  `yaml:"run_sort,omitempty"`
	IssueSort      SortKey  `yaml:"issue_sort,omitempty"`
	ShowResolved   bool     `yaml:"show_resolved"`
	ShowClosed     bool     `yaml:"show_closed"`
	FavoriteAgents []string `yaml:"favorite_agents,omitempty"`
}

const uiSettingsFile = "monitor-settings.yaml"

// DefaultUISettings returns the default UI settings.
func DefaultUISettings() *UISettings {
	return &UISettings{
		RunSort:      SortByUpdated,
		IssueSort:    SortByName,
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
	}
	if IsValidSortKey(loaded.IssueSort) {
		settings.IssueSort = loaded.IssueSort
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

// GetOrchDir returns the .orch directory path from a vault path.
func GetOrchDir(vaultPath string) string {
	if vaultPath == "" {
		return ""
	}
	// The vault path is typically .orch/vault, so we get the parent
	return filepath.Dir(vaultPath)
}
