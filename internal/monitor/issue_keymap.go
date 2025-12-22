package monitor

import "fmt"

// IssueKeyMap defines shortcuts for the issues dashboard.
type IssueKeyMap struct {
	Runs     string
	Issues   string
	Chat     string
	OpenRun  string
	StartRun string
	Open     string
	Resolve  string
	Filter   string
	Quit     string
	Help     string
}

// DefaultIssueKeyMap returns the default issue dashboard shortcut mapping.
func DefaultIssueKeyMap() IssueKeyMap {
	return IssueKeyMap{
		Runs:     "g",
		Issues:   "i",
		Chat:     "c",
		OpenRun:  "enter",
		StartRun: "r",
		Open:     "o",
		Resolve:  "x",
		Filter:   "f",
		Quit:     "q",
		Help:     "?",
	}
}

// HelpLine renders the footer help text.
func (k IssueKeyMap) HelpLine() string {
	return fmt.Sprintf("[%s] runs  [%s] issues  [%s] chat  [%s] open run  [%s] start run  [%s] open  [%s] resolve  [%s] filter  [%s] quit  [%s] help",
		k.Runs, k.Issues, k.Chat, k.OpenRun, k.StartRun, k.Open, k.Resolve, k.Filter, k.Quit, k.Help)
}
