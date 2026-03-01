// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"context"
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

// Parse opens a temporary in-memory SQLite database, executes the provided
// DDL, and returns the resulting Schema.
//
// Port of C++ parse() (dist/sqlift.cpp lines 756-766).
func Parse(ddl string) (Schema, error) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return Schema{}, &ParseError{Msg: "Failed to parse schema SQL: " + err.Error()}
	}
	defer db.Close()

	// Ensure the pool does not open a second in-memory database.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(ddl); err != nil {
		return Schema{}, &ParseError{Msg: "Failed to parse schema SQL: " + err.Error()}
	}

	return Extract(context.Background(), db)
}
