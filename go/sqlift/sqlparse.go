// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import "strings"

// parsedTableBody holds structured data extracted from a CREATE TABLE body.
type parsedTableBody struct {
	checks            []CheckConstraint
	generatedExprs    map[string]string // column name -> expression
	pkConstraintName  string
	fkConstraintNames map[string]string // comma-joined from_columns -> name
	collations        map[string]string // column name -> collation (uppercase)
}

// parseCreateTableBody parses the body of a CREATE TABLE statement, extracting
// CHECK constraints, GENERATED ALWAYS AS expressions, PRIMARY KEY / FOREIGN KEY
// constraint names, and COLLATE clauses.
//
// Port of C++ parse_create_table_body (dist/sqlift.cpp lines 328-511), with an
// additional COLLATE extraction pass that replaces the C++ use of
// sqlite3_table_column_metadata (not exposed by mattn/go-sqlite3).
func parseCreateTableBody(rawSQL string) parsedTableBody {
	result := parsedTableBody{
		generatedExprs:    make(map[string]string),
		fkConstraintNames: make(map[string]string),
		collations:        make(map[string]string),
	}

	// --- Phase 1: locate the outer (...) of CREATE TABLE name (...) ----------

	depth := 0
	bodyStart := -1
	bodyEnd := -1

	for i := 0; i < len(rawSQL); i++ {
		switch rawSQL[i] {
		case '\'':
			// Skip single-quoted string literal ('...' with '' escaping).
			i++ // skip opening quote
			for i < len(rawSQL) {
				if rawSQL[i] == '\'' && i+1 < len(rawSQL) && rawSQL[i+1] == '\'' {
					i += 2 // skip escaped ''
				} else if rawSQL[i] == '\'' {
					break // closing quote; outer loop ++i advances past it
				} else {
					i++
				}
			}
		case '(':
			if depth == 0 {
				bodyStart = i + 1
			}
			depth++
		case ')':
			depth--
			if depth == 0 {
				bodyEnd = i
			}
		}
		if bodyEnd >= 0 {
			break
		}
	}

	if bodyStart < 0 || bodyEnd < 0 {
		return result
	}

	// --- Phase 2: split body by ',' at depth 0 --------------------------------

	body := rawSQL[bodyStart:bodyEnd]
	defs := splitByCommaDepth0(body)

	// --- Phase 3: classify each definition ------------------------------------

	for _, def := range defs {
		upperDef := toUpper(def)

		isCheck := false
		var chk CheckConstraint

		switch {
		case strings.HasPrefix(upperDef, "CHECK"):
			isCheck = true
			if paren := strings.IndexByte(def, '('); paren >= 0 {
				chk.Expression = strings.TrimSpace(extractParenContent(def, paren))
			}

		case strings.HasPrefix(upperDef, "CONSTRAINT"):
			if checkPos := strings.Index(upperDef, "CHECK"); checkPos >= 0 {
				// CONSTRAINT name CHECK(...)
				isCheck = true
				chk.Name = stripQuotes(strings.TrimSpace(def[10:checkPos]))
				if paren := indexByteFrom(def, '(', checkPos); paren >= 0 {
					chk.Expression = strings.TrimSpace(extractParenContent(def, paren))
				}
			} else if pkPos := strings.Index(upperDef, "PRIMARY KEY"); pkPos >= 0 {
				result.pkConstraintName = stripQuotes(strings.TrimSpace(def[10:pkPos]))
				continue
			} else if fkPos := strings.Index(upperDef, "FOREIGN KEY"); fkPos >= 0 {
				namePart := stripQuotes(strings.TrimSpace(def[10:fkPos]))
				if paren := indexByteFrom(def, '(', fkPos); paren >= 0 {
					colsStr := extractParenContent(def, paren)
					key := buildFKKey(colsStr)
					result.fkConstraintNames[key] = namePart
				}
				continue
			} else {
				// Unrecognised CONSTRAINT variant; skip.
				continue
			}
		}

		if isCheck {
			result.checks = append(result.checks, chk)
			continue
		}

		// GENERATED ALWAYS AS (expr)
		if genPos := strings.Index(upperDef, "GENERATED ALWAYS AS"); genPos >= 0 {
			colName := firstToken(def)
			if paren := indexByteFrom(def, '(', genPos); paren >= 0 {
				result.generatedExprs[colName] = strings.TrimSpace(extractParenContent(def, paren))
			}
		}

		// COLLATE <name> — appears anywhere in a column definition after the
		// column name.  We search in the uppercased definition for the keyword,
		// then read the token immediately following it from the original text so
		// we can normalise to uppercase.
		if collatePos := strings.Index(upperDef, "COLLATE"); collatePos >= 0 {
			// Verify "COLLATE" is a standalone keyword (preceded by whitespace
			// or start-of-string, and followed by whitespace).
			before := collatePos == 0 || isSpace(upperDef[collatePos-1])
			after := collatePos+7 < len(upperDef) && isSpace(upperDef[collatePos+7])
			if before && after {
				colName := firstToken(def)
				if colName != "" {
					// Find the collation name: first non-space token after COLLATE.
					rest := strings.TrimLeft(def[collatePos+7:], " \t\r\n")
					collationName := firstToken(rest)
					if collationName != "" {
						result.collations[colName] = toUpper(collationName)
					}
				}
			}
		}
	}

	return result
}

// parseTableOptions parses table options that appear after the closing ')' of
// a CREATE TABLE statement, e.g. "WITHOUT ROWID, STRICT".
//
// Port of C++ parse_table_options (dist/sqlift.cpp lines 515-543).
func parseTableOptions(rawSQL string) (withoutRowid, strict bool) {
	// Find the closing ')' at depth 0.
	depth := 0
	closeParen := -1
	for i := 0; i < len(rawSQL); i++ {
		switch rawSQL[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				closeParen = i
			}
		}
		if closeParen >= 0 {
			break
		}
	}

	if closeParen < 0 || closeParen+1 >= len(rawSQL) {
		return
	}

	tail := rawSQL[closeParen+1:]
	for _, token := range strings.Split(tail, ",") {
		t := toUpper(strings.TrimSpace(token))
		switch t {
		case "WITHOUT ROWID":
			withoutRowid = true
		case "STRICT":
			strict = true
		}
	}
	return
}

// --- helpers ------------------------------------------------------------------

// splitByCommaDepth0 splits s by ',' characters that are not inside nested
// parentheses or single-quoted string literals.
func splitByCommaDepth0(s string) []string {
	var defs []string
	depth := 0
	segStart := 0

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			// Skip string literal.
			i++
			for i < len(s) {
				if s[i] == '\'' && i+1 < len(s) && s[i+1] == '\'' {
					i += 2
				} else if s[i] == '\'' {
					break
				} else {
					i++
				}
			}
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				defs = append(defs, strings.TrimSpace(s[segStart:i]))
				segStart = i + 1
			}
		}
	}
	defs = append(defs, strings.TrimSpace(s[segStart:]))
	return defs
}

// extractParenContent returns the content between the matching parentheses
// starting at parenStart in s (exclusive of the outer parens themselves).
// Returns an empty string if no matching close paren is found.
func extractParenContent(s string, parenStart int) string {
	d := 0
	for i := parenStart; i < len(s); i++ {
		switch s[i] {
		case '(':
			d++
		case ')':
			d--
			if d == 0 {
				return s[parenStart+1 : i]
			}
		}
	}
	return ""
}

// indexByteFrom returns the index of the first occurrence of b in s at or
// after position from, or -1 if not found.
func indexByteFrom(s string, b byte, from int) int {
	if from >= len(s) {
		return -1
	}
	idx := strings.IndexByte(s[from:], b)
	if idx < 0 {
		return -1
	}
	return from + idx
}

// buildFKKey builds the comma-joined (no spaces) stripped column-name key used
// as the map key for fk_constraint_names.
func buildFKKey(colsStr string) string {
	parts := strings.Split(colsStr, ",")
	b := strings.Builder{}
	for i, p := range parts {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(stripQuotes(strings.TrimSpace(p)))
	}
	return b.String()
}

// firstToken returns the first whitespace-delimited token in s, with quotes
// stripped.
func firstToken(s string) string {
	s = strings.TrimLeft(s, " \t\r\n")
	end := strings.IndexAny(s, " \t\r\n")
	if end < 0 {
		return stripQuotes(s)
	}
	return stripQuotes(s[:end])
}

// isSpace reports whether b is an ASCII whitespace character.
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}
