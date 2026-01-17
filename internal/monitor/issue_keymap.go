package monitor

import "fmt"

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
	SortDir     string
	Attach      string
	Quit        string
	Help        string

	SortID       string
	SortStatus   string
	SortTitle    string
	SortPriority string
	SortUpdated  string
}

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
		SortDir:     "D",
		Attach:      "a",
		Quit:        "q",
		Help:        "?",

		SortID:       "1",
		SortStatus:   "t",
		SortTitle:    "n",
		SortPriority: "p",
		SortUpdated:  "u",
	}
}

// HelpLine renders the footer help text.
func (k IssueKeyMap) HelpLine() string {
	return fmt.Sprintf("[%s] edit  [%s] runs  [%s] issues  [%s] chat  [%s] open run  [%s] start run  [%s] continue  [%s] attach  [%s] open  [%s] resolve  [%s] filter  [%s] sort  [%s] quit  [%s] help",
		k.EditIssue, k.Runs, k.Issues, k.Chat, k.OpenRun, k.StartRun, k.ContinueRun, k.Attach, k.Open, k.Resolve, k.Filter, k.Sort, k.Quit, k.Help)
}
