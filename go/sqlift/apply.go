// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Apply executes plan against db.
//
// Port of C++ apply() (dist/sqlift.cpp lines 1457-1533).
//
// Steps:
//  1. If the plan is empty, return nil immediately.
//  2. If the plan contains destructive operations and opts.AllowDestructive is
//     false, return a [DestructiveError].
//  3. Extract the current schema and load the stored hash from _sqlift_state.
//     If the stored hash is non-empty and differs from the current schema hash,
//     return a [DriftError].
//  4. Record the current PRAGMA foreign_keys state so it can be restored on
//     failure.
//  5. Execute each operation's SQL statements on a single *sql.Conn for
//     connection affinity. PRAGMA foreign_key_check statements are handled
//     specially: rows indicate violations and cause an [ApplyError].
//  6. On any error: attempt to roll back the sqlift_rebuild savepoint, release
//     it, and restore the FK enforcement state, then return the original error.
//  7. On success: extract the updated schema and store its hash.
func Apply(ctx context.Context, db *sql.DB, plan MigrationPlan, opts ApplyOptions) error {
	if plan.Empty() {
		return nil
	}

	if plan.HasDestructiveOperations() && !opts.AllowDestructive {
		return &DestructiveError{
			Msg: "Migration plan contains destructive operations. " +
				"Set AllowDestructive=true to proceed.",
		}
	}

	// Acquire a dedicated connection for the entire apply operation so that
	// PRAGMA results (foreign_keys, savepoints) are coherent.
	conn, err := db.Conn(ctx)
	if err != nil {
		return &ApplyError{Msg: "failed to acquire connection: " + err.Error()}
	}
	defer conn.Close()

	// Drift detection: extract the current schema and compare with stored hash.
	// Use extractConn so we reuse the connection we already hold (avoiding a
	// deadlock when MaxOpenConns == 1).
	current, err := extractConn(ctx, conn)
	if err != nil {
		return err
	}
	storedHash, err := loadSchemaHash(ctx, conn)
	if err != nil {
		return err
	}
	if storedHash != "" {
		actualHash := current.Hash()
		if storedHash != actualHash {
			return &DriftError{
				Msg: "Schema drift detected: the database schema has been modified " +
					"outside of sqlift. Stored hash: " + storedHash +
					", actual hash: " + actualHash,
			}
		}
	}

	// Save the current FK enforcement state so we can restore it on failure.
	var fkWasOn bool
	{
		row := conn.QueryRowContext(ctx, "PRAGMA foreign_keys")
		var fkVal int
		if err := row.Scan(&fkVal); err == nil {
			fkWasOn = fkVal != 0
		}
	}
	fkRestore := "PRAGMA foreign_keys=OFF"
	if fkWasOn {
		fkRestore = "PRAGMA foreign_keys=ON"
	}

	// Execute each operation's SQL statements.
	if err := executeOps(ctx, conn, plan); err != nil {
		// Roll back any open savepoint from a failed rebuild, then restore FK state.
		// PRAGMA foreign_keys cannot be changed inside an open transaction/savepoint,
		// so we must release the savepoint first.
		conn.ExecContext(ctx, "ROLLBACK TO SAVEPOINT sqlift_rebuild") //nolint:errcheck
		conn.ExecContext(ctx, "RELEASE SAVEPOINT sqlift_rebuild")     //nolint:errcheck
		conn.ExecContext(ctx, fkRestore)                              //nolint:errcheck
		return err
	}

	// Update stored hash with the schema after the migration.
	// Use extractConn so we reuse the connection we already hold (avoiding a
	// deadlock when MaxOpenConns == 1).
	after, err := extractConn(ctx, conn)
	if err != nil {
		return err
	}
	return storeSchemaHash(ctx, conn, after.Hash())
}

// executeOps runs the SQL statements for each operation in the plan.
func executeOps(ctx context.Context, conn *sql.Conn, plan MigrationPlan) error {
	for _, op := range plan.Operations() {
		for _, stmt := range op.SQL {
			if strings.HasPrefix(stmt, "PRAGMA foreign_key_check") {
				// This PRAGMA returns rows when there are FK violations.
				rows, err := conn.QueryContext(ctx, stmt)
				if err != nil {
					return &ApplyError{Msg: "PRAGMA foreign_key_check failed: " + err.Error()}
				}
				if rows.Next() {
					var table string
					var rowid int64
					var parent string
					var fkid int64
					if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
						rows.Close()
						return &ApplyError{Msg: "failed to scan foreign_key_check row: " + err.Error()}
					}
					rows.Close()
					return &ApplyError{
						Msg: fmt.Sprintf(
							"Foreign key violation in table %q (rowid %d): "+
								"references missing row in parent table %q",
							table, rowid, parent),
					}
				}
				if err := rows.Close(); err != nil {
					return &ApplyError{Msg: "failed to close foreign_key_check cursor: " + err.Error()}
				}
				continue
			}

			if _, err := conn.ExecContext(ctx, stmt); err != nil {
				return &ApplyError{
					Msg: fmt.Sprintf("failed to execute SQL %q: %v", stmt, err),
				}
			}
		}
	}
	return nil
}

// MigrationVersion returns the current migration version stored in
// _sqlift_state, or 0 if the table does not exist or the key is absent.
//
// Port of C++ migration_version() (dist/sqlift.cpp lines 1535-1550).
func MigrationVersion(ctx context.Context, db *sql.DB) (int64, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return 0, &ApplyError{Msg: "failed to acquire connection: " + err.Error()}
	}
	defer conn.Close()

	exists, err := stateTableExists(ctx, conn)
	if err != nil || !exists {
		return 0, err
	}

	var raw string
	row := conn.QueryRowContext(ctx,
		"SELECT value FROM _sqlift_state WHERE key='migration_version'")
	if err := row.Scan(&raw); err != nil {
		// sql.ErrNoRows means key is absent — return 0.
		return 0, nil
	}

	var v int64
	if _, err := fmt.Sscan(raw, &v); err != nil {
		return 0, nil
	}
	return v, nil
}

// ensureStateTable creates the _sqlift_state table if it does not exist.
func ensureStateTable(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx,
		"CREATE TABLE IF NOT EXISTS _sqlift_state ("+
			"key   TEXT PRIMARY KEY,"+
			"value TEXT NOT NULL"+
			")")
	return err
}

// storeSchemaHash persists hash in _sqlift_state and increments the migration
// version counter.
func storeSchemaHash(ctx context.Context, conn *sql.Conn, hash string) error {
	if err := ensureStateTable(ctx, conn); err != nil {
		return &ApplyError{Msg: "failed to create _sqlift_state table: " + err.Error()}
	}

	if _, err := conn.ExecContext(ctx,
		"INSERT OR REPLACE INTO _sqlift_state (key, value) VALUES ('schema_hash', ?)",
		hash); err != nil {
		return &ApplyError{Msg: "failed to store schema hash: " + err.Error()}
	}

	if _, err := conn.ExecContext(ctx,
		"INSERT OR REPLACE INTO _sqlift_state (key, value) "+
			"VALUES ('migration_version', COALESCE("+
			"(SELECT CAST(value AS INTEGER) + 1 FROM _sqlift_state WHERE key='migration_version'), 1))"); err != nil {
		return &ApplyError{Msg: "failed to increment migration version: " + err.Error()}
	}

	return nil
}

// loadSchemaHash returns the schema_hash value stored in _sqlift_state, or an
// empty string if the table or key does not exist.
func loadSchemaHash(ctx context.Context, conn *sql.Conn) (string, error) {
	exists, err := stateTableExists(ctx, conn)
	if err != nil {
		return "", &ApplyError{Msg: "failed to check _sqlift_state existence: " + err.Error()}
	}
	if !exists {
		return "", nil
	}

	var hash string
	row := conn.QueryRowContext(ctx,
		"SELECT value FROM _sqlift_state WHERE key='schema_hash'")
	if err := row.Scan(&hash); err != nil {
		// sql.ErrNoRows means key is absent — that is fine.
		return "", nil
	}
	return hash, nil
}

// stateTableExists returns true if the _sqlift_state table is present in
// sqlite_master.
func stateTableExists(ctx context.Context, conn *sql.Conn) (bool, error) {
	var name string
	row := conn.QueryRowContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='table' AND name='_sqlift_state'")
	if err := row.Scan(&name); err != nil {
		return false, nil
	}
	return true, nil
}
