package model

import "strings"

var yamlSpecialChars = []string{
	":", "#", "[", "]", "{", "}", ">", "|", "*", "&", "!", "%", "@", "`",
	"'", "\"", "\\", "\n", "\r", "\t",
}

var yamlSpecialPrefixes = []string{"-", "?", " "}

var yamlImplicitScalars = map[string]bool{
	"true": true, "false": true, "null": true, "~": true,
	"True": true, "False": true, "Null": true,
	"TRUE": true, "FALSE": true, "NULL": true,
	"yes": true, "no": true, "on": true, "off": true,
	"Yes": true, "No": true, "On": true, "Off": true,
	"YES": true, "NO": true, "ON": true, "OFF": true,
}

// NeedsYAMLQuoting returns true if s contains YAML special characters.
func NeedsYAMLQuoting(s string) bool {
	if s == "" {
		return true
	}

	if yamlImplicitScalars[s] {
		return true
	}

	for _, prefix := range yamlSpecialPrefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}

	if strings.HasSuffix(s, " ") {
		return true
	}

	for _, char := range yamlSpecialChars {
		if strings.Contains(s, char) {
			return true
		}
	}

	return false
}

// QuoteYAMLValue returns s quoted for YAML if needed.
func QuoteYAMLValue(s string) string {
	if !NeedsYAMLQuoting(s) {
		return s
	}

	escaped := strings.ReplaceAll(s, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	escaped = strings.ReplaceAll(escaped, "\n", "\\n")
	escaped = strings.ReplaceAll(escaped, "\r", "\\r")
	escaped = strings.ReplaceAll(escaped, "\t", "\\t")

	return "\"" + escaped + "\""
}
