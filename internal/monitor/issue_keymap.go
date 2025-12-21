package monitor

import "fmt"

// IssueKeyMap defines shortcuts for the issues dashboard.
type IssueKeyMap struct {
	Runs     string
	Issues   string
	Chat     string
	StartRun string
	Open     string
	NewIssue string
	Refresh  string
	Quit     string
	Help     string
}

// DefaultIssueKeyMap returns the default issue dashboard shortcut mapping.
func DefaultIssueKeyMap() IssueKeyMap {
	return IssueKeyMap{
		Runs:     "g",
		Issues:   "i",
		Chat:     "c",
		StartRun: "enter",
		Open:     "o",
		NewIssue: "n",
		Refresh:  "r",
		Quit:     "q",
		Help:     "?",
	}
}

// HelpLine renders the footer help text.
func (k IssueKeyMap) HelpLine() string {
	return fmt.Sprintf("[%s] runs  [%s] issues  [%s] chat  [%s] start  [%s] open  [%s] new  [%s] refresh  [%s] quit  [%s] help",
		k.Runs, k.Issues, k.Chat, k.StartRun, k.Open, k.NewIssue, k.Refresh, k.Quit, k.Help)
}
