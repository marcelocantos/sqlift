// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	t.Run("parse empty string", func(t *testing.T) {
		s, err := Parse("")
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		if len(s.Tables) != 0 {
			t.Errorf("expected empty tables, got %d", len(s.Tables))
		}
		if len(s.Indexes) != 0 {
			t.Errorf("expected empty indexes, got %d", len(s.Indexes))
		}
		if len(s.Views) != 0 {
			t.Errorf("expected empty views, got %d", len(s.Views))
		}
		if len(s.Triggers) != 0 {
			t.Errorf("expected empty triggers, got %d", len(s.Triggers))
		}
	})

	t.Run("parse single table", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE users ("+
				"  id INTEGER PRIMARY KEY,"+
				"  name TEXT NOT NULL,"+
				"  email TEXT"+
				");")

		if len(s.Tables) != 1 {
			t.Fatalf("expected 1 table, got %d", len(s.Tables))
		}
		tbl, ok := s.Tables["users"]
		if !ok {
			t.Fatal("table 'users' not found")
		}
		if tbl.Name != "users" {
			t.Errorf("expected table name 'users', got %q", tbl.Name)
		}
		if len(tbl.Columns) != 3 {
			t.Fatalf("expected 3 columns, got %d", len(tbl.Columns))
		}

		if tbl.Columns[0].Name != "id" {
			t.Errorf("col[0].Name: got %q, want %q", tbl.Columns[0].Name, "id")
		}
		if tbl.Columns[0].Type != "INTEGER" {
			t.Errorf("col[0].Type: got %q, want %q", tbl.Columns[0].Type, "INTEGER")
		}
		if tbl.Columns[0].PK != 1 {
			t.Errorf("col[0].PK: got %d, want 1", tbl.Columns[0].PK)
		}

		if tbl.Columns[1].Name != "name" {
			t.Errorf("col[1].Name: got %q, want %q", tbl.Columns[1].Name, "name")
		}
		if tbl.Columns[1].Type != "TEXT" {
			t.Errorf("col[1].Type: got %q, want %q", tbl.Columns[1].Type, "TEXT")
		}
		if !tbl.Columns[1].NotNull {
			t.Errorf("col[1].NotNull: got false, want true")
		}
		if tbl.Columns[1].PK != 0 {
			t.Errorf("col[1].PK: got %d, want 0", tbl.Columns[1].PK)
		}

		if tbl.Columns[2].Name != "email" {
			t.Errorf("col[2].Name: got %q, want %q", tbl.Columns[2].Name, "email")
		}
		if tbl.Columns[2].Type != "TEXT" {
			t.Errorf("col[2].Type: got %q, want %q", tbl.Columns[2].Type, "TEXT")
		}
		if tbl.Columns[2].NotNull {
			t.Errorf("col[2].NotNull: got true, want false")
		}
	})

	t.Run("parse table with default", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE items ("+
				"  id INTEGER PRIMARY KEY,"+
				"  active INTEGER NOT NULL DEFAULT 1"+
				");")

		col := s.Tables["items"].Columns[1]
		if col.Name != "active" {
			t.Errorf("col.Name: got %q, want %q", col.Name, "active")
		}
		if !col.NotNull {
			t.Errorf("col.NotNull: got false, want true")
		}
		if col.DefaultValue != "1" {
			t.Errorf("col.DefaultValue: got %q, want %q", col.DefaultValue, "1")
		}
	})

	t.Run("parse table with foreign key", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE posts ("+
				"  id INTEGER PRIMARY KEY,"+
				"  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE"+
				");"+
				"CREATE TABLE users ("+
				"  id INTEGER PRIMARY KEY"+
				");")

		tbl, ok := s.Tables["posts"]
		if !ok {
			t.Fatal("table 'posts' not found")
		}
		if len(tbl.ForeignKeys) != 1 {
			t.Fatalf("expected 1 foreign key, got %d", len(tbl.ForeignKeys))
		}
		fk := tbl.ForeignKeys[0]
		if fk.ToTable != "users" {
			t.Errorf("fk.ToTable: got %q, want %q", fk.ToTable, "users")
		}
		if len(fk.ToColumns) != 1 || fk.ToColumns[0] != "id" {
			t.Errorf("fk.ToColumns: got %v, want [id]", fk.ToColumns)
		}
		if fk.OnDelete != "CASCADE" {
			t.Errorf("fk.OnDelete: got %q, want %q", fk.OnDelete, "CASCADE")
		}
	})

	t.Run("parse index", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);"+
				"CREATE UNIQUE INDEX idx_email ON users(email);")

		if len(s.Indexes) != 1 {
			t.Fatalf("expected 1 index, got %d", len(s.Indexes))
		}
		idx, ok := s.Indexes["idx_email"]
		if !ok {
			t.Fatal("index 'idx_email' not found")
		}
		if idx.TableName != "users" {
			t.Errorf("idx.TableName: got %q, want %q", idx.TableName, "users")
		}
		if !idx.Unique {
			t.Errorf("idx.Unique: got false, want true")
		}
		if len(idx.Columns) != 1 || idx.Columns[0] != "email" {
			t.Errorf("idx.Columns: got %v, want [email]", idx.Columns)
		}
	})

	t.Run("parse view", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"+
				"CREATE VIEW active_users AS SELECT * FROM users;")

		if len(s.Views) != 1 {
			t.Fatalf("expected 1 view, got %d", len(s.Views))
		}
		if _, ok := s.Views["active_users"]; !ok {
			t.Error("view 'active_users' not found")
		}
	})

	t.Run("parse trigger", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"+
				"CREATE TABLE log (msg TEXT);"+
				"CREATE TRIGGER on_user_insert AFTER INSERT ON users "+
				"BEGIN INSERT INTO log VALUES ('user added'); END;")

		if len(s.Triggers) != 1 {
			t.Fatalf("expected 1 trigger, got %d", len(s.Triggers))
		}
		tr, ok := s.Triggers["on_user_insert"]
		if !ok {
			t.Fatal("trigger 'on_user_insert' not found")
		}
		if tr.TableName != "users" {
			t.Errorf("tr.TableName: got %q, want %q", tr.TableName, "users")
		}
	})

	t.Run("parse invalid SQL throws ParseError", func(t *testing.T) {
		_, err := Parse("NOT VALID SQL")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("expected *ParseError, got %T: %v", err, err)
		}
	})

	t.Run("parse composite primary key", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE user_roles ("+
				"  user_id INTEGER,"+
				"  role_id INTEGER,"+
				"  PRIMARY KEY (user_id, role_id)"+
				");")

		tbl := s.Tables["user_roles"]
		if tbl.Columns[0].PK != 1 {
			t.Errorf("col[0].PK: got %d, want 1", tbl.Columns[0].PK)
		}
		if tbl.Columns[1].PK != 2 {
			t.Errorf("col[1].PK: got %d, want 2", tbl.Columns[1].PK)
		}
	})

	t.Run("parse column with COLLATE NOCASE", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE users ("+
				"  id INTEGER PRIMARY KEY,"+
				"  name TEXT COLLATE NOCASE"+
				");")

		tbl := s.Tables["users"]
		if len(tbl.Columns) != 2 {
			t.Fatalf("expected 2 columns, got %d", len(tbl.Columns))
		}
		if tbl.Columns[1].Name != "name" {
			t.Errorf("col[1].Name: got %q, want %q", tbl.Columns[1].Name, "name")
		}
		if tbl.Columns[1].Collation != "NOCASE" {
			t.Errorf("col[1].Collation: got %q, want %q", tbl.Columns[1].Collation, "NOCASE")
		}
		if tbl.Columns[0].Collation != "" {
			t.Errorf("col[0].Collation: got %q, want empty", tbl.Columns[0].Collation)
		}
	})

	t.Run("parse table with CHECK constraint", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE items ("+
				"  id INTEGER PRIMARY KEY,"+
				"  price REAL NOT NULL,"+
				"  CHECK (price > 0)"+
				");")

		tbl := s.Tables["items"]
		if len(tbl.CheckConstraints) != 1 {
			t.Fatalf("expected 1 check constraint, got %d", len(tbl.CheckConstraints))
		}
		chk := tbl.CheckConstraints[0]
		if chk.Name != "" {
			t.Errorf("chk.Name: got %q, want empty", chk.Name)
		}
		if chk.Expression != "price > 0" {
			t.Errorf("chk.Expression: got %q, want %q", chk.Expression, "price > 0")
		}
	})

	t.Run("parse table with named CHECK constraint", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE items ("+
				"  id INTEGER PRIMARY KEY,"+
				"  price REAL NOT NULL,"+
				"  CONSTRAINT positive_price CHECK (price > 0)"+
				");")

		tbl := s.Tables["items"]
		if len(tbl.CheckConstraints) != 1 {
			t.Fatalf("expected 1 check constraint, got %d", len(tbl.CheckConstraints))
		}
		chk := tbl.CheckConstraints[0]
		if chk.Name != "positive_price" {
			t.Errorf("chk.Name: got %q, want %q", chk.Name, "positive_price")
		}
		if chk.Expression != "price > 0" {
			t.Errorf("chk.Expression: got %q, want %q", chk.Expression, "price > 0")
		}
	})

	t.Run("parse stored generated column", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE people ("+
				"  id INTEGER PRIMARY KEY,"+
				"  first_name TEXT,"+
				"  last_name TEXT,"+
				"  full_name TEXT GENERATED ALWAYS AS (first_name || ' ' || last_name) STORED"+
				");")

		tbl := s.Tables["people"]
		if len(tbl.Columns) != 4 {
			t.Fatalf("expected 4 columns, got %d", len(tbl.Columns))
		}
		col := tbl.Columns[3]
		if col.Name != "full_name" {
			t.Errorf("col.Name: got %q, want %q", col.Name, "full_name")
		}
		if col.Generated != GeneratedStored {
			t.Errorf("col.Generated: got %v, want GeneratedStored", col.Generated)
		}
		if col.GeneratedExpr != "first_name || ' ' || last_name" {
			t.Errorf("col.GeneratedExpr: got %q, want %q", col.GeneratedExpr, "first_name || ' ' || last_name")
		}
	})

	t.Run("parse virtual generated column", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE products ("+
				"  id INTEGER PRIMARY KEY,"+
				"  price REAL,"+
				"  tax REAL GENERATED ALWAYS AS (price * 0.1) VIRTUAL"+
				");")

		tbl := s.Tables["products"]
		if len(tbl.Columns) != 3 {
			t.Fatalf("expected 3 columns, got %d", len(tbl.Columns))
		}
		col := tbl.Columns[2]
		if col.Name != "tax" {
			t.Errorf("col.Name: got %q, want %q", col.Name, "tax")
		}
		if col.Generated != GeneratedVirtual {
			t.Errorf("col.Generated: got %v, want GeneratedVirtual", col.Generated)
		}
		if col.GeneratedExpr != "price * 0.1" {
			t.Errorf("col.GeneratedExpr: got %q, want %q", col.GeneratedExpr, "price * 0.1")
		}
	})

	t.Run("parse STRICT table", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE data ("+
				"  id INTEGER PRIMARY KEY,"+
				"  value TEXT NOT NULL"+
				") STRICT;")

		tbl := s.Tables["data"]
		if !tbl.Strict {
			t.Errorf("tbl.Strict: got false, want true")
		}
		if tbl.WithoutRowid {
			t.Errorf("tbl.WithoutRowid: got true, want false")
		}
	})

	t.Run("parse STRICT WITHOUT ROWID table", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE data ("+
				"  id INTEGER PRIMARY KEY,"+
				"  value TEXT NOT NULL"+
				") STRICT, WITHOUT ROWID;")

		tbl := s.Tables["data"]
		if !tbl.Strict {
			t.Errorf("tbl.Strict: got false, want true")
		}
		if !tbl.WithoutRowid {
			t.Errorf("tbl.WithoutRowid: got false, want true")
		}
	})

	t.Run("parse WITHOUT ROWID STRICT table", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE data ("+
				"  id INTEGER PRIMARY KEY,"+
				"  value TEXT NOT NULL"+
				") WITHOUT ROWID, STRICT;")

		tbl := s.Tables["data"]
		if !tbl.Strict {
			t.Errorf("tbl.Strict: got false, want true")
		}
		if !tbl.WithoutRowid {
			t.Errorf("tbl.WithoutRowid: got false, want true")
		}
	})

	t.Run("parse named PRIMARY KEY constraint", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE user_roles ("+
				"  user_id INTEGER,"+
				"  role_id INTEGER,"+
				"  CONSTRAINT pk_user_roles PRIMARY KEY (user_id, role_id)"+
				");")

		tbl := s.Tables["user_roles"]
		if tbl.PKConstraintName != "pk_user_roles" {
			t.Errorf("tbl.PKConstraintName: got %q, want %q", tbl.PKConstraintName, "pk_user_roles")
		}
		if tbl.Columns[0].PK != 1 {
			t.Errorf("col[0].PK: got %d, want 1", tbl.Columns[0].PK)
		}
		if tbl.Columns[1].PK != 2 {
			t.Errorf("col[1].PK: got %d, want 2", tbl.Columns[1].PK)
		}
	})

	t.Run("parse unnamed PRIMARY KEY constraint", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE user_roles ("+
				"  user_id INTEGER,"+
				"  role_id INTEGER,"+
				"  PRIMARY KEY (user_id, role_id)"+
				");")

		tbl := s.Tables["user_roles"]
		if tbl.PKConstraintName != "" {
			t.Errorf("tbl.PKConstraintName: got %q, want empty", tbl.PKConstraintName)
		}
	})

	t.Run("parse named FOREIGN KEY constraint", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY);"+
				"CREATE TABLE posts ("+
				"  id INTEGER PRIMARY KEY,"+
				"  user_id INTEGER,"+
				"  CONSTRAINT fk_posts_user FOREIGN KEY (user_id) REFERENCES users(id)"+
				");")

		tbl := s.Tables["posts"]
		if len(tbl.ForeignKeys) != 1 {
			t.Fatalf("expected 1 foreign key, got %d", len(tbl.ForeignKeys))
		}
		fk := tbl.ForeignKeys[0]
		if fk.ConstraintName != "fk_posts_user" {
			t.Errorf("fk.ConstraintName: got %q, want %q", fk.ConstraintName, "fk_posts_user")
		}
		if len(fk.FromColumns) != 1 || fk.FromColumns[0] != "user_id" {
			t.Errorf("fk.FromColumns: got %v, want [user_id]", fk.FromColumns)
		}
		if fk.ToTable != "users" {
			t.Errorf("fk.ToTable: got %q, want %q", fk.ToTable, "users")
		}
	})

	t.Run("parse unnamed FOREIGN KEY constraint", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY);"+
				"CREATE TABLE posts ("+
				"  id INTEGER PRIMARY KEY,"+
				"  user_id INTEGER,"+
				"  FOREIGN KEY (user_id) REFERENCES users(id)"+
				");")

		tbl := s.Tables["posts"]
		if len(tbl.ForeignKeys) != 1 {
			t.Fatalf("expected 1 foreign key, got %d", len(tbl.ForeignKeys))
		}
		if tbl.ForeignKeys[0].ConstraintName != "" {
			t.Errorf("fk.ConstraintName: got %q, want empty", tbl.ForeignKeys[0].ConstraintName)
		}
	})

	t.Run("parse named composite FOREIGN KEY constraint", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE parent (a INTEGER, b INTEGER, PRIMARY KEY (a, b));"+
				"CREATE TABLE child ("+
				"  id INTEGER PRIMARY KEY,"+
				"  pa INTEGER,"+
				"  pb INTEGER,"+
				"  CONSTRAINT fk_child_parent FOREIGN KEY (pa, pb) REFERENCES parent(a, b)"+
				");")

		tbl := s.Tables["child"]
		if len(tbl.ForeignKeys) != 1 {
			t.Fatalf("expected 1 foreign key, got %d", len(tbl.ForeignKeys))
		}
		fk := tbl.ForeignKeys[0]
		if fk.ConstraintName != "fk_child_parent" {
			t.Errorf("fk.ConstraintName: got %q, want %q", fk.ConstraintName, "fk_child_parent")
		}
		if len(fk.FromColumns) != 2 || fk.FromColumns[0] != "pa" || fk.FromColumns[1] != "pb" {
			t.Errorf("fk.FromColumns: got %v, want [pa pb]", fk.FromColumns)
		}
	})
}
