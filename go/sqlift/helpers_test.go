// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"testing"
)

// openMemory opens a fresh :memory: SQLite database.
func openMemory(t *testing.T) *Database {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// mustParse calls Parse and fails the test on error.
func mustParse(t *testing.T, ddl string) Schema {
	t.Helper()
	s, err := Parse(ddl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	return s
}

// mustExec executes SQL on the database, failing the test on error.
func mustExec(t *testing.T, db *Database, sql string) {
	t.Helper()
	if err := db.Exec(sql); err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
}

// mustExtract calls Extract and fails the test on error.
func mustExtract(t *testing.T, db *Database) Schema {
	t.Helper()
	s, err := Extract(db)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	return s
}

// mustDiff calls Diff and fails the test on error.
func mustDiff(t *testing.T, current, desired Schema) MigrationPlan {
	t.Helper()
	plan, err := Diff(current, desired)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	return plan
}

// mustApply calls Apply and fails the test on error.
func mustApply(t *testing.T, db *Database, plan MigrationPlan, opts ...ApplyOptions) {
	t.Helper()
	opt := ApplyOptions{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	if err := Apply(db, plan, opt); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
}
