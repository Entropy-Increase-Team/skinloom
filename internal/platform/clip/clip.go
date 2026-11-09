// Package clip shortens text to fit a column or a log line without cutting a UTF-8
// character in half.
package clip

import "unicode/utf8"

// Bytes cuts s to at most n bytes, at a character boundary.
func Bytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	n = max(n, 0)
	// A character has at most UTFMax-1 continuation bytes; stepping back further only
	// happens in invalid UTF-8, which is cut where it is.
	for i := 0; i < utf8.UTFMax-1 && n > 0 && !utf8.RuneStart(s[n]); i++ {
		n--
	}
	return s[:n]
}

// Runes cuts s to at most n characters.
func Runes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:max(n, 0)])
}
