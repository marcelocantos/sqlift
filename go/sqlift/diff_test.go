// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"errors"
	"testing"
)

func TestDiff(t *testing.T) {
	t.Run("diff identical schemas", func(t *testing.T) {
		sql := "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
		a := mustParse(t, sql)
		b := mustParse(t, sql)
		plan := mustDiff(t, a, b)
		if !plan.Empty() {
			t.Errorf("expected empty plan, got %d operations", len(plan.Operations()))
		}
	})

	t.Run("diff add table", func(t *testing.T) {
		desired := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		plan := mustDiff(t, Schema{}, desired)
		ops := plan.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Type != CreateTable {
			t.Errorf("expected CreateTable, got %v", ops[0].Type)
		}
		if ops[0].ObjectName != "users" {
			t.Errorf("expected object name 'users', got %q", ops[0].ObjectName)
		}
		if plan.HasDestructiveOperations() {
			t.Error("expected no destructive operations")
		}
	})

	t.Run("diff drop table", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY);")
		plan := mustDiff(t, current, Schema{})
		ops := plan.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Type != DropTable {
			t.Errorf("expected DropTable, got %v", ops[0].Type)
		}
		if !ops[0].Destructive {
			t.Error("expected destructive operation")
		}
	})

	t.Run("diff add column - simple append", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		desired := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);")
		plan := mustDiff(t, current, desired)
		ops := plan.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Type != AddColumn {
			t.Errorf("expected AddColumn, got %v", ops[0].Type)
		}
		if ops[0].ObjectName != "users" {
			t.Errorf("expected object name 'users', got %q", ops[0].ObjectName)
		}
		if plan.HasDestructiveOperations() {
			t.Error("expected no destructive operations")
		}
	})

	t.Run("diff add NOT NULL column with default - simple append", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY);")
		desired := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER NOT NULL DEFAULT 1);")
		plan := mustDiff(t, current, desired)
		ops := plan.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Type != AddColumn {
			t.Errorf("expected AddColumn, got %v", ops[0].Type)
		}
	})

	t.Run("diff add NOT NULL column without default - breaking change", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY);")
		desired := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")
		_, err := Diff(current, desired)
		var breakErr *BreakingChangeError
		if !errors.As(err, &breakErr) {
			t.Fatal("expected BreakingChangeError")
		}
	})

	t.Run("diff remove column", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);")
		desired := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		plan := mustDiff(t, current, desired)
		ops := plan.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Type != RebuildTable {
			t.Errorf("expected RebuildTable, got %v", ops[0].Type)
		}
		if !ops[0].Destructive {
			t.Error("expected destructive operation")
		}
	})

	t.Run("diff change column type", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, age TEXT);")
		desired := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, age INTEGER);")
		plan := mustDiff(t, current, desired)
		ops := plan.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Type != RebuildTable {
			t.Errorf("expected RebuildTable, got %v", ops[0].Type)
		}
	})

	t.Run("diff change column nullability - breaking change", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		desired := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")
		_, err := Diff(current, desired)
		var breakErr *BreakingChangeError
		if !errors.As(err, &breakErr) {
			t.Fatal("expected BreakingChangeError")
		}
	})

	t.Run("diff add index", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);")
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);"+
				"CREATE INDEX idx_email ON users(email);")
		plan := mustDiff(t, current, desired)
		ops := plan.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Type != CreateIndex {
			t.Errorf("expected CreateIndex, got %v", ops[0].Type)
		}
	})

	t.Run("diff drop index", func(t *testing.T) {
		current := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);"+
				"CREATE INDEX idx_email ON users(email);")
		desired := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);")
		plan := mustDiff(t, current, desired)
		ops := plan.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Type != DropIndex {
			t.Errorf("expected DropIndex, got %v", ops[0].Type)
		}
		if !ops[0].Destructive {
			t.Error("expected destructive operation")
		}
	})

	t.Run("diff add view", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"+
				"CREATE VIEW all_users AS SELECT * FROM users;")
		plan := mustDiff(t, current, desired)
		ops := plan.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Type != CreateView {
			t.Errorf("expected CreateView, got %v", ops[0].Type)
		}
	})

	t.Run("diff change view", func(t *testing.T) {
		current := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, active INTEGER);"+
				"CREATE VIEW all_users AS SELECT * FROM users;")
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, active INTEGER);"+
				"CREATE VIEW all_users AS SELECT * FROM users WHERE active = 1;")
		plan := mustDiff(t, current, desired)
		hasDrop, hasCreate := false, false
		for _, op := range plan.Operations() {
			if op.Type == DropView {
				hasDrop = true
			}
			if op.Type == CreateView {
				hasCreate = true
			}
		}
		if !hasDrop {
			t.Error("expected DropView operation")
		}
		if !hasCreate {
			t.Error("expected CreateView operation")
		}
	})

	t.Run("diff add trigger", func(t *testing.T) {
		current := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY);"+
				"CREATE TABLE log (msg TEXT);")
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY);"+
				"CREATE TABLE log (msg TEXT);"+
				"CREATE TRIGGER on_insert AFTER INSERT ON users "+
				"BEGIN INSERT INTO log VALUES ('added'); END;")
		plan := mustDiff(t, current, desired)
		ops := plan.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Type != CreateTrigger {
			t.Errorf("expected CreateTrigger, got %v", ops[0].Type)
		}
	})

	t.Run("diff rejects nullable to NOT NULL on existing column", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		desired := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")
		_, err := Diff(current, desired)
		var breakErr *BreakingChangeError
		if !errors.As(err, &breakErr) {
			t.Fatal("expected BreakingChangeError")
		}
	})

	t.Run("diff rejects nullable to NOT NULL even with DEFAULT", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		desired := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL DEFAULT 'unknown');")
		_, err := Diff(current, desired)
		var breakErr *BreakingChangeError
		if !errors.As(err, &breakErr) {
			t.Fatal("expected BreakingChangeError")
		}
	})

	t.Run("diff rejects adding FK to existing table", func(t *testing.T) {
		current := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY);"+
				"CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER);")
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY);"+
				"CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));")
		_, err := Diff(current, desired)
		var breakErr *BreakingChangeError
		if !errors.As(err, &breakErr) {
			t.Fatal("expected BreakingChangeError")
		}
	})

	t.Run("diff rejects new NOT NULL column without DEFAULT", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY);")
		desired := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")
		_, err := Diff(current, desired)
		var breakErr *BreakingChangeError
		if !errors.As(err, &breakErr) {
			t.Fatal("expected BreakingChangeError")
		}
	})

	t.Run("diff allows new NOT NULL column with DEFAULT via AddColumn", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY);")
		desired := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER NOT NULL DEFAULT 1);")
		plan := mustDiff(t, current, desired)
		ops := plan.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Type != AddColumn {
			t.Errorf("expected AddColumn, got %v", ops[0].Type)
		}
	})

	t.Run("diff allows new table with NOT NULL columns and FKs", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY);")
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY);"+
				"CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id));")
		plan := mustDiff(t, current, desired)
		hasCreate := false
		for _, op := range plan.Operations() {
			if op.Type == CreateTable && op.ObjectName == "orders" {
				hasCreate = true
			}
		}
		if !hasCreate {
			t.Error("expected CreateTable for 'orders'")
		}
	})

	t.Run("diff destructive guard", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY);")
		plan := mustDiff(t, current, Schema{})
		if !plan.HasDestructiveOperations() {
			t.Error("expected destructive operations")
		}
	})

	t.Run("diff rejects adding CHECK constraint to existing table", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE items (id INTEGER PRIMARY KEY, price REAL);")
		desired := mustParse(t, "CREATE TABLE items (id INTEGER PRIMARY KEY, price REAL, CHECK (price > 0));")
		_, err := Diff(current, desired)
		var breakErr *BreakingChangeError
		if !errors.As(err, &breakErr) {
			t.Fatal("expected BreakingChangeError")
		}
	})

	t.Run("diff COLLATE change triggers rebuild", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		desired := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT COLLATE NOCASE);")
		plan := mustDiff(t, current, desired)
		ops := plan.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Type != RebuildTable {
			t.Errorf("expected RebuildTable, got %v", ops[0].Type)
		}
	})

	t.Run("diff GENERATED column change triggers rebuild", func(t *testing.T) {
		current := mustParse(t,
			"CREATE TABLE people ("+
				"  id INTEGER PRIMARY KEY,"+
				"  first_name TEXT,"+
				"  last_name TEXT,"+
				"  full_name TEXT GENERATED ALWAYS AS (first_name || ' ' || last_name) STORED"+
				");")
		desired := mustParse(t,
			"CREATE TABLE people ("+
				"  id INTEGER PRIMARY KEY,"+
				"  first_name TEXT,"+
				"  last_name TEXT,"+
				"  full_name TEXT GENERATED ALWAYS AS (last_name || ', ' || first_name) STORED"+
				");")
		plan := mustDiff(t, current, desired)
		ops := plan.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Type != RebuildTable {
			t.Errorf("expected RebuildTable, got %v", ops[0].Type)
		}
	})

	t.Run("diff STRICT change triggers rebuild", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE data (id INTEGER PRIMARY KEY, value TEXT);")
		desired := mustParse(t, "CREATE TABLE data (id INTEGER PRIMARY KEY, value TEXT) STRICT;")
		plan := mustDiff(t, current, desired)
		ops := plan.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Type != RebuildTable {
			t.Errorf("expected RebuildTable, got %v", ops[0].Type)
		}
	})

	t.Run("diff partial index", func(t *testing.T) {
		current := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER, name TEXT);"+
				"CREATE INDEX idx_active ON users(name) WHERE active = 1;")
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER, name TEXT);"+
				"CREATE INDEX idx_active ON users(name) WHERE active = 0;")
		plan := mustDiff(t, current, desired)
		hasDrop, hasCreate := false, false
		for _, op := range plan.Operations() {
			if op.Type == DropIndex && op.ObjectName == "idx_active" {
				hasDrop = true
			}
			if op.Type == CreateIndex && op.ObjectName == "idx_active" {
				hasCreate = true
			}
		}
		if !hasDrop {
			t.Error("expected DropIndex for 'idx_active'")
		}
		if !hasCreate {
			t.Error("expected CreateIndex for 'idx_active'")
		}
	})

	t.Run("diff expression index added", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"+
				"CREATE INDEX idx_name_len ON users(length(name));")
		plan := mustDiff(t, current, desired)
		ops := plan.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Type != CreateIndex {
			t.Errorf("expected CreateIndex, got %v", ops[0].Type)
		}
		if ops[0].ObjectName != "idx_name_len" {
			t.Errorf("expected object name 'idx_name_len', got %q", ops[0].ObjectName)
		}
	})

	t.Run("diff partial index extracted correctly", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER, name TEXT);"+
				"CREATE INDEX idx_active ON users(name) WHERE active = 1;")
		idx, ok := s.Indexes["idx_active"]
		if !ok {
			t.Fatal("index 'idx_active' not found in schema")
		}
		if idx.WhereClause != "active = 1" {
			t.Errorf("expected where_clause 'active = 1', got %q", idx.WhereClause)
		}
		if len(idx.Columns) != 1 || idx.Columns[0] != "name" {
			t.Errorf("expected columns [name], got %v", idx.Columns)
		}
	})

	t.Run("diff expression index uses expr placeholder", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"+
				"CREATE INDEX idx_name_len ON users(length(name));")
		idx, ok := s.Indexes["idx_name_len"]
		if !ok {
			t.Fatal("index 'idx_name_len' not found in schema")
		}
		if len(idx.Columns) != 1 || idx.Columns[0] != "<expr>" {
			t.Errorf("expected columns [<expr>], got %v", idx.Columns)
		}
	})

	t.Run("diff view dependency ordering", func(t *testing.T) {
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"+
				"CREATE VIEW base_users AS SELECT id, name FROM users;"+
				"CREATE VIEW active_users AS SELECT * FROM base_users;")
		plan := mustDiff(t, Schema{}, desired)
		basePos, activePos := -1, -1
		for i, op := range plan.Operations() {
			if op.Type == CreateView {
				switch op.ObjectName {
				case "base_users":
					basePos = i
				case "active_users":
					activePos = i
				}
			}
		}
		if basePos < 0 {
			t.Fatal("CreateView for 'base_users' not found")
		}
		if activePos < 0 {
			t.Fatal("CreateView for 'active_users' not found")
		}
		if basePos >= activePos {
			t.Errorf("expected base_users (pos %d) created before active_users (pos %d)", basePos, activePos)
		}
	})

	t.Run("diff view dependency ordering - drops", func(t *testing.T) {
		current := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"+
				"CREATE VIEW base_users AS SELECT id, name FROM users;"+
				"CREATE VIEW active_users AS SELECT * FROM base_users;")
		desired := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		plan := mustDiff(t, current, desired)
		basePos, activePos := -1, -1
		for i, op := range plan.Operations() {
			if op.Type == DropView {
				switch op.ObjectName {
				case "base_users":
					basePos = i
				case "active_users":
					activePos = i
				}
			}
		}
		if basePos < 0 {
			t.Fatal("DropView for 'base_users' not found")
		}
		if activePos < 0 {
			t.Fatal("DropView for 'active_users' not found")
		}
		// active_users (dependent) must be dropped before base_users (dependency)
		if activePos >= basePos {
			t.Errorf("expected active_users (pos %d) dropped before base_users (pos %d)", activePos, basePos)
		}
	})

	t.Run("diff trigger dependency ordering - creates", func(t *testing.T) {
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"+
				"CREATE TABLE log (msg TEXT);"+
				"CREATE TABLE audit (entry TEXT);"+
				"CREATE TRIGGER log_insert AFTER INSERT ON log "+
				"BEGIN INSERT INTO audit (entry) VALUES (NEW.msg); END;"+
				"CREATE TRIGGER on_user_insert AFTER INSERT ON users "+
				"BEGIN INSERT INTO log (msg) VALUES (NEW.name); END;")
		plan := mustDiff(t, Schema{}, desired)
		logPos, userPos := -1, -1
		for i, op := range plan.Operations() {
			if op.Type == CreateTrigger {
				switch op.ObjectName {
				case "log_insert":
					logPos = i
				case "on_user_insert":
					userPos = i
				}
			}
		}
		if logPos < 0 {
			t.Fatal("CreateTrigger for 'log_insert' not found")
		}
		if userPos < 0 {
			t.Fatal("CreateTrigger for 'on_user_insert' not found")
		}
		if logPos == userPos {
			t.Error("expected log_insert and on_user_insert at different positions")
		}
	})

	t.Run("diff trigger dependency ordering - drops", func(t *testing.T) {
		current := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"+
				"CREATE TABLE log (msg TEXT);"+
				"CREATE TABLE audit (entry TEXT);"+
				"CREATE TRIGGER log_insert AFTER INSERT ON log "+
				"BEGIN INSERT INTO audit (entry) VALUES (NEW.msg); END;"+
				"CREATE TRIGGER on_user_insert AFTER INSERT ON users "+
				"BEGIN INSERT INTO log (msg) VALUES (NEW.name); END;")
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"+
				"CREATE TABLE log (msg TEXT);"+
				"CREATE TABLE audit (entry TEXT);")
		plan := mustDiff(t, current, desired)
		logPos, userPos := -1, -1
		for i, op := range plan.Operations() {
			if op.Type == DropTrigger {
				switch op.ObjectName {
				case "log_insert":
					logPos = i
				case "on_user_insert":
					userPos = i
				}
			}
		}
		if logPos < 0 {
			t.Fatal("DropTrigger for 'log_insert' not found")
		}
		if userPos < 0 {
			t.Fatal("DropTrigger for 'on_user_insert' not found")
		}
		if logPos == userPos {
			t.Error("expected log_insert and on_user_insert at different positions")
		}
	})
}
