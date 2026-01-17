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

	OpenBrowser string
	ViewIssue   string
}

func DefaultIssueKeyMap() IssueKeyMap {
	return IssueKeyMap{
		Runs:        "g",
		Issues:      "i",
		Chat:        "c",
		EditIssue:   "e",
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

		OpenBrowser: "enter",
		ViewIssue:   "I",
	}
}

func (k IssueKeyMap) HelpLine() string {
	return fmt.Sprintf("[%s] browser  [%s] edit  [%s] view  [%s] open run  [%s] start  [%s] continue  [%s] attach  [%s] resolve  [%s] filter  [%s] sort  [%s] quit  [%s] help",
		k.OpenBrowser, k.EditIssue, k.ViewIssue, k.OpenRun, k.StartRun, k.ContinueRun, k.Attach, k.Resolve, k.Filter, k.Sort, k.Quit, k.Help)
}
