package monitor

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/s22625/orch/internal/model"
)

// Styles groups lipgloss styles for the dashboard.
type Styles struct {
	Box      lipgloss.Style
	Title    lipgloss.Style
	Header   lipgloss.Style
	Text     lipgloss.Style
	Selected lipgloss.Style
	Faint    lipgloss.Style
	Status   map[model.Status]lipgloss.Style
	Alive    map[string]lipgloss.Style
	PRState  map[string]lipgloss.Style
}

// DefaultStyles returns the standard dashboard styles.
func DefaultStyles() Styles {
	return Styles{
		Box:      lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1),
		Title:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")),
		Header:   lipgloss.NewStyle().Bold(true),
		Text:     lipgloss.NewStyle(),
		Selected: lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("237")),
		Faint:    lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		Status: map[model.Status]lipgloss.Style{
			model.StatusRunning:    lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
			model.StatusBlocked:    lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
			model.StatusBlockedAPI: lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
			model.StatusBooting:    lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
			model.StatusQueued:     lipgloss.NewStyle().Foreground(lipgloss.Color("7")),
			model.StatusPROpen:     lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
			model.StatusDone:       lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
			model.StatusFailed:     lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
			model.StatusCanceled:   lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
			model.StatusUnknown:    lipgloss.NewStyle().Foreground(lipgloss.Color("5")),
		},
		Alive: map[string]lipgloss.Style{
			"yes": lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
			"no":  lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
			"-":   lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		},
		PRState: map[string]lipgloss.Style{
			"open":   lipgloss.NewStyle().Foreground(lipgloss.Color("2")), // green - PR is open
			"merged": lipgloss.NewStyle().Foreground(lipgloss.Color("5")), // magenta - PR was merged
			"closed": lipgloss.NewStyle().Foreground(lipgloss.Color("1")), // red - PR was closed without merge
		},
	}
}
