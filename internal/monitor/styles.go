package monitor

import (
	"github.com/charmbracelet/lipgloss"
)

// Color palette
var (
	colorGreen   = lipgloss.Color("42")
	colorYellow  = lipgloss.Color("214")
	colorRed     = lipgloss.Color("196")
	colorBlue    = lipgloss.Color("39")
	colorCyan    = lipgloss.Color("45")
	colorGray    = lipgloss.Color("245")
	colorMagenta = lipgloss.Color("165")
	colorWhite   = lipgloss.Color("255")
	colorBorder  = lipgloss.Color("240")
)

// Styles defines the visual styles for the monitor dashboard
type Styles struct {
	// Box styles
	TitleBox    lipgloss.Style
	ContentBox  lipgloss.Style
	StatusBar   lipgloss.Style
	HelpBar     lipgloss.Style
	FullBox     lipgloss.Style

	// Text styles
	Title       lipgloss.Style
	Header      lipgloss.Style
	Normal      lipgloss.Style
	Muted       lipgloss.Style
	Selected    lipgloss.Style

	// Status colors
	StatusRunning  lipgloss.Style
	StatusBlocked  lipgloss.Style
	StatusFailed   lipgloss.Style
	StatusDone     lipgloss.Style
	StatusPROpen   lipgloss.Style
	StatusQueued   lipgloss.Style
	StatusBooting  lipgloss.Style
	StatusCanceled lipgloss.Style
	StatusUnknown  lipgloss.Style

	// Status indicators
	IndicatorRunning  string
	IndicatorBlocked  string
	IndicatorDone     string
	IndicatorFailed   string

	// Table column widths
	ColIndex   int
	ColID      int
	ColIssue   int
	ColStatus  int
	ColAgo     int
	ColSummary int
}

// DefaultStyles returns the default style configuration
func DefaultStyles() Styles {
	return Styles{
		TitleBox: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite).
			Padding(0, 1),

		ContentBox: lipgloss.NewStyle().
			Padding(0, 1),

		StatusBar: lipgloss.NewStyle().
			Foreground(colorGray).
			Padding(0, 1),

		HelpBar: lipgloss.NewStyle().
			Foreground(colorGray).
			Padding(0, 1),

		FullBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder),

		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite),

		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorGray),

		Normal: lipgloss.NewStyle().
			Foreground(colorWhite),

		Muted: lipgloss.NewStyle().
			Foreground(colorGray),

		Selected: lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color("236")).
			Foreground(colorWhite),

		StatusRunning: lipgloss.NewStyle().
			Foreground(colorGreen),

		StatusBlocked: lipgloss.NewStyle().
			Foreground(colorYellow),

		StatusFailed: lipgloss.NewStyle().
			Foreground(colorRed),

		StatusDone: lipgloss.NewStyle().
			Foreground(colorBlue),

		StatusPROpen: lipgloss.NewStyle().
			Foreground(colorCyan),

		StatusQueued: lipgloss.NewStyle().
			Foreground(colorGray),

		StatusBooting: lipgloss.NewStyle().
			Foreground(colorGreen),

		StatusCanceled: lipgloss.NewStyle().
			Foreground(colorGray),

		StatusUnknown: lipgloss.NewStyle().
			Foreground(colorMagenta),

		IndicatorRunning: "●",
		IndicatorBlocked: "◐",
		IndicatorDone:    "✓",
		IndicatorFailed:  "✗",

		ColIndex:   3,
		ColID:      8,
		ColIssue:   12,
		ColStatus:  10,
		ColAgo:     6,
		ColSummary: 30,
	}
}

// StyleStatus returns styled status text
func (s Styles) StyleStatus(status string) string {
	switch status {
	case "running":
		return s.StatusRunning.Render(status)
	case "blocked":
		return s.StatusBlocked.Render(status)
	case "failed":
		return s.StatusFailed.Render(status)
	case "done":
		return s.StatusDone.Render(status)
	case "pr_open":
		return s.StatusPROpen.Render(status)
	case "queued":
		return s.StatusQueued.Render(status)
	case "booting":
		return s.StatusBooting.Render(status)
	case "canceled":
		return s.StatusCanceled.Render(status)
	case "unknown":
		return s.StatusUnknown.Render(status)
	default:
		return s.Normal.Render(status)
	}
}

// StatusSummary returns a styled summary of status counts
func (s Styles) StatusSummary(running, blocked, done, failed int) string {
	parts := []string{}

	if running > 0 {
		parts = append(parts, s.StatusRunning.Render(s.IndicatorRunning+" running: "+itoa(running)))
	}
	if blocked > 0 {
		parts = append(parts, s.StatusBlocked.Render(s.IndicatorBlocked+" blocked: "+itoa(blocked)))
	}
	if done > 0 {
		parts = append(parts, s.StatusDone.Render(s.IndicatorDone+" done: "+itoa(done)))
	}
	if failed > 0 {
		parts = append(parts, s.StatusFailed.Render(s.IndicatorFailed+" failed: "+itoa(failed)))
	}

	if len(parts) == 0 {
		return s.Muted.Render("no runs")
	}

	result := ""
	for i, part := range parts {
		if i > 0 {
			result += "    "
		}
		result += part
	}
	return result
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
