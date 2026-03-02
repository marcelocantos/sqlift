// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"errors"
	"testing"
)

func TestRoundtrip(t *testing.T) {
	t.Run("roundtrip: empty to schema", func(t *testing.T) {
		const ddl = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT);" +
			"CREATE INDEX idx_email ON users(email);"

		db := openMemory(t)
		desired := mustParse(t, ddl)
		current := mustExtract(t, db)
		plan := mustDiff(t, current, desired)
		mustApply(t, db, plan)

		result := mustExtract(t, db)

		if len(result.Tables) != 1 {
			t.Fatalf("expected 1 table, got %d", len(result.Tables))
		}
		if len(result.Indexes) != 1 {
			t.Fatalf("expected 1 index, got %d", len(result.Indexes))
		}

		tbl, ok := result.Tables["users"]
		if !ok {
			t.Fatal("expected table 'users' not found")
		}
		if len(tbl.Columns) != 3 {
			t.Fatalf("expected 3 columns, got %d", len(tbl.Columns))
		}

		// id: INTEGER PRIMARY KEY
		if tbl.Columns[0].Name != "id" {
			t.Errorf("col[0].Name: got %q, want %q", tbl.Columns[0].Name, "id")
		}
		if tbl.Columns[0].Type != "INTEGER" {
			t.Errorf("col[0].Type: got %q, want %q", tbl.Columns[0].Type, "INTEGER")
		}
		if tbl.Columns[0].PK != 1 {
			t.Errorf("col[0].PK: got %d, want 1", tbl.Columns[0].PK)
		}

		// name: TEXT NOT NULL
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

		// email: TEXT (nullable)
		if tbl.Columns[2].Name != "email" {
			t.Errorf("col[2].Name: got %q, want %q", tbl.Columns[2].Name, "email")
		}
		if tbl.Columns[2].Type != "TEXT" {
			t.Errorf("col[2].Type: got %q, want %q", tbl.Columns[2].Type, "TEXT")
		}
		if tbl.Columns[2].NotNull {
			t.Errorf("col[2].NotNull: got true, want false")
		}
		if tbl.Columns[2].PK != 0 {
			t.Errorf("col[2].PK: got %d, want 0", tbl.Columns[2].PK)
		}
	})

	t.Run("roundtrip: idempotent apply", func(t *testing.T) {
		const ddl = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"

		db := openMemory(t)
		desired := mustParse(t, ddl)

		// First apply.
		current := mustExtract(t, db)
		plan := mustDiff(t, current, desired)
		mustApply(t, db, plan)

		// Second diff should be empty.
		after := mustExtract(t, db)
		plan2 := mustDiff(t, after, desired)
		if !plan2.Empty() {
			t.Errorf("expected empty plan on second diff, got %d operation(s)", len(plan2.Operations()))
		}
	})

	t.Run("roundtrip: v1 to v2 migration", func(t *testing.T) {
		const v1 = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
		const v2 = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);" +
			"CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id), title TEXT NOT NULL);"

		db := openMemory(t)

		// Apply v1.
		desired1 := mustParse(t, v1)
		empty := mustExtract(t, db)
		plan1 := mustDiff(t, empty, desired1)
		mustApply(t, db, plan1)

		// Insert data.
		mustExec(t, db, "INSERT INTO users VALUES (1, 'Alice')")

		// Apply v2.
		desired2 := mustParse(t, v2)
		current2 := mustExtract(t, db)
		plan2 := mustDiff(t, current2, desired2)
		mustApply(t, db, plan2)

		// Verify Alice is still in users.
		name, err := db.QueryText("SELECT name FROM users WHERE id = 1")
		if err != nil {
			t.Fatalf("query for Alice failed: %v", err)
		}
		if name != "Alice" {
			t.Errorf("expected name == 'Alice', got %q", name)
		}

		// Verify posts table exists.
		result := mustExtract(t, db)
		if _, ok := result.Tables["posts"]; !ok {
			t.Error("expected table 'posts' not found")
		}

		// Verify users has 3 columns.
		users, ok := result.Tables["users"]
		if !ok {
			t.Fatal("expected table 'users' not found")
		}
		if len(users.Columns) != 3 {
			t.Errorf("expected 3 columns on users, got %d", len(users.Columns))
		}

		// Second diff of v2 should be empty (idempotent).
		plan3 := mustDiff(t, result, desired2)
		if !plan3.Empty() {
			t.Errorf("expected empty plan on idempotent v2 diff, got %d operation(s)", len(plan3.Operations()))
		}
	})

	t.Run("roundtrip: v1 to v2 to v3 breaking change rejected", func(t *testing.T) {
		const v1 = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
		const v2 = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);"
		const v3 = "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL);"

		db := openMemory(t)

		// Apply v1.
		desired1 := mustParse(t, v1)
		empty := mustExtract(t, db)
		plan1 := mustDiff(t, empty, desired1)
		mustApply(t, db, plan1)

		// Insert data.
		mustExec(t, db, "INSERT INTO users VALUES (1, 'Alice')")

		// Apply v2.
		desired2 := mustParse(t, v2)
		current2 := mustExtract(t, db)
		plan2 := mustDiff(t, current2, desired2)
		mustApply(t, db, plan2)

		// Diff to v3 should fail with BreakingChangeError.
		desired3 := mustParse(t, v3)
		current3 := mustExtract(t, db)
		_, err := Diff(current3, desired3)
		if err == nil {
			t.Fatal("expected BreakingChangeError, got nil")
		}
		var bce *BreakingChangeError
		if !errors.As(err, &bce) {
			t.Errorf("expected *BreakingChangeError, got %T: %v", err, err)
		}
	})
}
