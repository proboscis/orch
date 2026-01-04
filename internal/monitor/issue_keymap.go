package monitor

import "fmt"

// IssueKeyMap defines shortcuts for the issues dashboard.
type IssueKeyMap struct {
	Runs        string
	Issues      string
	Chat        string
	EditIssue   string
	OpenRun     string
	StartRun    string
	ContinueRun string
	Open        string
	Resolve     string
	Filter      string
	Sort        string
	Attach      string
	Quit        string
	Help        string
}

// DefaultIssueKeyMap returns the default issue dashboard shortcut mapping.
func DefaultIssueKeyMap() IssueKeyMap {
	return IssueKeyMap{
		Runs:        "g",
		Issues:      "i",
		Chat:        "c",
		EditIssue:   "enter",
		OpenRun:     "O",
		StartRun:    "r",
		ContinueRun: "C",
		Open:        "o",
		Resolve:     "x",
		Filter:      "f",
		Sort:        "S",
		Attach:      "a",
		Quit:        "q",
		Help:        "?",
	}
}

// HelpLine renders the footer help text.
func (k IssueKeyMap) HelpLine() string {
	return fmt.Sprintf("[%s] edit  [%s] runs  [%s] issues  [%s] chat  [%s] open run  [%s] start run  [%s] continue  [%s] attach  [%s] open  [%s] resolve  [%s] filter  [%s] sort  [%s] quit  [%s] help",
		k.EditIssue, k.Runs, k.Issues, k.Chat, k.OpenRun, k.StartRun, k.ContinueRun, k.Attach, k.Open, k.Resolve, k.Filter, k.Sort, k.Quit, k.Help)
}
