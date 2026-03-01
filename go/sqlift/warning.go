// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"sort"
)

// WarningType classifies the kind of schema warning.
type WarningType int

const (
	// RedundantIndex indicates an index whose columns duplicate or are a
	// prefix of another index or the table's PRIMARY KEY.
	RedundantIndex WarningType = iota
)

// Warning describes a non-fatal issue detected in a schema.
type Warning struct {
	Type      WarningType
	Message   string // Human-readable description.
	IndexName string // The redundant index.
	CoveredBy string // The covering index name, or "PRIMARY KEY".
	TableName string // The table both indexes belong to.
}

// DetectRedundantIndexes analyses a schema for prefix-duplicate and
// PK-duplicate indexes.
func DetectRedundantIndexes(schema Schema) []Warning {
	var warnings []Warning
	pkFlagged := make(map[string]bool)

	// Group indexes by table.
	byTable := make(map[string][]*Index)
	for name := range schema.Indexes {
		idx := schema.Indexes[name]
		byTable[idx.TableName] = append(byTable[idx.TableName], &idx)
	}

	for tableName, table := range schema.Tables {
		// Build PK column list ordered by pk position.
		type pkPair struct {
			pos  int
			name string
		}
		var pkPairs []pkPair
		for _, col := range table.Columns {
			if col.PK > 0 {
				pkPairs = append(pkPairs, pkPair{col.PK, col.Name})
			}
		}
		sort.Slice(pkPairs, func(i, j int) bool { return pkPairs[i].pos < pkPairs[j].pos })
		pkColumns := make([]string, len(pkPairs))
		for i, p := range pkPairs {
			pkColumns[i] = p.name
		}

		indexes := byTable[tableName]
		if len(indexes) == 0 {
			continue
		}

		// --- PK-duplicate detection ---
		if len(pkColumns) > 0 {
			for _, idx := range indexes {
				// Partial indexes can't be PK-duplicates (PK has no WHERE).
				if idx.WhereClause != "" {
					continue
				}
				if len(idx.Columns) > len(pkColumns) {
					continue
				}
				// Check if idx.Columns is a prefix of pkColumns.
				if !isPrefix(idx.Columns, pkColumns) {
					continue
				}
				exactMatch := len(idx.Columns) == len(pkColumns)
				if exactMatch || !idx.Unique {
					pkFlagged[idx.Name] = true
					rel := "a prefix of"
					if exactMatch {
						rel = "identical to"
					}
					warnings = append(warnings, Warning{
						Type:      RedundantIndex,
						Message:   "Index '" + idx.Name + "' on table '" + tableName + "' is redundant: columns are " + rel + " PRIMARY KEY",
						IndexName: idx.Name,
						CoveredBy: "PRIMARY KEY",
						TableName: tableName,
					})
				}
			}
		}

		// --- Prefix-duplicate detection ---
		for _, shorter := range indexes {
			if pkFlagged[shorter.Name] {
				continue
			}
			for _, longer := range indexes {
				if shorter.Name == longer.Name {
					continue
				}
				if pkFlagged[longer.Name] {
					continue
				}
				if len(shorter.Columns) >= len(longer.Columns) {
					continue
				}
				if shorter.WhereClause != longer.WhereClause {
					continue
				}
				if !isPrefix(shorter.Columns, longer.Columns) {
					continue
				}
				// Non-unique shorter: redundant. Unique shorter: not redundant.
				if !shorter.Unique {
					warnings = append(warnings, Warning{
						Type:      RedundantIndex,
						Message:   "Index '" + shorter.Name + "' on table '" + tableName + "' is redundant: columns are a prefix of index '" + longer.Name + "'",
						IndexName: shorter.Name,
						CoveredBy: longer.Name,
						TableName: tableName,
					})
					break // One warning per redundant index.
				}
			}
		}

		// --- Exact-duplicate detection (same columns, same WHERE) ---
		for i := 0; i < len(indexes); i++ {
			if pkFlagged[indexes[i].Name] {
				continue
			}
			for j := i + 1; j < len(indexes); j++ {
				if pkFlagged[indexes[j].Name] {
					continue
				}
				if !sliceEqual(indexes[i].Columns, indexes[j].Columns) {
					continue
				}
				if indexes[i].WhereClause != indexes[j].WhereClause {
					continue
				}

				var redundant, keeper *Index
				if indexes[i].Unique == indexes[j].Unique {
					if indexes[i].Name < indexes[j].Name {
						redundant = indexes[j]
						keeper = indexes[i]
					} else {
						redundant = indexes[i]
						keeper = indexes[j]
					}
				} else if !indexes[i].Unique {
					redundant = indexes[i]
					keeper = indexes[j]
				} else {
					redundant = indexes[j]
					keeper = indexes[i]
				}

				// Skip if already warned.
				alreadyWarned := false
				for _, w := range warnings {
					if w.IndexName == redundant.Name {
						alreadyWarned = true
						break
					}
				}
				if alreadyWarned {
					continue
				}

				warnings = append(warnings, Warning{
					Type:      RedundantIndex,
					Message:   "Index '" + redundant.Name + "' on table '" + tableName + "' is redundant: duplicate of index '" + keeper.Name + "'",
					IndexName: redundant.Name,
					CoveredBy: keeper.Name,
					TableName: tableName,
				})
			}
		}
	}

	// Sort warnings by (table_name, index_name) for deterministic output.
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].TableName != warnings[j].TableName {
			return warnings[i].TableName < warnings[j].TableName
		}
		return warnings[i].IndexName < warnings[j].IndexName
	})

	return warnings
}

// isPrefix reports whether a is an element-wise prefix of b.
func isPrefix(a, b []string) bool {
	if len(a) > len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
