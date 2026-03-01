// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sort"

	_ "github.com/mattn/go-sqlite3"
)

func openDB() *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	return db
}

func ExampleParse() {
	schema, err := Parse(`
		CREATE TABLE users (
			id   INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT
		);
		CREATE TABLE posts (
			id      INTEGER PRIMARY KEY,
			user_id INTEGER REFERENCES users(id),
			title   TEXT NOT NULL
		);
	`)
	if err != nil {
		log.Fatal(err)
	}

	var names []string
	for name := range schema.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t := schema.Tables[name]
		fmt.Printf("%s: %d columns\n", name, len(t.Columns))
	}
	// Output:
	// posts: 3 columns
	// users: 3 columns
}

func ExampleExtract() {
	db := openDB()
	defer db.Close()

	_, err := db.Exec(`
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
		CREATE INDEX idx_users_name ON users (name);
	`)
	if err != nil {
		log.Fatal(err)
	}

	schema, err := Extract(context.Background(), db)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("tables: %d\n", len(schema.Tables))
	fmt.Printf("indexes: %d\n", len(schema.Indexes))
	// Output:
	// tables: 1
	// indexes: 1
}

func ExampleDiff() {
	current, err := Parse("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
	if err != nil {
		log.Fatal(err)
	}

	desired, err := Parse(`
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);
		CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT NOT NULL);
	`)
	if err != nil {
		log.Fatal(err)
	}

	plan, err := Diff(current, desired)
	if err != nil {
		log.Fatal(err)
	}

	for _, op := range plan.Operations() {
		fmt.Println(op.Description)
	}
	// Output:
	// Create table posts
	// Add column email to users
}

func ExampleApply() {
	db := openDB()
	defer db.Close()

	// Set up initial schema.
	_, err := db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
	if err != nil {
		log.Fatal(err)
	}

	current, err := Extract(context.Background(), db)
	if err != nil {
		log.Fatal(err)
	}

	desired, err := Parse(`
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);
	`)
	if err != nil {
		log.Fatal(err)
	}

	plan, err := Diff(current, desired)
	if err != nil {
		log.Fatal(err)
	}

	err = Apply(context.Background(), db, plan, ApplyOptions{})
	if err != nil {
		log.Fatal(err)
	}

	// Verify the column was added.
	after, err := Extract(context.Background(), db)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("columns: %d\n", len(after.Tables["users"].Columns))
	// Output:
	// columns: 3
}

func ExampleApply_destructive() {
	db := openDB()
	defer db.Close()

	_, err := db.Exec("CREATE TABLE old_table (id INTEGER PRIMARY KEY);")
	if err != nil {
		log.Fatal(err)
	}

	current, err := Extract(context.Background(), db)
	if err != nil {
		log.Fatal(err)
	}

	// Desired schema has no tables — dropping old_table is destructive.
	plan, err := Diff(current, Schema{})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("destructive: %v\n", plan.HasDestructiveOperations())

	// Without AllowDestructive, Apply rejects the plan.
	err = Apply(context.Background(), db, plan, ApplyOptions{})
	var de *DestructiveError
	fmt.Printf("blocked: %v\n", errors.As(err, &de))

	// With AllowDestructive, it proceeds.
	err = Apply(context.Background(), db, plan, ApplyOptions{AllowDestructive: true})
	if err != nil {
		log.Fatal(err)
	}

	after, err := Extract(context.Background(), db)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("tables: %d\n", len(after.Tables))
	// Output:
	// destructive: true
	// blocked: true
	// tables: 0
}

func ExampleMigrationVersion() {
	db := openDB()
	defer db.Close()

	_, err := db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY);")
	if err != nil {
		log.Fatal(err)
	}

	current, err := Extract(context.Background(), db)
	if err != nil {
		log.Fatal(err)
	}

	desired, err := Parse(`
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
	`)
	if err != nil {
		log.Fatal(err)
	}

	plan, err := Diff(current, desired)
	if err != nil {
		log.Fatal(err)
	}

	err = Apply(context.Background(), db, plan, ApplyOptions{})
	if err != nil {
		log.Fatal(err)
	}

	ver, err := MigrationVersion(context.Background(), db)
	if err != nil {
		log.Fatal(err)
	}
	// The version is a unix-micro timestamp; just check it's positive.
	fmt.Printf("version > 0: %v\n", ver > 0)
	// Output:
	// version > 0: true
}

func ExampleDetectRedundantIndexes() {
	schema, err := Parse(`
		CREATE TABLE users (
			id   INTEGER PRIMARY KEY,
			name TEXT,
			email TEXT
		);
		CREATE INDEX idx_name ON users (name);
		CREATE INDEX idx_name_email ON users (name, email);
	`)
	if err != nil {
		log.Fatal(err)
	}

	warnings := DetectRedundantIndexes(schema)
	for _, w := range warnings {
		fmt.Println(w.Message)
	}
	// Output:
	// Index 'idx_name' on table 'users' is redundant: columns are a prefix of index 'idx_name_email'
}

func ExampleDiff_breakingChange() {
	current, err := Parse("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
	if err != nil {
		log.Fatal(err)
	}

	// Changing nullable name to NOT NULL is a breaking change.
	desired, err := Parse("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")
	if err != nil {
		log.Fatal(err)
	}

	_, err = Diff(current, desired)
	var bce *BreakingChangeError
	fmt.Printf("breaking: %v\n", errors.As(err, &bce))
	// Output:
	// breaking: true
}

func ExampleToJSON() {
	current, err := Parse("CREATE TABLE users (id INTEGER PRIMARY KEY);")
	if err != nil {
		log.Fatal(err)
	}

	desired, err := Parse(`
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
	`)
	if err != nil {
		log.Fatal(err)
	}

	plan, err := Diff(current, desired)
	if err != nil {
		log.Fatal(err)
	}

	data, err := ToJSON(plan)
	if err != nil {
		log.Fatal(err)
	}

	roundTripped, err := FromJSON(data)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("operations: %d\n", len(roundTripped.Operations()))
	// Output:
	// operations: 1
}
