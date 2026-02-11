package daemon

import (
	"strings"
	"unicode/utf8"
)

func sanitizeUTF8(value string) string {
	if utf8.ValidString(value) {
		return value
	}
	return strings.ToValidUTF8(value, "\uFFFD")
}

func sanitizeUTF8Slice(values []string) []string {
	if len(values) == 0 {
		return values
	}

	sanitized := make([]string, len(values))
	for i, value := range values {
		sanitized[i] = sanitizeUTF8(value)
	}

	return sanitized
}
