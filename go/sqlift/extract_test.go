// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"testing"
)

func TestExtract(t *testing.T) {
	t.Run("extract empty database", func(t *testing.T) {
		db := openMemory(t)
		s := mustExtract(t, db)

		if len(s.Tables) != 0 {
			t.Errorf("expected 0 tables, got %d", len(s.Tables))
		}
		if len(s.Indexes) != 0 {
			t.Errorf("expected 0 indexes, got %d", len(s.Indexes))
		}
		if len(s.Views) != 0 {
			t.Errorf("expected 0 views, got %d", len(s.Views))
		}
		if len(s.Triggers) != 0 {
			t.Errorf("expected 0 triggers, got %d", len(s.Triggers))
		}
	})

	t.Run("extract table", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")

		s := mustExtract(t, db)

		if len(s.Tables) != 1 {
			t.Fatalf("expected 1 table, got %d", len(s.Tables))
		}
		tbl, ok := s.Tables["users"]
		if !ok {
			t.Fatal("expected table 'users' not found")
		}
		if len(tbl.Columns) != 2 {
			t.Fatalf("expected 2 columns, got %d", len(tbl.Columns))
		}
		if tbl.Columns[0].Name != "id" {
			t.Errorf("expected columns[0].Name == 'id', got %q", tbl.Columns[0].Name)
		}
		if tbl.Columns[0].Type != "INTEGER" {
			t.Errorf("expected columns[0].Type == 'INTEGER', got %q", tbl.Columns[0].Type)
		}
		if tbl.Columns[0].PK != 1 {
			t.Errorf("expected columns[0].PK == 1, got %d", tbl.Columns[0].PK)
		}
		if tbl.Columns[1].Name != "name" {
			t.Errorf("expected columns[1].Name == 'name', got %q", tbl.Columns[1].Name)
		}
		if tbl.Columns[1].Type != "TEXT" {
			t.Errorf("expected columns[1].Type == 'TEXT', got %q", tbl.Columns[1].Type)
		}
		if !tbl.Columns[1].NotNull {
			t.Errorf("expected columns[1].NotNull == true")
		}
	})

	t.Run("extract excludes _sqlift_state", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE _sqlift_state (key TEXT PRIMARY KEY, value TEXT NOT NULL);")
		mustExec(t, db, "CREATE TABLE users (id INTEGER PRIMARY KEY);")

		s := mustExtract(t, db)

		if len(s.Tables) != 1 {
			t.Errorf("expected 1 table, got %d", len(s.Tables))
		}
		if _, ok := s.Tables["users"]; !ok {
			t.Error("expected table 'users' not found")
		}
		if _, ok := s.Tables["_sqlift_state"]; ok {
			t.Error("expected '_sqlift_state' to be excluded but it was present")
		}
	})

	t.Run("extract excludes sqlite_autoindex", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT UNIQUE);")

		s := mustExtract(t, db)

		if len(s.Indexes) != 0 {
			t.Errorf("expected 0 indexes (autoindex excluded), got %d", len(s.Indexes))
		}
	})

	t.Run("extract index", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);")
		mustExec(t, db, "CREATE UNIQUE INDEX idx_email ON users(email);")

		s := mustExtract(t, db)

		if len(s.Indexes) != 1 {
			t.Fatalf("expected 1 index, got %d", len(s.Indexes))
		}
		idx, ok := s.Indexes["idx_email"]
		if !ok {
			t.Fatal("expected index 'idx_email' not found")
		}
		if idx.TableName != "users" {
			t.Errorf("expected TableName == 'users', got %q", idx.TableName)
		}
		if !idx.Unique {
			t.Error("expected Unique == true")
		}
		if len(idx.Columns) != 1 || idx.Columns[0] != "email" {
			t.Errorf("expected Columns == [\"email\"], got %v", idx.Columns)
		}
	})

	t.Run("extract foreign key", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE users (id INTEGER PRIMARY KEY);")
		mustExec(t, db,
			"CREATE TABLE posts ("+
				"  id INTEGER PRIMARY KEY,"+
				"  user_id INTEGER REFERENCES users(id) ON DELETE CASCADE"+
				");")

		s := mustExtract(t, db)

		posts, ok := s.Tables["posts"]
		if !ok {
			t.Fatal("expected table 'posts' not found")
		}
		if len(posts.ForeignKeys) != 1 {
			t.Fatalf("expected 1 foreign key, got %d", len(posts.ForeignKeys))
		}
		fk := posts.ForeignKeys[0]
		if fk.ToTable != "users" {
			t.Errorf("expected ToTable == 'users', got %q", fk.ToTable)
		}
		if fk.OnDelete != "CASCADE" {
			t.Errorf("expected OnDelete == 'CASCADE', got %q", fk.OnDelete)
		}
	})

	t.Run("extract STRICT table", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE data (id INTEGER PRIMARY KEY, value TEXT NOT NULL) STRICT;")

		s := mustExtract(t, db)

		tbl, ok := s.Tables["data"]
		if !ok {
			t.Fatal("expected table 'data' not found")
		}
		if !tbl.Strict {
			t.Error("expected Strict == true")
		}
	})

	t.Run("extract WITHOUT ROWID table", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT) WITHOUT ROWID;")

		s := mustExtract(t, db)

		tbl, ok := s.Tables["kv"]
		if !ok {
			t.Fatal("expected table 'kv' not found")
		}
		if !tbl.WithoutRowid {
			t.Error("expected WithoutRowid == true")
		}
	})

	t.Run("extract GENERATED columns", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db,
			"CREATE TABLE people ("+
				"  id INTEGER PRIMARY KEY,"+
				"  first TEXT,"+
				"  last TEXT,"+
				"  full_name TEXT GENERATED ALWAYS AS (first || ' ' || last) STORED"+
				");")

		s := mustExtract(t, db)

		tbl, ok := s.Tables["people"]
		if !ok {
			t.Fatal("expected table 'people' not found")
		}
		if len(tbl.Columns) != 4 {
			t.Fatalf("expected 4 columns, got %d", len(tbl.Columns))
		}
		fullName := tbl.Columns[3]
		if fullName.Name != "full_name" {
			t.Errorf("expected columns[3].Name == 'full_name', got %q", fullName.Name)
		}
		if fullName.Generated != GeneratedStored {
			t.Errorf("expected columns[3].Generated == GeneratedStored, got %v", fullName.Generated)
		}
		if fullName.GeneratedExpr == "" {
			t.Error("expected columns[3].GeneratedExpr to be non-empty")
		}
		if tbl.Columns[0].Generated != GeneratedNormal {
			t.Errorf("expected columns[0].Generated == GeneratedNormal, got %v", tbl.Columns[0].Generated)
		}
	})

	t.Run("extract partial index", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER, email TEXT);")
		mustExec(t, db, "CREATE INDEX idx_active_email ON users(email) WHERE active = 1;")

		s := mustExtract(t, db)

		idx, ok := s.Indexes["idx_active_email"]
		if !ok {
			t.Fatal("expected index 'idx_active_email' not found")
		}
		if idx.WhereClause != "active = 1" {
			t.Errorf("expected WhereClause == 'active = 1', got %q", idx.WhereClause)
		}
	})

	t.Run("extract CHECK constraint", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE products (id INTEGER PRIMARY KEY, price REAL, CHECK(price > 0));")

		s := mustExtract(t, db)

		tbl, ok := s.Tables["products"]
		if !ok {
			t.Fatal("expected table 'products' not found")
		}
		if len(tbl.CheckConstraints) != 1 {
			t.Fatalf("expected 1 check constraint, got %d", len(tbl.CheckConstraints))
		}
		if tbl.CheckConstraints[0].Expression != "price > 0" {
			t.Errorf("expected Expression == 'price > 0', got %q", tbl.CheckConstraints[0].Expression)
		}
	})

	t.Run("extract COLLATE clause", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT COLLATE NOCASE);")

		s := mustExtract(t, db)

		tbl, ok := s.Tables["users"]
		if !ok {
			t.Fatal("expected table 'users' not found")
		}
		if tbl.Columns[1].Collation != "NOCASE" {
			t.Errorf("expected columns[1].Collation == 'NOCASE', got %q", tbl.Columns[1].Collation)
		}
		if tbl.Columns[0].Collation != "" {
			t.Errorf("expected columns[0].Collation == '', got %q", tbl.Columns[0].Collation)
		}
	})

	t.Run("extract named constraints", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE parent (id INTEGER PRIMARY KEY);")
		mustExec(t, db,
			"CREATE TABLE child ("+
				"  id INTEGER PRIMARY KEY,"+
				"  parent_id INTEGER,"+
				"  CONSTRAINT fk_parent FOREIGN KEY (parent_id) REFERENCES parent(id)"+
				");")

		s := mustExtract(t, db)

		tbl, ok := s.Tables["child"]
		if !ok {
			t.Fatal("expected table 'child' not found")
		}
		if len(tbl.ForeignKeys) != 1 {
			t.Fatalf("expected 1 foreign key, got %d", len(tbl.ForeignKeys))
		}
		if tbl.ForeignKeys[0].ConstraintName != "fk_parent" {
			t.Errorf("expected ConstraintName == 'fk_parent', got %q", tbl.ForeignKeys[0].ConstraintName)
		}
	})

	t.Run("extract view", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		mustExec(t, db, "CREATE VIEW user_names AS SELECT name FROM users;")

		s := mustExtract(t, db)

		if len(s.Views) != 1 {
			t.Fatalf("expected 1 view, got %d", len(s.Views))
		}
		v, ok := s.Views["user_names"]
		if !ok {
			t.Fatal("expected view 'user_names' not found")
		}
		if v.SQL == "" {
			t.Error("expected view SQL to be non-empty")
		}
	})

	t.Run("extract trigger", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		mustExec(t, db, "CREATE TABLE log (msg TEXT);")
		mustExec(t, db,
			"CREATE TRIGGER on_user_insert AFTER INSERT ON users "+
				"BEGIN INSERT INTO log (msg) VALUES ('inserted'); END;")

		s := mustExtract(t, db)

		if len(s.Triggers) != 1 {
			t.Fatalf("expected 1 trigger, got %d", len(s.Triggers))
		}
		tr, ok := s.Triggers["on_user_insert"]
		if !ok {
			t.Fatal("expected trigger 'on_user_insert' not found")
		}
		if tr.TableName != "users" {
			t.Errorf("expected TableName == 'users', got %q", tr.TableName)
		}
		if tr.SQL == "" {
			t.Error("expected trigger SQL to be non-empty")
		}
	})

	t.Run("extract FK ordering matches parse", func(t *testing.T) {
		ddl := "CREATE TABLE parent (id INTEGER PRIMARY KEY);" +
			"CREATE TABLE other (id INTEGER PRIMARY KEY);" +
			"CREATE TABLE child (" +
			"  id INTEGER PRIMARY KEY," +
			"  parent_id INTEGER REFERENCES parent(id)," +
			"  other_id INTEGER REFERENCES other(id)" +
			");"

		parsed := mustParse(t, ddl)

		db := openMemory(t)
		mustExec(t, db, ddl)
		extracted := mustExtract(t, db)

		pFKs := parsed.Tables["child"].ForeignKeys
		eFKs := extracted.Tables["child"].ForeignKeys

		if len(pFKs) != len(eFKs) {
			t.Fatalf("FK count mismatch: parsed=%d extracted=%d", len(pFKs), len(eFKs))
		}
		for i := range pFKs {
			if pFKs[i].ToTable != eFKs[i].ToTable {
				t.Errorf("FK[%d] ToTable mismatch: parsed=%q extracted=%q", i, pFKs[i].ToTable, eFKs[i].ToTable)
			}
			if !sliceEqual(pFKs[i].FromColumns, eFKs[i].FromColumns) {
				t.Errorf("FK[%d] FromColumns mismatch: parsed=%v extracted=%v", i, pFKs[i].FromColumns, eFKs[i].FromColumns)
			}
		}
	})

	t.Run("extract non-unique index", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);")
		mustExec(t, db, "CREATE INDEX idx_email ON users(email);")

		s := mustExtract(t, db)

		idx, ok := s.Indexes["idx_email"]
		if !ok {
			t.Fatal("expected index 'idx_email' not found")
		}
		if idx.Unique {
			t.Error("expected Unique == false")
		}
	})
}
