package monitor

import "fmt"

// KeyMap defines the keyboard shortcuts displayed in the footer.
type KeyMap struct {
	Runs    string
	Issues  string
	Chat    string
	Open    string
	Answer  string
	Stop    string
	NewRun  string
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
		Answer:  "a",
		Stop:    "s",
		NewRun:  "n",
		Refresh: "r",
		Quit:    "q",
		Help:    "?",
	}
}

// HelpLine renders the footer help text.
func (k KeyMap) HelpLine() string {
	return fmt.Sprintf("[%s] runs  [%s] issues  [%s] chat  [%s] open  [%s] answer  [%s] stop  [%s] new  [%s] refresh  [%s] quit  [%s] help",
		k.Runs, k.Issues, k.Chat, k.Open, k.Answer, k.Stop, k.NewRun, k.Refresh, k.Quit, k.Help)
}
