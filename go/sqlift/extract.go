// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Extract reads the schema from db and returns a Schema value.
//
// Port of C++ extract() (dist/sqlift.cpp lines 547-749). A single *sql.Conn
// is acquired for the duration so that PRAGMA results are coherent on the same
// connection.
func Extract(ctx context.Context, db *sql.DB) (Schema, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return Schema{}, &ExtractError{Msg: "failed to acquire connection: " + err.Error()}
	}
	defer conn.Close()
	return extractConn(ctx, conn)
}

// extractConn reads the schema from an already-acquired conn. It is used
// internally by Apply to avoid acquiring a second connection from the pool
// (which would deadlock when MaxOpenConns == 1).
func extractConn(ctx context.Context, conn *sql.Conn) (Schema, error) {
	// --- Query sqlite_master --------------------------------------------------

	type masterRow struct {
		typ, name, tblName, rawSQL string
	}

	rows, err := conn.QueryContext(ctx,
		"SELECT type, name, tbl_name, sql FROM sqlite_master "+
			"WHERE type IN ('table', 'index', 'view', 'trigger') "+
			"AND name NOT LIKE 'sqlite_%' "+
			"AND name != '_sqlift_state' "+
			"ORDER BY type, name")
	if err != nil {
		return Schema{}, &ExtractError{Msg: "failed to query sqlite_master: " + err.Error()}
	}

	var masterRows []masterRow
	for rows.Next() {
		var r masterRow
		var rawSQL sql.NullString
		if err := rows.Scan(&r.typ, &r.name, &r.tblName, &rawSQL); err != nil {
			rows.Close()
			return Schema{}, &ExtractError{Msg: "failed to scan sqlite_master row: " + err.Error()}
		}
		r.rawSQL = rawSQL.String
		masterRows = append(masterRows, r)
	}
	if err := rows.Close(); err != nil {
		return Schema{}, &ExtractError{Msg: "failed to close sqlite_master cursor: " + err.Error()}
	}

	// --- Process rows ---------------------------------------------------------

	schema := Schema{
		Tables:   make(map[string]Table),
		Indexes:  make(map[string]Index),
		Views:    make(map[string]View),
		Triggers: make(map[string]Trigger),
	}

	for _, row := range masterRows {
		switch row.typ {
		case "table":
			table, err := extractTable(ctx, conn, row.name, row.rawSQL)
			if err != nil {
				return Schema{}, err
			}
			schema.Tables[row.name] = table

		case "index":
			// Skip SQLite auto-indexes and indexes with no SQL (implicit PKs).
			if strings.HasPrefix(row.name, "sqlite_autoindex_") || row.rawSQL == "" {
				continue
			}
			idx, err := extractIndex(ctx, conn, row.name, row.tblName, row.rawSQL)
			if err != nil {
				return Schema{}, err
			}
			schema.Indexes[row.name] = idx

		case "view":
			schema.Views[row.name] = View{Name: row.name, SQL: row.rawSQL}

		case "trigger":
			schema.Triggers[row.name] = Trigger{
				Name:      row.name,
				TableName: row.tblName,
				SQL:       row.rawSQL,
			}
		}
	}

	return schema, nil
}

// extractTable builds a Table from PRAGMA queries.
func extractTable(ctx context.Context, conn *sql.Conn, name, rawSQL string) (Table, error) {
	table := Table{
		Name:   name,
		RawSQL: rawSQL,
	}

	table.WithoutRowid, table.Strict = parseTableOptions(rawSQL)

	// --- Columns via PRAGMA table_xinfo ---------------------------------------

	colRows, err := conn.QueryContext(ctx, "PRAGMA table_xinfo("+quoteID(name)+")")
	if err != nil {
		return Table{}, &ExtractError{Msg: fmt.Sprintf("PRAGMA table_xinfo(%q): %v", name, err)}
	}
	for colRows.Next() {
		// Columns: cid, name, type, notnull, dflt_value, pk, hidden
		var cid, notnull, pk, hidden int
		var colName, colType string
		var dfltValue sql.NullString
		if err := colRows.Scan(&cid, &colName, &colType, &notnull, &dfltValue, &pk, &hidden); err != nil {
			colRows.Close()
			return Table{}, &ExtractError{Msg: fmt.Sprintf("scanning table_xinfo row for %q: %v", name, err)}
		}
		if hidden != 0 && hidden != 2 && hidden != 3 {
			colRows.Close()
			return Table{}, &ExtractError{Msg: fmt.Sprintf("unsupported generated column type %d in table %q", hidden, name)}
		}
		col := Column{
			Name:         colName,
			Type:         toUpper(colType),
			NotNull:      notnull != 0,
			DefaultValue: dfltValue.String,
			PK:           pk,
			Generated:    GeneratedType(hidden),
		}
		table.Columns = append(table.Columns, col)
	}
	if err := colRows.Close(); err != nil {
		return Table{}, &ExtractError{Msg: fmt.Sprintf("closing table_xinfo cursor for %q: %v", name, err)}
	}

	// --- Collation from parseCreateTableBody ----------------------------------

	parsed := parseCreateTableBody(rawSQL)
	for i := range table.Columns {
		if coll, ok := parsed.collations[table.Columns[i].Name]; ok && coll != "" && coll != "BINARY" {
			table.Columns[i].Collation = coll
		}
	}

	// --- CHECK constraints and GENERATED expressions -------------------------

	table.CheckConstraints = parsed.checks
	for i := range table.Columns {
		if expr, ok := parsed.generatedExprs[table.Columns[i].Name]; ok {
			table.Columns[i].GeneratedExpr = expr
		}
	}

	// --- Foreign keys via PRAGMA foreign_key_list ----------------------------

	type fkEntry struct {
		toTable  string
		onUpdate string
		onDelete string
		fromCols []string
		toCols   []string
	}
	fkMap := make(map[int]*fkEntry)
	var fkOrder []int

	fkRows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_list("+quoteID(name)+")")
	if err != nil {
		return Table{}, &ExtractError{Msg: fmt.Sprintf("PRAGMA foreign_key_list(%q): %v", name, err)}
	}
	for fkRows.Next() {
		// Columns: id, seq, table, from, to, on_update, on_delete, match
		var id, seq int
		var toTable, fromCol, toCol, onUpdate, onDelete, match string
		if err := fkRows.Scan(&id, &seq, &toTable, &fromCol, &toCol, &onUpdate, &onDelete, &match); err != nil {
			fkRows.Close()
			return Table{}, &ExtractError{Msg: fmt.Sprintf("scanning foreign_key_list row for %q: %v", name, err)}
		}
		if seq == 0 {
			fkMap[id] = &fkEntry{
				toTable:  toTable,
				onUpdate: toUpper(onUpdate),
				onDelete: toUpper(onDelete),
			}
			fkOrder = append(fkOrder, id)
		}
		fkMap[id].fromCols = append(fkMap[id].fromCols, fromCol)
		fkMap[id].toCols = append(fkMap[id].toCols, toCol)
	}
	if err := fkRows.Close(); err != nil {
		return Table{}, &ExtractError{Msg: fmt.Sprintf("closing foreign_key_list cursor for %q: %v", name, err)}
	}

	for _, id := range fkOrder {
		e := fkMap[id]
		fk := ForeignKey{
			FromColumns: e.fromCols,
			ToTable:     e.toTable,
			ToColumns:   e.toCols,
			OnUpdate:    e.onUpdate,
			OnDelete:    e.onDelete,
		}
		// Look up constraint name from parsed body.
		key := strings.Join(fk.FromColumns, ",")
		if cname, ok := parsed.fkConstraintNames[key]; ok {
			fk.ConstraintName = cname
		}
		table.ForeignKeys = append(table.ForeignKeys, fk)
	}

	table.PKConstraintName = parsed.pkConstraintName

	return table, nil
}

// extractIndex builds an Index from PRAGMA queries.
func extractIndex(ctx context.Context, conn *sql.Conn, name, tableName, rawSQL string) (Index, error) {
	idx := Index{
		Name:      name,
		TableName: tableName,
		RawSQL:    rawSQL,
	}

	// --- Uniqueness via PRAGMA index_list ------------------------------------

	ilRows, err := conn.QueryContext(ctx, "PRAGMA index_list("+quoteID(tableName)+")")
	if err != nil {
		return Index{}, &ExtractError{Msg: fmt.Sprintf("PRAGMA index_list(%q): %v", tableName, err)}
	}
	for ilRows.Next() {
		// Columns: seq, name, unique, origin, partial
		var seq, unique, partial int
		var idxName, origin string
		if err := ilRows.Scan(&seq, &idxName, &unique, &origin, &partial); err != nil {
			ilRows.Close()
			return Index{}, &ExtractError{Msg: fmt.Sprintf("scanning index_list row for table %q: %v", tableName, err)}
		}
		if idxName == name {
			idx.Unique = unique != 0
			break
		}
	}
	if err := ilRows.Close(); err != nil {
		return Index{}, &ExtractError{Msg: fmt.Sprintf("closing index_list cursor for table %q: %v", tableName, err)}
	}

	// --- Columns via PRAGMA index_info ---------------------------------------

	iiRows, err := conn.QueryContext(ctx, "PRAGMA index_info("+quoteID(name)+")")
	if err != nil {
		return Index{}, &ExtractError{Msg: fmt.Sprintf("PRAGMA index_info(%q): %v", name, err)}
	}
	for iiRows.Next() {
		// Columns: seqno, cid, name
		var seqno, cid int
		var colName sql.NullString
		if err := iiRows.Scan(&seqno, &cid, &colName); err != nil {
			iiRows.Close()
			return Index{}, &ExtractError{Msg: fmt.Sprintf("scanning index_info row for %q: %v", name, err)}
		}
		cn := colName.String
		if cn == "" {
			cn = "<expr>"
		}
		idx.Columns = append(idx.Columns, cn)
	}
	if err := iiRows.Close(); err != nil {
		return Index{}, &ExtractError{Msg: fmt.Sprintf("closing index_info cursor for %q: %v", name, err)}
	}

	// --- WHERE clause from raw SQL -------------------------------------------

	upperSQL := toUpper(rawSQL)
	wherePos := strings.LastIndex(upperSQL, "WHERE")
	if wherePos >= 0 {
		// Verify the WHERE is at top level (not inside parens or string literals).
		parenDepth := 0
		inString := false
		var stringChar byte
		topLevel := true

		for i := 0; i < wherePos; i++ {
			c := rawSQL[i]
			if inString {
				if c == stringChar {
					if i+1 < wherePos && rawSQL[i+1] == stringChar {
						i++ // escaped quote
					} else {
						inString = false
					}
				}
			} else {
				switch c {
				case '\'', '"':
					inString = true
					stringChar = c
				case '(':
					parenDepth++
				case ')':
					parenDepth--
				}
			}
		}
		if parenDepth != 0 {
			topLevel = false
		}

		if topLevel {
			clause := strings.TrimSpace(rawSQL[wherePos+len("WHERE"):])
			idx.WhereClause = clause
		}
	}

	return idx, nil
}
