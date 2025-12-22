package agent

import "strings"

// doubleQuote wraps a string in double quotes, escaping special characters.
func doubleQuote(s string) string {
	// Escape backslashes, double quotes, backticks, and dollar signs.
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "$", "\\$")
	return "\"" + s + "\""
}
