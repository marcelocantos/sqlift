// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"testing"
)

func TestSchemaHash(t *testing.T) {
	t.Run("empty schema", func(t *testing.T) {
		s := Schema{
			Tables:   map[string]Table{},
			Indexes:  map[string]Index{},
			Views:    map[string]View{},
			Triggers: map[string]Trigger{},
		}
		h := s.Hash()
		if h == "" {
			t.Fatal("expected non-empty hash for empty schema")
		}
		// Same schema must produce same hash.
		if h2 := s.Hash(); h != h2 {
			t.Errorf("hash not deterministic: %q != %q", h, h2)
		}
	})

	t.Run("different schemas produce different hashes", func(t *testing.T) {
		s1 := mustParse(t, "CREATE TABLE a (id INTEGER PRIMARY KEY);")
		s2 := mustParse(t, "CREATE TABLE b (id INTEGER PRIMARY KEY);")
		if s1.Hash() == s2.Hash() {
			t.Error("expected different hashes for different table names")
		}
	})

	t.Run("cosmetic fields excluded from hash", func(t *testing.T) {
		// RawSQL differs but structural fields are the same.
		s1 := Schema{
			Tables: map[string]Table{
				"t": {
					Name:    "t",
					Columns: []Column{{Name: "id", Type: "INTEGER", PK: 1}},
					RawSQL:  "CREATE TABLE t (id INTEGER PRIMARY KEY)",
				},
			},
			Indexes:  map[string]Index{},
			Views:    map[string]View{},
			Triggers: map[string]Trigger{},
		}
		s2 := Schema{
			Tables: map[string]Table{
				"t": {
					Name:    "t",
					Columns: []Column{{Name: "id", Type: "INTEGER", PK: 1}},
					RawSQL:  "CREATE TABLE t(id INTEGER PRIMARY KEY)",
				},
			},
			Indexes:  map[string]Index{},
			Views:    map[string]View{},
			Triggers: map[string]Trigger{},
		}
		if s1.Hash() != s2.Hash() {
			t.Error("RawSQL should not affect hash")
		}
	})

	t.Run("constraint name excluded from hash", func(t *testing.T) {
		base := func(fkName, pkName string) Schema {
			return Schema{
				Tables: map[string]Table{
					"t": {
						Name:    "t",
						Columns: []Column{{Name: "id", Type: "INTEGER", PK: 1}},
						ForeignKeys: []ForeignKey{{
							ConstraintName: fkName,
							FromColumns:    []string{"id"},
							ToTable:        "t",
							ToColumns:      []string{"id"},
							OnUpdate:       "NO ACTION",
							OnDelete:       "NO ACTION",
						}},
						PKConstraintName: pkName,
					},
				},
				Indexes:  map[string]Index{},
				Views:    map[string]View{},
				Triggers: map[string]Trigger{},
			}
		}
		s1 := base("fk_one", "pk_one")
		s2 := base("fk_two", "pk_two")
		if s1.Hash() != s2.Hash() {
			t.Error("constraint names should not affect hash")
		}
	})

	t.Run("column order matters", func(t *testing.T) {
		s1 := Schema{
			Tables: map[string]Table{
				"t": {
					Name: "t",
					Columns: []Column{
						{Name: "a", Type: "TEXT"},
						{Name: "b", Type: "TEXT"},
					},
				},
			},
			Indexes:  map[string]Index{},
			Views:    map[string]View{},
			Triggers: map[string]Trigger{},
		}
		s2 := Schema{
			Tables: map[string]Table{
				"t": {
					Name: "t",
					Columns: []Column{
						{Name: "b", Type: "TEXT"},
						{Name: "a", Type: "TEXT"},
					},
				},
			},
			Indexes:  map[string]Index{},
			Views:    map[string]View{},
			Triggers: map[string]Trigger{},
		}
		if s1.Hash() == s2.Hash() {
			t.Error("column order should affect hash")
		}
	})
}

func TestSchemaEqual(t *testing.T) {
	t.Run("identical schemas", func(t *testing.T) {
		s1 := mustParse(t, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT);")
		s2 := mustParse(t, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT);")
		if !s1.Equal(s2) {
			t.Error("identical schemas should be equal")
		}
	})

	t.Run("different column types", func(t *testing.T) {
		s1 := mustParse(t, "CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT);")
		s2 := mustParse(t, "CREATE TABLE t (id INTEGER PRIMARY KEY, v BLOB);")
		if s1.Equal(s2) {
			t.Error("schemas with different column types should not be equal")
		}
	})

	t.Run("RawSQL excluded from equality", func(t *testing.T) {
		s1 := Schema{
			Tables: map[string]Table{
				"t": {
					Name:    "t",
					Columns: []Column{{Name: "id", Type: "INTEGER", PK: 1}},
					RawSQL:  "CREATE TABLE t (id INTEGER PRIMARY KEY)",
				},
			},
			Indexes:  map[string]Index{},
			Views:    map[string]View{},
			Triggers: map[string]Trigger{},
		}
		s2 := Schema{
			Tables: map[string]Table{
				"t": {
					Name:    "t",
					Columns: []Column{{Name: "id", Type: "INTEGER", PK: 1}},
					RawSQL:  "CREATE TABLE t(id INTEGER PRIMARY KEY)",
				},
			},
			Indexes:  map[string]Index{},
			Views:    map[string]View{},
			Triggers: map[string]Trigger{},
		}
		if !s1.Equal(s2) {
			t.Error("RawSQL should be excluded from equality")
		}
	})

	t.Run("constraint names excluded from equality", func(t *testing.T) {
		base := func(fkName, pkName string) Schema {
			return Schema{
				Tables: map[string]Table{
					"t": {
						Name:    "t",
						Columns: []Column{{Name: "id", Type: "INTEGER", PK: 1}},
						ForeignKeys: []ForeignKey{{
							ConstraintName: fkName,
							FromColumns:    []string{"id"},
							ToTable:        "t",
							ToColumns:      []string{"id"},
							OnUpdate:       "NO ACTION",
							OnDelete:       "NO ACTION",
						}},
						PKConstraintName: pkName,
					},
				},
				Indexes:  map[string]Index{},
				Views:    map[string]View{},
				Triggers: map[string]Trigger{},
			}
		}
		if !base("fk1", "pk1").Equal(base("fk2", "pk2")) {
			t.Error("constraint names should be excluded from equality")
		}
	})

	t.Run("extra table", func(t *testing.T) {
		s1 := mustParse(t, "CREATE TABLE a (id INTEGER PRIMARY KEY);")
		s2 := mustParse(t, "CREATE TABLE a (id INTEGER PRIMARY KEY); CREATE TABLE b (id INTEGER PRIMARY KEY);")
		if s1.Equal(s2) {
			t.Error("schemas with different table counts should not be equal")
		}
	})
}

// TestCrossLanguageHash verifies that the Go hash matches the C++ hash
// for a known DDL. The expected hash was computed independently by both
// the C++ and Go implementations and verified to match.
func TestCrossLanguageHash(t *testing.T) {
	const ddl = `
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT COLLATE NOCASE,
    age INTEGER CHECK(age > 0),
    FOREIGN KEY (id) REFERENCES users(id) ON DELETE CASCADE ON UPDATE NO ACTION
);
CREATE TABLE posts (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    title TEXT NOT NULL DEFAULT '',
    body TEXT
);
CREATE INDEX idx_posts_user ON posts(user_id);
CREATE UNIQUE INDEX idx_users_email ON users(email);
CREATE VIEW active_users AS SELECT id, name FROM users WHERE age > 18;
CREATE TRIGGER trg_posts_delete AFTER DELETE ON posts BEGIN SELECT 1; END;
`
	const expectedHash = "e712ade60030bfb83109e2bc49ba2d6d3025ade275dffde2a33ea5279dc99c13"

	s := mustParse(t, ddl)
	got := s.Hash()
	if got != expectedHash {
		t.Errorf("cross-language hash mismatch:\n  got:  %s\n  want: %s", got, expectedHash)
	}
}
