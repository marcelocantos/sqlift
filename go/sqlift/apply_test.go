// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"errors"
	"strings"
	"testing"
)

func TestApply(t *testing.T) {
	t.Run("apply create table", func(t *testing.T) {
		db := openMemory(t)
		empty := mustExtract(t, db)
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")
		plan := mustDiff(t, empty, desired)
		mustApply(t, db, plan)

		after := mustExtract(t, db)
		tbl, ok := after.Tables["users"]
		if !ok {
			t.Fatal("table 'users' not found after apply")
		}
		if len(tbl.Columns) != 2 {
			t.Errorf("expected 2 columns, got %d", len(tbl.Columns))
		}
	})

	t.Run("apply add column", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")
		mustExec(t, db, "INSERT INTO users VALUES (1, 'Alice');")

		current := mustExtract(t, db)
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT);")
		plan := mustDiff(t, current, desired)
		mustApply(t, db, plan)

		after := mustExtract(t, db)
		tbl, ok := after.Tables["users"]
		if !ok {
			t.Fatal("table 'users' not found after apply")
		}
		if len(tbl.Columns) != 3 {
			t.Errorf("expected 3 columns, got %d", len(tbl.Columns))
		}

		name, err := db.QueryText("SELECT name FROM users WHERE id=1")
		if err != nil {
			t.Fatalf("failed to query users: %v", err)
		}
		if name != "Alice" {
			t.Errorf("expected name 'Alice', got %q", name)
		}
	})

	t.Run("apply rebuild table - change column type", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, age TEXT);")
		mustExec(t, db, "INSERT INTO users VALUES (1, '30');")

		current := mustExtract(t, db)
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, age INTEGER);")
		plan := mustDiff(t, current, desired)
		mustApply(t, db, plan)

		after := mustExtract(t, db)
		tbl, ok := after.Tables["users"]
		if !ok {
			t.Fatal("table 'users' not found after apply")
		}
		if len(tbl.Columns) != 2 {
			t.Errorf("expected 2 columns, got %d", len(tbl.Columns))
		}
		if tbl.Columns[1].Type != "INTEGER" {
			t.Errorf("expected age column type 'INTEGER', got %q", tbl.Columns[1].Type)
		}

		age, err := db.QueryInt64("SELECT age FROM users WHERE id=1")
		if err != nil {
			t.Fatalf("failed to query users: %v", err)
		}
		if age != 30 {
			t.Errorf("expected age 30, got %d", age)
		}
	})

	t.Run("apply refuses destructive without flag", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")

		current := mustExtract(t, db)
		desired := mustParse(t, "") // empty schema — drops users
		plan := mustDiff(t, current, desired)

		err := Apply(db, plan, ApplyOptions{})
		if err == nil {
			t.Fatal("expected DestructiveError, got nil")
		}
		var de *DestructiveError
		if !errors.As(err, &de) {
			t.Errorf("expected *DestructiveError, got %T: %v", err, err)
		}
	})

	t.Run("apply destructive with flag", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")

		current := mustExtract(t, db)
		desired := mustParse(t, "") // empty schema — drops users
		plan := mustDiff(t, current, desired)
		mustApply(t, db, plan, ApplyOptions{Allow: AllowAll})

		after := mustExtract(t, db)
		if len(after.Tables) != 0 {
			t.Errorf("expected 0 tables after destructive apply, got %d", len(after.Tables))
		}
	})

	t.Run("apply refuses rebuild without flag", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")

		// Column type change forces a rebuild but is not destructive.
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name BLOB);")
		current := mustExtract(t, db)
		plan := mustDiff(t, current, desired)

		err := Apply(db, plan, ApplyOptions{}) // strictest defaults
		if err == nil {
			t.Fatal("expected RebuildError, got nil")
		}
		var re *RebuildError
		if !errors.As(err, &re) {
			t.Errorf("expected *RebuildError, got %T: %v", err, err)
		}
	})

	t.Run("apply rebuild with flag", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		mustExec(t, db, "INSERT INTO users (id, name) VALUES (1, 'Alice');")

		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name BLOB);")
		current := mustExtract(t, db)
		plan := mustDiff(t, current, desired)

		mustApply(t, db, plan, ApplyOptions{Allow: AllowRebuild})

		n, err := db.QueryInt64("SELECT COUNT(*) FROM users")
		if err != nil {
			t.Fatalf("QueryInt64: %v", err)
		}
		if n != 1 {
			t.Errorf("expected 1 row preserved through rebuild, got %d", n)
		}
	})

	t.Run("apply pure addition succeeds with strictest defaults", func(t *testing.T) {
		db := openMemory(t)
		desired := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY);")
		empty := mustExtract(t, db)
		plan := mustDiff(t, empty, desired)

		// AllowNone = no rebuild, no destructive. Pure CREATE TABLE works.
		if err := Apply(db, plan, ApplyOptions{}); err != nil {
			t.Fatalf("Apply with strict defaults failed: %v", err)
		}
	})

	t.Run("apply rejects destructive even with allow_rebuild", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE users (id INTEGER PRIMARY KEY);")

		current := mustExtract(t, db)
		desired := mustParse(t, "")
		plan := mustDiff(t, current, desired)

		err := Apply(db, plan, ApplyOptions{Allow: AllowRebuild})
		if err == nil {
			t.Fatal("expected DestructiveError, got nil")
		}
		var de *DestructiveError
		if !errors.As(err, &de) {
			t.Errorf("expected *DestructiveError, got %T: %v", err, err)
		}
	})

	t.Run("apply rejects data-dependent change without flag", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")

		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")
		current := mustExtract(t, db)
		plan := mustDiff(t, current, desired)

		if !hasDataDependent(plan) {
			t.Fatal("expected data-dependent op in plan")
		}
		err := Apply(db, plan, ApplyOptions{Allow: AllowRebuild})
		var bce *BreakingChangeError
		if !errors.As(err, &bce) {
			t.Errorf("expected *BreakingChangeError, got %T: %v", err, err)
		}
	})

	t.Run("apply data-dependent on empty table with flag succeeds", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		// Empty table -- the tightening will succeed.

		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")
		current := mustExtract(t, db)
		plan := mustDiff(t, current, desired)

		mustApply(t, db, plan, ApplyOptions{Allow: AllowRebuild | AllowDataDependent})

		after := mustExtract(t, db)
		users, ok := after.Tables["users"]
		if !ok {
			t.Fatal("missing users table after apply")
		}
		nameNotNull := false
		for _, col := range users.Columns {
			if col.Name == "name" && col.NotNull {
				nameNotNull = true
			}
		}
		if !nameNotNull {
			t.Error("expected name to be NOT NULL after apply")
		}
	})

	t.Run("apply pure-loosen rebuild with allow_loosen succeeds", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db,
			"CREATE TABLE items (id INTEGER PRIMARY KEY, price REAL,"+
				" CHECK (price > 0));")
		mustExec(t, db, "INSERT INTO items (id, price) VALUES (1, 9.99);")

		// Drop the CHECK -- pure loosening.
		desired := mustParse(t,
			"CREATE TABLE items (id INTEGER PRIMARY KEY, price REAL);")
		current := mustExtract(t, db)
		plan := mustDiff(t, current, desired)

		if !hasLoosensOnly(plan) {
			t.Fatal("expected loosens_only op in plan")
		}
		// allow_loosen alone -- no allow_rebuild needed.
		mustApply(t, db, plan, ApplyOptions{Allow: AllowLoosen})

		n, err := db.QueryInt64("SELECT COUNT(*) FROM items")
		if err != nil {
			t.Fatalf("QueryInt64: %v", err)
		}
		if n != 1 {
			t.Errorf("expected 1 row preserved, got %d", n)
		}
	})

	t.Run("apply mixed rebuild rejected by allow_loosen alone", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")

		// Column type change -- not pure loosening.
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name BLOB);")
		current := mustExtract(t, db)
		plan := mustDiff(t, current, desired)

		if hasLoosensOnly(plan) {
			t.Fatal("type change should not be flagged loosens_only")
		}
		err := Apply(db, plan, ApplyOptions{Allow: AllowLoosen})
		var re *RebuildError
		if !errors.As(err, &re) {
			t.Errorf("expected *RebuildError, got %T: %v", err, err)
		}
	})

	t.Run("apply updates state hash", func(t *testing.T) {
		db := openMemory(t)
		empty := mustExtract(t, db)
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")
		plan := mustDiff(t, empty, desired)
		mustApply(t, db, plan)

		hash, err := db.QueryText(
			"SELECT value FROM _sqlift_state WHERE key='schema_hash'")
		if err != nil {
			t.Fatalf("failed to read schema_hash: %v", err)
		}
		if hash == "" {
			t.Error("expected non-empty schema_hash after apply")
		}
	})

	t.Run("apply FK violation includes parent table and rowid", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "PRAGMA foreign_keys=OFF")
		mustExec(t, db,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		mustExec(t, db,
			"CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));")
		mustExec(t, db, "INSERT INTO users VALUES (1, 'Alice');")
		mustExec(t, db, "INSERT INTO orders VALUES (1, 1);")
		mustExec(t, db, "INSERT INTO orders VALUES (2, 999);") // orphan
		mustExec(t, db, "PRAGMA foreign_keys=ON")

		current := mustExtract(t, db)
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"+
				"CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id BIGINT REFERENCES users(id));")
		plan := mustDiff(t, current, desired)

		err := Apply(db, plan, ApplyOptions{Allow: AllowRebuild})
		if err == nil {
			t.Fatal("expected ApplyError for FK violation, got nil")
		}
		var ae *ApplyError
		if !errors.As(err, &ae) {
			t.Errorf("expected *ApplyError, got %T: %v", err, err)
		}
		msg := ae.Msg
		if !strings.Contains(msg, "orders") {
			t.Errorf("expected error message to contain 'orders', got: %s", msg)
		}
		if !strings.Contains(msg, "users") {
			t.Errorf("expected error message to contain 'users', got: %s", msg)
		}
		if !strings.Contains(msg, "rowid") {
			t.Errorf("expected error message to contain 'rowid', got: %s", msg)
		}
	})

	t.Run("apply error recovery preserves database state", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "PRAGMA foreign_keys=OFF")
		mustExec(t, db,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		mustExec(t, db,
			"CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));")
		mustExec(t, db, "INSERT INTO users VALUES (1, 'Alice');")
		mustExec(t, db, "INSERT INTO orders VALUES (1, 1);")
		mustExec(t, db, "INSERT INTO orders VALUES (2, 999);") // orphan
		mustExec(t, db, "PRAGMA foreign_keys=ON")

		current := mustExtract(t, db)
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"+
				"CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id BIGINT REFERENCES users(id));")
		plan := mustDiff(t, current, desired)

		// This should fail due to the orphan FK.
		_ = Apply(db, plan, ApplyOptions{})

		// Row count should be preserved.
		count, err := db.QueryInt64("SELECT count(*) FROM orders")
		if err != nil {
			t.Fatalf("failed to query orders: %v", err)
		}
		if count != 2 {
			t.Errorf("expected 2 rows in orders after failed apply, got %d", count)
		}

		// No temp table should remain.
		tempCount, err := db.QueryInt64(
			"SELECT count(*) FROM sqlite_master WHERE type='table' AND name LIKE '%sqlift_new%'")
		if err != nil {
			t.Fatalf("failed to query temp tables: %v", err)
		}
		if tempCount != 0 {
			t.Errorf("expected 0 temp tables, got %d", tempCount)
		}

		// FK enforcement should be ON.
		fkVal, err := db.QueryInt64("PRAGMA foreign_keys")
		if err != nil {
			t.Fatalf("failed to query PRAGMA foreign_keys: %v", err)
		}
		if fkVal != 1 {
			t.Errorf("expected PRAGMA foreign_keys=1 after failed apply, got %d", fkVal)
		}
	})

	t.Run("apply restores FK enforcement ON after successful rebuild", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "PRAGMA foreign_keys=ON")
		mustExec(t, db,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, age TEXT);")
		mustExec(t, db, "INSERT INTO users VALUES (1, '25');")

		current := mustExtract(t, db)
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, age INTEGER);")
		plan := mustDiff(t, current, desired)
		mustApply(t, db, plan)

		fkVal, err := db.QueryInt64("PRAGMA foreign_keys")
		if err != nil {
			t.Fatalf("failed to query PRAGMA foreign_keys: %v", err)
		}
		if fkVal != 1 {
			t.Errorf("expected PRAGMA foreign_keys=1 after successful rebuild, got %d", fkVal)
		}
	})

	t.Run("schema hash is deterministic", func(t *testing.T) {
		db1 := openMemory(t)
		db2 := openMemory(t)
		ddl := "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);"
		mustExec(t, db1, ddl)
		mustExec(t, db2, ddl)
		s1 := mustExtract(t, db1)
		s2 := mustExtract(t, db2)
		if s1.Hash() != s2.Hash() {
			t.Errorf("expected identical hashes for same DDL, got %q and %q",
				s1.Hash(), s2.Hash())
		}
	})

	t.Run("schema hash differs for different schemas", func(t *testing.T) {
		db1 := openMemory(t)
		db2 := openMemory(t)
		mustExec(t, db1,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")
		mustExec(t, db2,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);")
		s1 := mustExtract(t, db1)
		s2 := mustExtract(t, db2)
		if s1.Hash() == s2.Hash() {
			t.Error("expected different hashes for different DDL, got same hash")
		}
	})

	t.Run("apply rebuilds multiple tables", func(t *testing.T) {
		db := openMemory(t)
		mustExec(t, db, "PRAGMA foreign_keys=ON")
		mustExec(t, db,
			"CREATE TABLE a (id INTEGER PRIMARY KEY, x TEXT);")
		mustExec(t, db,
			"CREATE TABLE b (id INTEGER PRIMARY KEY, y TEXT);")
		mustExec(t, db, "INSERT INTO a VALUES (1, '42');")
		mustExec(t, db, "INSERT INTO b VALUES (1, '99');")

		current := mustExtract(t, db)
		desired := mustParse(t,
			"CREATE TABLE a (id INTEGER PRIMARY KEY, x INTEGER);"+
				"CREATE TABLE b (id INTEGER PRIMARY KEY, y INTEGER);")
		plan := mustDiff(t, current, desired)
		mustApply(t, db, plan)

		// Verify data preserved.
		xVal, err := db.QueryInt64("SELECT x FROM a WHERE id=1")
		if err != nil {
			t.Fatalf("failed to query a: %v", err)
		}
		if xVal != 42 {
			t.Errorf("expected x=42, got %d", xVal)
		}

		yVal, err := db.QueryInt64("SELECT y FROM b WHERE id=1")
		if err != nil {
			t.Fatalf("failed to query b: %v", err)
		}
		if yVal != 99 {
			t.Errorf("expected y=99, got %d", yVal)
		}

		// FK enforcement should be ON.
		fkVal, err := db.QueryInt64("PRAGMA foreign_keys")
		if err != nil {
			t.Fatalf("failed to query PRAGMA foreign_keys: %v", err)
		}
		if fkVal != 1 {
			t.Errorf("expected PRAGMA foreign_keys=1 after multi-table rebuild, got %d", fkVal)
		}
	})

	t.Run("migration_version starts at 0", func(t *testing.T) {
		db := openMemory(t)
		v, err := MigrationVersion(db)
		if err != nil {
			t.Fatalf("MigrationVersion failed: %v", err)
		}
		if v != 0 {
			t.Errorf("expected migration_version=0 on fresh db, got %d", v)
		}
	})

	t.Run("migration_version increments on apply", func(t *testing.T) {
		db := openMemory(t)

		// First apply: version should become 1.
		empty := mustExtract(t, db)
		v1 := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		plan1 := mustDiff(t, empty, v1)
		mustApply(t, db, plan1)

		ver, err := MigrationVersion(db)
		if err != nil {
			t.Fatalf("MigrationVersion failed: %v", err)
		}
		if ver != 1 {
			t.Errorf("expected migration_version=1 after first apply, got %d", ver)
		}

		// Second apply: version should become 2.
		current := mustExtract(t, db)
		v2 := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);")
		plan2 := mustDiff(t, current, v2)
		mustApply(t, db, plan2)

		ver, err = MigrationVersion(db)
		if err != nil {
			t.Fatalf("MigrationVersion failed: %v", err)
		}
		if ver != 2 {
			t.Errorf("expected migration_version=2 after second apply, got %d", ver)
		}
	})

	t.Run("migration_version survives no-op apply", func(t *testing.T) {
		db := openMemory(t)

		// Apply v1.
		empty := mustExtract(t, db)
		v1 := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		plan1 := mustDiff(t, empty, v1)
		mustApply(t, db, plan1)

		ver, err := MigrationVersion(db)
		if err != nil {
			t.Fatalf("MigrationVersion failed: %v", err)
		}
		if ver != 1 {
			t.Errorf("expected migration_version=1 after first apply, got %d", ver)
		}

		// Apply same schema again — plan is empty, version must not change.
		current := mustExtract(t, db)
		sameDesired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		noop := mustDiff(t, current, sameDesired)
		mustApply(t, db, noop)

		ver, err = MigrationVersion(db)
		if err != nil {
			t.Fatalf("MigrationVersion failed: %v", err)
		}
		if ver != 1 {
			t.Errorf("expected migration_version=1 after no-op apply, got %d", ver)
		}
	})

	t.Run("apply detects drift", func(t *testing.T) {
		db := openMemory(t)

		// Apply v1.
		empty := mustExtract(t, db)
		v1 := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
		plan1 := mustDiff(t, empty, v1)
		mustApply(t, db, plan1)

		// Simulate out-of-band schema modification.
		mustExec(t, db, "ALTER TABLE users ADD COLUMN sneaky TEXT;")

		// Try to apply v2 — should detect drift.
		current := mustExtract(t, db)
		v2 := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);")
		plan2, err := Diff(current, v2)
		if err != nil {
			t.Fatalf("Diff failed: %v", err)
		}

		err = Apply(db, plan2, ApplyOptions{Allow: AllowAll})
		if err == nil {
			t.Fatal("expected DriftError, got nil")
		}
		var de *DriftError
		if !errors.As(err, &de) {
			t.Errorf("expected *DriftError, got %T: %v", err, err)
		}
	})
}
