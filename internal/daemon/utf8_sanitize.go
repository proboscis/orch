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

	// Check if any value needs sanitization before allocating.
	needsCopy := false
	for _, v := range values {
		if !utf8.ValidString(v) {
			needsCopy = true
			break
		}
	}
	if !needsCopy {
		return values
	}

	sanitized := make([]string, len(values))
	for i, value := range values {
		sanitized[i] = sanitizeUTF8(value)
	}
	return sanitized
}

func sanitizeUTF8Map(m map[string]string) map[string]string {
	if len(m) == 0 {
		return m
	}

	needsCopy := false
	for k, v := range m {
		if !utf8.ValidString(k) || !utf8.ValidString(v) {
			needsCopy = true
			break
		}
	}
	if !needsCopy {
		return m
	}

	sanitized := make(map[string]string, len(m))
	for k, v := range m {
		sanitized[sanitizeUTF8(k)] = sanitizeUTF8(v)
	}
	return sanitized
}
