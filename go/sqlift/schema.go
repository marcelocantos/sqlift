// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

// GeneratedType matches SQLite's table_xinfo hidden field values.
type GeneratedType int

const (
	GeneratedNormal  GeneratedType = 0
	GeneratedVirtual GeneratedType = 2
	GeneratedStored  GeneratedType = 3
)

// Column represents a single column in a table.
type Column struct {
	Name          string
	Type          string // Uppercase. Empty if untyped.
	NotNull       bool
	DefaultValue  string        // Raw SQL expression; empty if no default.
	PK            int           // 0 = not PK, 1+ = position in composite PK.
	Collation     string        // e.g. "NOCASE"; empty = default (BINARY).
	Generated     GeneratedType // Normal, Virtual, or Stored.
	GeneratedExpr string        // e.g. "first_name || ' ' || last_name"; empty if not generated.
}

// Equal returns true if all column fields match.
func (c Column) Equal(o Column) bool {
	return c.Name == o.Name &&
		c.Type == o.Type &&
		c.NotNull == o.NotNull &&
		c.DefaultValue == o.DefaultValue &&
		c.PK == o.PK &&
		c.Collation == o.Collation &&
		c.Generated == o.Generated &&
		c.GeneratedExpr == o.GeneratedExpr
}

// CheckConstraint represents a CHECK constraint on a table.
type CheckConstraint struct {
	Name       string // empty if unnamed
	Expression string // e.g. "age > 0"
}

// Equal returns true if both name and expression match.
func (c CheckConstraint) Equal(o CheckConstraint) bool {
	return c.Name == o.Name && c.Expression == o.Expression
}

// ForeignKey represents a foreign key constraint.
type ForeignKey struct {
	ConstraintName string   // empty if unnamed (cosmetic, excluded from Equal)
	FromColumns    []string
	ToTable        string
	ToColumns      []string
	OnUpdate       string // default "NO ACTION"
	OnDelete       string // default "NO ACTION"
}

// Equal compares structural fields only, excluding ConstraintName (cosmetic).
func (f ForeignKey) Equal(o ForeignKey) bool {
	return sliceEqual(f.FromColumns, o.FromColumns) &&
		f.ToTable == o.ToTable &&
		sliceEqual(f.ToColumns, o.ToColumns) &&
		f.OnUpdate == o.OnUpdate &&
		f.OnDelete == o.OnDelete
}

// Table represents a database table.
type Table struct {
	Name             string
	Columns          []Column          // Ordered by cid.
	ForeignKeys      []ForeignKey
	CheckConstraints []CheckConstraint
	PKConstraintName string // empty if unnamed (cosmetic, excluded from Equal)
	WithoutRowid     bool
	Strict           bool
	RawSQL           string // Original CREATE TABLE from sqlite_master.
}

// Equal compares structural fields only, excluding RawSQL and PKConstraintName.
func (t Table) Equal(o Table) bool {
	if t.Name != o.Name || t.WithoutRowid != o.WithoutRowid || t.Strict != o.Strict {
		return false
	}
	if len(t.Columns) != len(o.Columns) {
		return false
	}
	for i := range t.Columns {
		if !t.Columns[i].Equal(o.Columns[i]) {
			return false
		}
	}
	if len(t.ForeignKeys) != len(o.ForeignKeys) {
		return false
	}
	for i := range t.ForeignKeys {
		if !t.ForeignKeys[i].Equal(o.ForeignKeys[i]) {
			return false
		}
	}
	if len(t.CheckConstraints) != len(o.CheckConstraints) {
		return false
	}
	for i := range t.CheckConstraints {
		if !t.CheckConstraints[i].Equal(o.CheckConstraints[i]) {
			return false
		}
	}
	return true
}

// Index represents a database index.
type Index struct {
	Name        string
	TableName   string
	Columns     []string
	Unique      bool
	WhereClause string // Partial index; empty if not partial.
	RawSQL      string // Original CREATE INDEX from sqlite_master.
}

// Equal compares structural fields only, excluding RawSQL.
func (idx Index) Equal(o Index) bool {
	return idx.Name == o.Name &&
		idx.TableName == o.TableName &&
		sliceEqual(idx.Columns, o.Columns) &&
		idx.Unique == o.Unique &&
		idx.WhereClause == o.WhereClause
}

// View represents a database view.
type View struct {
	Name string
	SQL  string
}

// Equal returns true if both name and SQL match.
func (v View) Equal(o View) bool {
	return v.Name == o.Name && v.SQL == o.SQL
}

// Trigger represents a database trigger.
type Trigger struct {
	Name      string
	TableName string
	SQL       string
}

// Equal returns true if all fields match.
func (tr Trigger) Equal(o Trigger) bool {
	return tr.Name == o.Name && tr.TableName == o.TableName && tr.SQL == o.SQL
}

// Schema represents the complete schema of a database.
type Schema struct {
	Tables   map[string]Table
	Indexes  map[string]Index
	Views    map[string]View
	Triggers map[string]Trigger
}

// Equal returns true if all schema objects are structurally equal.
func (s Schema) Equal(o Schema) bool {
	if len(s.Tables) != len(o.Tables) {
		return false
	}
	for name, t := range s.Tables {
		ot, ok := o.Tables[name]
		if !ok || !t.Equal(ot) {
			return false
		}
	}
	if len(s.Indexes) != len(o.Indexes) {
		return false
	}
	for name, idx := range s.Indexes {
		oidx, ok := o.Indexes[name]
		if !ok || !idx.Equal(oidx) {
			return false
		}
	}
	if len(s.Views) != len(o.Views) {
		return false
	}
	for name, v := range s.Views {
		ov, ok := o.Views[name]
		if !ok || !v.Equal(ov) {
			return false
		}
	}
	if len(s.Triggers) != len(o.Triggers) {
		return false
	}
	for name, tr := range s.Triggers {
		otr, ok := o.Triggers[name]
		if !ok || !tr.Equal(otr) {
			return false
		}
	}
	return true
}

// Hash returns a deterministic SHA-256 hex digest of the schema.
// The serialization format is identical to the C++ implementation
// for cross-language drift detection.
func (s Schema) Hash() string {
	var b strings.Builder

	// Tables in sorted order.
	for _, name := range sortedKeys(s.Tables) {
		table := s.Tables[name]
		b.WriteString("TABLE ")
		b.WriteString(name)
		b.WriteByte('\n')
		for _, col := range table.Columns {
			b.WriteString("  COL ")
			b.WriteString(col.Name)
			b.WriteByte(' ')
			b.WriteString(col.Type)
			if col.NotNull {
				b.WriteString(" NOTNULL")
			}
			b.WriteString(" DEFAULT=")
			b.WriteString(col.DefaultValue)
			b.WriteString(" PK=")
			fmt.Fprintf(&b, "%d", col.PK)
			if col.Collation != "" {
				b.WriteString(" COLLATE=")
				b.WriteString(col.Collation)
			}
			if col.Generated != GeneratedNormal {
				fmt.Fprintf(&b, " GENERATED=%d", int(col.Generated))
			}
			if col.GeneratedExpr != "" {
				b.WriteString(" EXPR=")
				b.WriteString(col.GeneratedExpr)
			}
			b.WriteByte('\n')
		}
		for _, fk := range table.ForeignKeys {
			b.WriteString("  FK")
			for _, c := range fk.FromColumns {
				b.WriteByte(' ')
				b.WriteString(c)
			}
			b.WriteString(" -> ")
			b.WriteString(fk.ToTable)
			b.WriteByte('(')
			for i, c := range fk.ToColumns {
				if i > 0 {
					b.WriteByte(',')
				}
				b.WriteString(c)
			}
			b.WriteString(") UPDATE=")
			b.WriteString(fk.OnUpdate)
			b.WriteString(" DELETE=")
			b.WriteString(fk.OnDelete)
			b.WriteByte('\n')
		}
		for _, chk := range table.CheckConstraints {
			b.WriteString("  CHECK")
			if chk.Name != "" {
				b.WriteString(" NAME=")
				b.WriteString(chk.Name)
			}
			b.WriteString(" EXPR=")
			b.WriteString(chk.Expression)
			b.WriteByte('\n')
		}
		b.WriteString("  ROWID=")
		if table.WithoutRowid {
			b.WriteString("no")
		} else {
			b.WriteString("yes")
		}
		b.WriteByte('\n')
		if table.Strict {
			b.WriteString("  STRICT=yes\n")
		}
	}

	// Indexes in sorted order.
	for _, name := range sortedKeys(s.Indexes) {
		idx := s.Indexes[name]
		b.WriteString("INDEX ")
		b.WriteString(name)
		b.WriteString(" ON ")
		b.WriteString(idx.TableName)
		if idx.Unique {
			b.WriteString(" UNIQUE")
		}
		for _, c := range idx.Columns {
			b.WriteByte(' ')
			b.WriteString(c)
		}
		if idx.WhereClause != "" {
			b.WriteString(" WHERE ")
			b.WriteString(idx.WhereClause)
		}
		b.WriteByte('\n')
	}

	// Views in sorted order.
	for _, name := range sortedKeys(s.Views) {
		view := s.Views[name]
		b.WriteString("VIEW ")
		b.WriteString(name)
		b.WriteByte(' ')
		b.WriteString(view.SQL)
		b.WriteByte('\n')
	}

	// Triggers in sorted order.
	for _, name := range sortedKeys(s.Triggers) {
		trig := s.Triggers[name]
		b.WriteString("TRIGGER ")
		b.WriteString(name)
		b.WriteByte(' ')
		b.WriteString(trig.SQL)
		b.WriteByte('\n')
	}

	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("%x", sum)
}

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sliceEqual returns true if two string slices are element-wise equal.
func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
