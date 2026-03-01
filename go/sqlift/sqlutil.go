// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import "strings"

// quoteID quotes an identifier for use in SQL using double quotes.
func quoteID(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// stripQuotes removes surrounding double quotes, backticks, or square brackets
// from an identifier.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		switch {
		case s[0] == '"' && s[len(s)-1] == '"':
			return s[1 : len(s)-1]
		case s[0] == '[' && s[len(s)-1] == ']':
			return s[1 : len(s)-1]
		case s[0] == '`' && s[len(s)-1] == '`':
			return s[1 : len(s)-1]
		}
	}
	return s
}

// toUpper returns the uppercase version of s (ASCII only, matching SQLite).
func toUpper(s string) string {
	return strings.ToUpper(s)
}
