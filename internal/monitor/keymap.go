package monitor

import "fmt"

type KeyMap struct {
	Runs        string
	Issues      string
	Chat        string
	Open        string
	EditIssue   string
	Exec        string
	Stop        string
	KillSession string
	NewRun      string
	Resolve     string
	Refresh     string
	Sort        string
	SortDir     string
	Filter      string
	QuickFilter string
	Quit        string
	Help        string

	SortUpdated string
	SortStarted string
	SortStatus  string
	SortIssue   string
	SortAgent   string
	SortElapsed string
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Runs:        "g",
		Issues:      "i",
		Chat:        "c",
		Open:        "enter",
		EditIssue:   "I",
		Exec:        "e",
		Stop:        "s",
		KillSession: "X",
		NewRun:      "n",
		Resolve:     "R",
		Refresh:     "r",
		Sort:        "S",
		SortDir:     "D",
		Filter:      "f",
		QuickFilter: "F",
		Quit:        "q",
		Help:        "?",

		SortUpdated: "u",
		SortStarted: "1",
		SortStatus:  "t",
		SortIssue:   "2",
		SortAgent:   "3",
		SortElapsed: "4",
	}
}

// HelpLine renders the footer help text.
func (k KeyMap) HelpLine() string {
	return fmt.Sprintf("[%s] runs  [%s] issues  [%s] chat  [%s] open  [%s] issue  [%s] exec  [%s] stop  [%s] kill  [%s] new  [%s] resolve  [%s] refresh  [%s] sort  [%s] filter  [%s] presets  [%s] quit  [%s] help",
		k.Runs, k.Issues, k.Chat, k.Open, k.EditIssue, k.Exec, k.Stop, k.KillSession, k.NewRun, k.Resolve, k.Refresh, k.Sort, k.Filter, k.QuickFilter, k.Quit, k.Help)
}
