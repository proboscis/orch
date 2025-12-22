package monitor

import "fmt"

// IssueKeyMap defines shortcuts for the issues dashboard.
type IssueKeyMap struct {
	Runs        string
	Issues      string
	Chat        string
	OpenRun     string
	StartRun    string
	ContinueRun string
	Open        string
	Status      string
	Filter      string
	Quit        string
	Help        string
}

// DefaultIssueKeyMap returns the default issue dashboard shortcut mapping.
func DefaultIssueKeyMap() IssueKeyMap {
	return IssueKeyMap{
		Runs:        "g",
		Issues:      "i",
		Chat:        "c",
		OpenRun:     "enter",
		StartRun:    "r",
		ContinueRun: "C",
		Open:        "o",
		Status:      "x",
		Filter:      "f",
		Quit:        "q",
		Help:        "?",
	}
}

// HelpLine renders the footer help text.
func (k IssueKeyMap) HelpLine() string {
	return fmt.Sprintf("[%s] runs  [%s] issues  [%s] chat  [%s] open run  [%s] start run  [%s] continue  [%s] open  [%s] status  [%s] filter  [%s] quit  [%s] help",
		k.Runs, k.Issues, k.Chat, k.OpenRun, k.StartRun, k.ContinueRun, k.Open, k.Status, k.Filter, k.Quit, k.Help)
}
