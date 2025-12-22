package monitor

import "fmt"

// KeyMap defines the keyboard shortcuts displayed in the footer.
type KeyMap struct {
	Runs    string
	Issues  string
	Chat    string
	Open    string
	Exec    string
	Stop    string
	NewRun  string
	Resolve string
	Refresh string
	Quit    string
	Help    string
}

// DefaultKeyMap returns the default shortcut mapping.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Runs:    "g",
		Issues:  "i",
		Chat:    "c",
		Open:    "enter",
		Exec:    "e",
		Stop:    "s",
		NewRun:  "n",
		Resolve: "R",
		Refresh: "r",
		Quit:    "q",
		Help:    "?",
	}
}

// HelpLine renders the footer help text.
func (k KeyMap) HelpLine() string {
	return fmt.Sprintf("[%s] runs  [%s] issues  [%s] chat  [%s] open  [%s] exec  [%s] stop  [%s] new  [%s] resolve  [%s] refresh  [%s] quit  [%s] help",
		k.Runs, k.Issues, k.Chat, k.Open, k.Exec, k.Stop, k.NewRun, k.Resolve, k.Refresh, k.Quit, k.Help)
}
