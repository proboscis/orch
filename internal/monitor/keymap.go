package monitor

import "fmt"

// KeyMap defines the keyboard shortcuts displayed in the footer.
type KeyMap struct {
	Runs        string
	Issues      string
	Chat        string
	Open        string
	Exec        string
	Stop        string
	NewRun      string
	Resolve     string
	Merge       string
	OpenPR      string
	Refresh     string
	Sort        string
	Filter      string
	QuickFilter string
	Quit        string
	Help        string
}

// DefaultKeyMap returns the default shortcut mapping.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Runs:        "g",
		Issues:      "i",
		Chat:        "c",
		Open:        "enter",
		Exec:        "e",
		Stop:        "s",
		NewRun:      "n",
		Resolve:     "R",
		Merge:       "M",
		OpenPR:      "P",
		Refresh:     "r",
		Sort:        "S",
		Filter:      "f",
		QuickFilter: "F",
		Quit:        "q",
		Help:        "?",
	}
}

// HelpLine renders the footer help text.
func (k KeyMap) HelpLine() string {
	return fmt.Sprintf("[%s] runs  [%s] issues  [%s] chat  [%s] open  [%s] exec  [%s] stop  [%s] new  [%s] resolve  [%s] merge  [%s] pr  [%s] refresh  [%s] sort  [%s] filter  [%s] presets  [%s] quit  [%s] help",
		k.Runs, k.Issues, k.Chat, k.Open, k.Exec, k.Stop, k.NewRun, k.Resolve, k.Merge, k.OpenPR, k.Refresh, k.Sort, k.Filter, k.QuickFilter, k.Quit, k.Help)
}
