// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"fmt"
	"strings"
)

// OpType classifies the kind of schema operation.
type OpType int

const (
	CreateTable  OpType = iota // Create a new table.
	DropTable                  // Drop an existing table.
	RebuildTable               // Rebuild (recreate) a table.
	AddColumn                  // Add a column via ALTER TABLE.
	CreateIndex                // Create a new index.
	DropIndex                  // Drop an existing index.
	CreateView                 // Create a new view.
	DropView                   // Drop an existing view.
	CreateTrigger              // Create a new trigger.
	DropTrigger                // Drop an existing trigger.
)

// Operation describes a single migration step.
type Operation struct {
	Type        OpType
	ObjectName  string
	Description string
	SQL         []string
	Destructive bool
}

// MigrationPlan holds an ordered list of operations produced by [Diff].
type MigrationPlan struct {
	operations []Operation
}

// Operations returns the ordered list of migration operations.
func (p MigrationPlan) Operations() []Operation { return p.operations }

// HasDestructiveOperations reports whether any operation in the plan is
// destructive (drops data).
func (p MigrationPlan) HasDestructiveOperations() bool {
	for i := range p.operations {
		if p.operations[i].Destructive {
			return true
		}
	}
	return false
}

// Empty reports whether the plan contains no operations.
func (p MigrationPlan) Empty() bool { return len(p.operations) == 0 }

// ApplyOptions controls the behavior of [Apply].
type ApplyOptions struct {
	AllowDestructive bool
}

// Diff compares two schemas and produces a [MigrationPlan] that migrates
// current to desired. It is a pure function and never touches a database.
//
// It returns a [*BreakingChangeError] if the desired schema contains changes
// whose success depends on existing data (e.g. nullable to NOT NULL).
func Diff(current, desired Schema) (MigrationPlan, error) {
	var plan MigrationPlan

	// Build known names for dependency analysis (drop phases need both
	// current and desired names).
	knownNames := make(map[string]bool)
	for n := range current.Tables {
		knownNames[n] = true
	}
	for n := range current.Views {
		knownNames[n] = true
	}
	for n := range desired.Tables {
		knownNames[n] = true
	}
	for n := range desired.Views {
		knownNames[n] = true
	}

	// --- Phase 1: Drop triggers that are removed or changed ---
	{
		var toDrop []string
		dropDestructive := map[string]bool{}
		for name, trig := range current.Triggers {
			dt, ok := desired.Triggers[name]
			if !ok || dt.SQL != trig.SQL {
				toDrop = append(toDrop, name)
				dropDestructive[name] = !ok
			}
		}
		// Build dependency graph; only keep deps that are also being dropped.
		dropSet := make(map[string]bool, len(toDrop))
		for _, n := range toDrop {
			dropSet[n] = true
		}
		deps := make(map[string]map[string]bool, len(toDrop))
		for _, name := range toDrop {
			allRefs := extractSQLReferences(current.Triggers[name].SQL, name, knownNames)
			filtered := make(map[string]bool)
			for d := range allRefs {
				if dropSet[d] {
					filtered[d] = true
				}
			}
			deps[name] = filtered
		}
		sorted, err := topoSort(toDrop, deps, true)
		if err != nil {
			return MigrationPlan{}, err
		}
		for _, name := range sorted {
			plan.operations = append(plan.operations, Operation{
				Type:        DropTrigger,
				ObjectName:  name,
				Description: "Drop trigger " + name,
				SQL:         []string{"DROP TRIGGER IF EXISTS " + quoteID(name)},
				Destructive: dropDestructive[name],
			})
		}
	}

	// --- Phase 2: Drop views that are removed or changed ---
	{
		var toDrop []string
		dropDestructive := map[string]bool{}
		for name, view := range current.Views {
			dv, ok := desired.Views[name]
			if !ok || dv.SQL != view.SQL {
				toDrop = append(toDrop, name)
				dropDestructive[name] = !ok
			}
		}
		deps := make(map[string]map[string]bool, len(toDrop))
		for _, name := range toDrop {
			deps[name] = extractSQLReferences(current.Views[name].SQL, name, knownNames)
		}
		sorted, err := topoSort(toDrop, deps, true)
		if err != nil {
			return MigrationPlan{}, err
		}
		for _, name := range sorted {
			plan.operations = append(plan.operations, Operation{
				Type:        DropView,
				ObjectName:  name,
				Description: "Drop view " + name,
				SQL:         []string{"DROP VIEW IF EXISTS " + quoteID(name)},
				Destructive: dropDestructive[name],
			})
		}
	}

	// --- Phase 3: Drop indexes that are removed or changed ---
	// Pre-scan to find which tables need rebuilding (not append-only).
	tablesToRebuild := make(map[string]bool)
	for name, desiredTable := range desired.Tables {
		currentTable, ok := current.Tables[name]
		if ok && !currentTable.Equal(desiredTable) {
			if !isAppendOnly(currentTable, desiredTable) {
				tablesToRebuild[name] = true
			}
		}
	}

	for name, idx := range current.Indexes {
		di, ok := desired.Indexes[name]
		needsDrop := false
		if !ok {
			needsDrop = true
		} else if !di.Equal(idx) {
			needsDrop = true
		} else if tablesToRebuild[idx.TableName] {
			needsDrop = true
		}
		if needsDrop {
			plan.operations = append(plan.operations, Operation{
				Type:        DropIndex,
				ObjectName:  name,
				Description: "Drop index " + name,
				SQL:         []string{"DROP INDEX IF EXISTS " + quoteID(name)},
				Destructive: !ok,
			})
		}
	}

	// --- Phase 4: Table operations ---

	// Create new tables.
	for name, table := range desired.Tables {
		if _, ok := current.Tables[name]; !ok {
			plan.operations = append(plan.operations, Operation{
				Type:        CreateTable,
				ObjectName:  name,
				Description: "Create table " + name,
				SQL:         []string{table.RawSQL},
				Destructive: false,
			})
		}
	}

	// Check for breaking changes across all modified tables.
	{
		var violations []string
		for name, desiredTable := range desired.Tables {
			currentTable, ok := current.Tables[name]
			if !ok || currentTable.Equal(desiredTable) {
				continue
			}

			// Build column lookup for the current table.
			curColMap := make(map[string]Column, len(currentTable.Columns))
			for _, col := range currentTable.Columns {
				curColMap[col.Name] = col
			}

			// (a) Existing nullable column becomes NOT NULL.
			for _, col := range desiredTable.Columns {
				if curCol, found := curColMap[col.Name]; found {
					if !curCol.NotNull && col.NotNull {
						violations = append(violations,
							fmt.Sprintf("Table '%s': column '%s' changes from nullable to NOT NULL",
								name, col.Name))
					}
				}
			}

			// (b) New FK constraint on existing table.
			for _, fk := range desiredTable.ForeignKeys {
				found := false
				for _, curFK := range currentTable.ForeignKeys {
					if curFK.Equal(fk) {
						found = true
						break
					}
				}
				if !found {
					var b strings.Builder
					fmt.Fprintf(&b, "Table '%s': adds foreign key (", name)
					for i, c := range fk.FromColumns {
						if i > 0 {
							b.WriteString(", ")
						}
						b.WriteString(c)
					}
					b.WriteString(") references ")
					b.WriteString(fk.ToTable)
					b.WriteByte('(')
					for i, c := range fk.ToColumns {
						if i > 0 {
							b.WriteString(", ")
						}
						b.WriteString(c)
					}
					b.WriteByte(')')
					violations = append(violations, b.String())
				}
			}

			// (c) New CHECK constraint on existing table.
			for _, chk := range desiredTable.CheckConstraints {
				found := false
				for _, curChk := range currentTable.CheckConstraints {
					if curChk.Equal(chk) {
						found = true
						break
					}
				}
				if !found {
					msg := fmt.Sprintf("Table '%s': adds CHECK constraint", name)
					if chk.Name != "" {
						msg += " '" + chk.Name + "'"
					}
					msg += " (" + chk.Expression + ")"
					violations = append(violations, msg)
				}
			}

			// (d) New NOT NULL column without DEFAULT (guaranteed failure on
			// non-empty table).
			for _, col := range desiredTable.Columns {
				if _, exists := curColMap[col.Name]; !exists {
					if col.NotNull && col.DefaultValue == "" && col.PK == 0 {
						violations = append(violations,
							fmt.Sprintf("Table '%s': new column '%s' is NOT NULL without DEFAULT",
								name, col.Name))
					}
				}
			}
		}
		if len(violations) > 0 {
			return MigrationPlan{}, &BreakingChangeError{
				Msg: "Breaking schema changes detected:\n- " + strings.Join(violations, "\n- "),
			}
		}
	}

	// Modify existing tables.
	for name, desiredTable := range desired.Tables {
		currentTable, ok := current.Tables[name]
		if !ok || currentTable.Equal(desiredTable) {
			continue
		}

		if isAppendOnly(currentTable, desiredTable) {
			// AddColumn fast path.
			for i := len(currentTable.Columns); i < len(desiredTable.Columns); i++ {
				col := desiredTable.Columns[i]
				plan.operations = append(plan.operations, Operation{
					Type:        AddColumn,
					ObjectName:  name,
					Description: "Add column " + col.Name + " to " + name,
					SQL:         []string{addColumnSQL(name, col)},
					Destructive: false,
				})
			}
		} else {
			// Full rebuild.
			plan.operations = append(plan.operations, Operation{
				Type:        RebuildTable,
				ObjectName:  name,
				Description: describeTableChanges(currentTable, desiredTable),
				SQL:         rebuildTableSQL(currentTable, desiredTable, desired),
				Destructive: rebuildIsDestructive(currentTable, desiredTable),
			})
		}
	}

	// Drop removed tables.
	for name := range current.Tables {
		if _, ok := desired.Tables[name]; !ok {
			plan.operations = append(plan.operations, Operation{
				Type:        DropTable,
				ObjectName:  name,
				Description: "Drop table " + name,
				SQL:         []string{"DROP TABLE IF EXISTS " + quoteID(name)},
				Destructive: true,
			})
		}
	}

	// --- Phase 5: Create indexes (not part of rebuilds) ---
	for name, idx := range desired.Indexes {
		// Skip indexes on rebuilt tables (they were recreated in the rebuild).
		if tablesToRebuild[idx.TableName] {
			continue
		}

		needsCreate := false
		ci, ok := current.Indexes[name]
		if !ok {
			needsCreate = true
		} else if !ci.Equal(idx) {
			needsCreate = true
		}

		if needsCreate {
			plan.operations = append(plan.operations, Operation{
				Type:        CreateIndex,
				ObjectName:  name,
				Description: "Create index " + name + " on " + idx.TableName,
				SQL:         []string{idx.RawSQL},
				Destructive: false,
			})
		}
	}

	// --- Phase 6: Create views (new or changed, topo order) ---
	{
		var toCreate []string
		for name, view := range desired.Views {
			cv, ok := current.Views[name]
			if !ok || cv.SQL != view.SQL {
				toCreate = append(toCreate, name)
			}
		}
		// Build known names from desired schema for create ordering.
		desiredKnown := make(map[string]bool)
		for n := range desired.Tables {
			desiredKnown[n] = true
		}
		for n := range desired.Views {
			desiredKnown[n] = true
		}
		deps := make(map[string]map[string]bool, len(toCreate))
		for _, name := range toCreate {
			deps[name] = extractSQLReferences(desired.Views[name].SQL, name, desiredKnown)
		}
		sorted, err := topoSort(toCreate, deps, false)
		if err != nil {
			return MigrationPlan{}, err
		}
		for _, name := range sorted {
			plan.operations = append(plan.operations, Operation{
				Type:        CreateView,
				ObjectName:  name,
				Description: "Create view " + name,
				SQL:         []string{desired.Views[name].SQL},
				Destructive: false,
			})
		}
	}

	// --- Phase 7: Create triggers (new or changed, topo order) ---
	{
		var toCreate []string
		for name, trig := range desired.Triggers {
			ct, ok := current.Triggers[name]
			if !ok || ct.SQL != trig.SQL {
				toCreate = append(toCreate, name)
			}
		}
		desiredKnown := make(map[string]bool)
		for n := range desired.Tables {
			desiredKnown[n] = true
		}
		for n := range desired.Views {
			desiredKnown[n] = true
		}
		for n := range desired.Triggers {
			desiredKnown[n] = true
		}
		deps := make(map[string]map[string]bool, len(toCreate))
		for _, name := range toCreate {
			deps[name] = extractSQLReferences(desired.Triggers[name].SQL, name, desiredKnown)
		}
		sorted, err := topoSort(toCreate, deps, false)
		if err != nil {
			return MigrationPlan{}, err
		}
		for _, name := range sorted {
			plan.operations = append(plan.operations, Operation{
				Type:        CreateTrigger,
				ObjectName:  name,
				Description: "Create trigger " + name,
				SQL:         []string{desired.Triggers[name].SQL},
				Destructive: false,
			})
		}
	}

	return plan, nil
}

// canAddColumn reports whether a column can be added via ALTER TABLE ADD COLUMN.
// SQLite restricts ADD COLUMN to non-PK, non-generated columns that either
// allow NULL or have a DEFAULT value.
func canAddColumn(col Column) bool {
	if col.PK != 0 {
		return false
	}
	if col.NotNull && col.DefaultValue == "" {
		return false
	}
	if col.Generated != GeneratedNormal {
		return false
	}
	return true
}

// isAppendOnly reports whether the only change between current and desired is
// new columns appended at the end, all of which can be added via ALTER TABLE
// ADD COLUMN.
func isAppendOnly(current, desired Table) bool {
	if len(desired.Columns) <= len(current.Columns) {
		return false
	}
	// All existing columns must be unchanged.
	for i := range current.Columns {
		if !current.Columns[i].Equal(desired.Columns[i]) {
			return false
		}
	}
	// Foreign keys must be unchanged.
	if len(current.ForeignKeys) != len(desired.ForeignKeys) {
		return false
	}
	for i := range current.ForeignKeys {
		if !current.ForeignKeys[i].Equal(desired.ForeignKeys[i]) {
			return false
		}
	}
	// CHECK constraints must be unchanged.
	if len(current.CheckConstraints) != len(desired.CheckConstraints) {
		return false
	}
	for i := range current.CheckConstraints {
		if !current.CheckConstraints[i].Equal(desired.CheckConstraints[i]) {
			return false
		}
	}
	// WITHOUT ROWID and STRICT must be unchanged.
	if current.WithoutRowid != desired.WithoutRowid {
		return false
	}
	if current.Strict != desired.Strict {
		return false
	}
	// All new columns must be addable.
	for i := len(current.Columns); i < len(desired.Columns); i++ {
		if !canAddColumn(desired.Columns[i]) {
			return false
		}
	}
	return true
}

// addColumnSQL builds an ALTER TABLE ADD COLUMN statement.
func addColumnSQL(tableName string, col Column) string {
	var b strings.Builder
	b.WriteString("ALTER TABLE ")
	b.WriteString(quoteID(tableName))
	b.WriteString(" ADD COLUMN ")
	b.WriteString(quoteID(col.Name))
	if col.Type != "" {
		b.WriteByte(' ')
		b.WriteString(col.Type)
	}
	if col.Collation != "" {
		b.WriteString(" COLLATE ")
		b.WriteString(col.Collation)
	}
	if col.NotNull {
		b.WriteString(" NOT NULL")
	}
	if col.DefaultValue != "" {
		b.WriteString(" DEFAULT ")
		b.WriteString(col.DefaultValue)
	}
	return b.String()
}

// rebuildTableSQL produces the SQL statements for a 12-step table rebuild.
func rebuildTableSQL(current, desired Table, desiredSchema Schema) []string {
	var stmts []string
	tmpName := quoteID(desired.Name + "_sqlift_new")
	tblName := quoteID(desired.Name)

	// Step 1: Disable foreign keys.
	stmts = append(stmts, "PRAGMA foreign_keys=OFF")

	// Step 2: Savepoint.
	stmts = append(stmts, "SAVEPOINT sqlift_rebuild")

	// Step 3: Create new table with desired schema.
	// Replace the table name in the CREATE TABLE statement with the temp name.
	createStmt := desired.RawSQL
	if parenPos := strings.Index(createStmt, "("); parenPos >= 0 {
		createStmt = "CREATE TABLE " + tmpName + " " + createStmt[parenPos:]
	}
	stmts = append(stmts, createStmt)

	// Step 4: Copy data from old table to new (common columns only).
	// Skip generated columns -- they are computed and cannot be inserted.
	desiredColNames := make(map[string]bool, len(desired.Columns))
	generatedColNames := make(map[string]bool)
	for _, col := range desired.Columns {
		desiredColNames[col.Name] = true
		if col.Generated != GeneratedNormal {
			generatedColNames[col.Name] = true
		}
	}
	var commonCols []string
	for _, col := range current.Columns {
		if desiredColNames[col.Name] && !generatedColNames[col.Name] {
			commonCols = append(commonCols, quoteID(col.Name))
		}
	}
	if len(commonCols) > 0 {
		colList := strings.Join(commonCols, ", ")
		stmts = append(stmts,
			"INSERT INTO "+tmpName+" ("+colList+") SELECT "+colList+" FROM "+tblName)
	}

	// Step 5: Drop old table.
	stmts = append(stmts, "DROP TABLE "+tblName)

	// Step 6: Rename new table.
	stmts = append(stmts, "ALTER TABLE "+tmpName+" RENAME TO "+tblName)

	// Step 7: Recreate indexes for this table.
	for _, idxName := range sortedKeys(desiredSchema.Indexes) {
		idx := desiredSchema.Indexes[idxName]
		if idx.TableName == desired.Name && idx.RawSQL != "" {
			stmts = append(stmts, idx.RawSQL)
		}
	}

	// Step 8: Recreate triggers for this table.
	for _, trigName := range sortedKeys(desiredSchema.Triggers) {
		trig := desiredSchema.Triggers[trigName]
		if trig.TableName == desired.Name && trig.SQL != "" {
			stmts = append(stmts, trig.SQL)
		}
	}

	// Step 9: FK check.
	stmts = append(stmts, "PRAGMA foreign_key_check("+quoteID(desired.Name)+")")

	// Step 10: Release savepoint.
	stmts = append(stmts, "RELEASE SAVEPOINT sqlift_rebuild")

	// Step 11: Re-enable foreign keys.
	stmts = append(stmts, "PRAGMA foreign_keys=ON")

	return stmts
}

// describeTableChanges produces a human-readable summary of what changed
// between two versions of a table.
func describeTableChanges(current, desired Table) string {
	var b strings.Builder
	b.WriteString("Rebuild table ")
	b.WriteString(desired.Name)
	b.WriteByte(':')

	currentCols := make(map[string]Column, len(current.Columns))
	desiredCols := make(map[string]Column, len(desired.Columns))
	currentColNames := make(map[string]bool, len(current.Columns))
	desiredColNames := make(map[string]bool, len(desired.Columns))
	for _, c := range current.Columns {
		currentCols[c.Name] = c
		currentColNames[c.Name] = true
	}
	for _, c := range desired.Columns {
		desiredCols[c.Name] = c
		desiredColNames[c.Name] = true
	}

	// Added columns.
	for name := range desiredColNames {
		if !currentColNames[name] {
			b.WriteString(" add column ")
			b.WriteString(name)
			b.WriteByte(';')
		}
	}
	// Removed columns.
	for name := range currentColNames {
		if !desiredColNames[name] {
			b.WriteString(" drop column ")
			b.WriteString(name)
			b.WriteByte(';')
		}
	}
	// Modified columns.
	for name := range currentColNames {
		if desiredColNames[name] {
			c := currentCols[name]
			d := desiredCols[name]
			if !c.Equal(d) {
				b.WriteString(" modify column ")
				b.WriteString(name)
				b.WriteByte(';')
			}
		}
	}

	// Structural property changes.
	if !fkSliceEqual(current.ForeignKeys, desired.ForeignKeys) {
		b.WriteString(" foreign keys changed;")
	}
	if !chkSliceEqual(current.CheckConstraints, desired.CheckConstraints) {
		b.WriteString(" CHECK constraints changed;")
	}
	if current.WithoutRowid != desired.WithoutRowid {
		b.WriteString(" WITHOUT ROWID changed;")
	}
	if current.Strict != desired.Strict {
		b.WriteString(" STRICT changed;")
	}

	return b.String()
}

// rebuildIsDestructive reports whether the rebuild drops any existing columns.
func rebuildIsDestructive(current, desired Table) bool {
	desiredCols := make(map[string]bool, len(desired.Columns))
	for _, c := range desired.Columns {
		desiredCols[c.Name] = true
	}
	for _, c := range current.Columns {
		if !desiredCols[c.Name] {
			return true
		}
	}
	return false
}

// fkSliceEqual reports whether two foreign key slices are element-wise equal
// (using structural equality).
func fkSliceEqual(a, b []ForeignKey) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}

// chkSliceEqual reports whether two check constraint slices are element-wise
// equal.
func chkSliceEqual(a, b []CheckConstraint) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}
