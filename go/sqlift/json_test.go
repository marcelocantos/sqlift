// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"errors"
	"testing"
)

func TestJSON(t *testing.T) {
	t.Run("to_string covers all OpType values", func(t *testing.T) {
		cases := []struct {
			op   OpType
			want string
		}{
			{CreateTable, "CreateTable"},
			{DropTable, "DropTable"},
			{RebuildTable, "RebuildTable"},
			{AddColumn, "AddColumn"},
			{CreateIndex, "CreateIndex"},
			{DropIndex, "DropIndex"},
			{CreateView, "CreateView"},
			{DropView, "DropView"},
			{CreateTrigger, "CreateTrigger"},
			{DropTrigger, "DropTrigger"},
		}
		for _, c := range cases {
			got := c.op.String()
			if got != c.want {
				t.Errorf("OpType(%d).String(): got %q, want %q", int(c.op), got, c.want)
			}
		}
	})

	t.Run("op_type_from_string round-trips with to_string", func(t *testing.T) {
		allTypes := []OpType{
			CreateTable, DropTable, RebuildTable, AddColumn,
			CreateIndex, DropIndex, CreateView, DropView,
			CreateTrigger, DropTrigger,
		}
		for _, op := range allTypes {
			got, err := ParseOpType(op.String())
			if err != nil {
				t.Errorf("ParseOpType(%q) returned error: %v", op.String(), err)
				continue
			}
			if got != op {
				t.Errorf("ParseOpType(%q): got %v, want %v", op.String(), got, op)
			}
		}
	})

	t.Run("op_type_from_string rejects unknown strings", func(t *testing.T) {
		bad := []string{"NotAnOp", "", "createtable"}
		for _, s := range bad {
			_, err := ParseOpType(s)
			if err == nil {
				t.Errorf("ParseOpType(%q): expected error, got nil", s)
				continue
			}
			var je *JSONError
			if !errors.As(err, &je) {
				t.Errorf("ParseOpType(%q): expected *JSONError, got %T: %v", s, err, err)
			}
		}
	})

	t.Run("json round-trip: empty plan", func(t *testing.T) {
		s := mustParse(t, "CREATE TABLE t (id INTEGER PRIMARY KEY);")
		plan := mustDiff(t, s, s)
		if !plan.Empty() {
			t.Fatal("expected empty plan")
		}

		data, err := ToJSON(plan)
		if err != nil {
			t.Fatalf("ToJSON failed: %v", err)
		}
		restored, err := FromJSON(data)
		if err != nil {
			t.Fatalf("FromJSON failed: %v", err)
		}
		if !restored.Empty() {
			t.Errorf("expected restored plan to be empty")
		}
		if len(restored.Operations()) != 0 {
			t.Errorf("expected 0 operations, got %d", len(restored.Operations()))
		}
	})

	t.Run("json round-trip: plan with multiple operation types", func(t *testing.T) {
		current := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"+
				"CREATE TABLE old_logs (id INTEGER PRIMARY KEY);"+
				"CREATE INDEX idx_name ON users(name);"+
				"CREATE VIEW all_users AS SELECT * FROM users;")

		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);"+
				"CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT);"+
				"CREATE VIEW active_users AS SELECT * FROM users;")

		plan := mustDiff(t, current, desired)
		if plan.Empty() {
			t.Fatal("expected non-empty plan")
		}

		data, err := ToJSON(plan)
		if err != nil {
			t.Fatalf("ToJSON failed: %v", err)
		}
		restored, err := FromJSON(data)
		if err != nil {
			t.Fatalf("FromJSON failed: %v", err)
		}

		origOps := plan.Operations()
		restOps := restored.Operations()
		if len(restOps) != len(origOps) {
			t.Fatalf("operation count mismatch: got %d, want %d", len(restOps), len(origOps))
		}
		for i := range origOps {
			orig := origOps[i]
			rest := restOps[i]
			if rest.Type != orig.Type {
				t.Errorf("op[%d].Type: got %v, want %v", i, rest.Type, orig.Type)
			}
			if rest.ObjectName != orig.ObjectName {
				t.Errorf("op[%d].ObjectName: got %q, want %q", i, rest.ObjectName, orig.ObjectName)
			}
			if rest.Description != orig.Description {
				t.Errorf("op[%d].Description: got %q, want %q", i, rest.Description, orig.Description)
			}
			if len(rest.SQL) != len(orig.SQL) {
				t.Errorf("op[%d].SQL length: got %d, want %d", i, len(rest.SQL), len(orig.SQL))
			} else {
				for j := range orig.SQL {
					if rest.SQL[j] != orig.SQL[j] {
						t.Errorf("op[%d].SQL[%d]: got %q, want %q", i, j, rest.SQL[j], orig.SQL[j])
					}
				}
			}
			if rest.Destructive != orig.Destructive {
				t.Errorf("op[%d].Destructive: got %v, want %v", i, rest.Destructive, orig.Destructive)
			}
		}
	})

	t.Run("json round-trip: destructive operations", func(t *testing.T) {
		current := mustParse(t, "CREATE TABLE users (id INTEGER PRIMARY KEY);")
		desired := Schema{
			Tables:   map[string]Table{},
			Indexes:  map[string]Index{},
			Views:    map[string]View{},
			Triggers: map[string]Trigger{},
		}

		plan := mustDiff(t, current, desired)
		if !plan.HasDestructiveOperations() {
			t.Fatal("expected plan to have destructive operations")
		}

		data, err := ToJSON(plan)
		if err != nil {
			t.Fatalf("ToJSON failed: %v", err)
		}
		restored, err := FromJSON(data)
		if err != nil {
			t.Fatalf("FromJSON failed: %v", err)
		}
		if !restored.HasDestructiveOperations() {
			t.Error("expected restored plan to have destructive operations")
		}
		ops := restored.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if !ops[0].Destructive {
			t.Error("expected op[0].Destructive == true")
		}
		if ops[0].Type != DropTable {
			t.Errorf("expected op[0].Type == DropTable, got %v", ops[0].Type)
		}
	})

	t.Run("json round-trip: rebuild table preserves multi-statement sql", func(t *testing.T) {
		current := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age TEXT);")
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER);")

		plan := mustDiff(t, current, desired)
		ops := plan.Operations()
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Type != RebuildTable {
			t.Fatalf("expected RebuildTable, got %v", ops[0].Type)
		}
		if len(ops[0].SQL) <= 1 {
			t.Fatalf("expected sql.size > 1, got %d", len(ops[0].SQL))
		}

		data, err := ToJSON(plan)
		if err != nil {
			t.Fatalf("ToJSON failed: %v", err)
		}
		restored, err := FromJSON(data)
		if err != nil {
			t.Fatalf("FromJSON failed: %v", err)
		}
		restOps := restored.Operations()
		if len(restOps[0].SQL) != len(ops[0].SQL) {
			t.Errorf("SQL statement count mismatch: got %d, want %d", len(restOps[0].SQL), len(ops[0].SQL))
		} else {
			for i := range ops[0].SQL {
				if restOps[0].SQL[i] != ops[0].SQL[i] {
					t.Errorf("SQL[%d]: got %q, want %q", i, restOps[0].SQL[i], ops[0].SQL[i])
				}
			}
		}
	})

	t.Run("deserialized plan can be applied to a database", func(t *testing.T) {
		desired := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);"+
				"CREATE INDEX idx_name ON users(name);")

		empty := Schema{
			Tables:   map[string]Table{},
			Indexes:  map[string]Index{},
			Views:    map[string]View{},
			Triggers: map[string]Trigger{},
		}
		plan := mustDiff(t, empty, desired)

		data, err := ToJSON(plan)
		if err != nil {
			t.Fatalf("ToJSON failed: %v", err)
		}
		restored, err := FromJSON(data)
		if err != nil {
			t.Fatalf("FromJSON failed: %v", err)
		}

		db := openMemory(t)
		mustApply(t, db, restored)

		after := mustExtract(t, db)
		if _, ok := after.Tables["users"]; !ok {
			t.Error("expected table 'users' to exist after apply")
		} else if len(after.Tables["users"].Columns) != 2 {
			t.Errorf("expected 2 columns in 'users', got %d", len(after.Tables["users"].Columns))
		}
		if _, ok := after.Indexes["idx_name"]; !ok {
			t.Error("expected index 'idx_name' to exist after apply")
		}
	})

	t.Run("from_json rejects invalid JSON", func(t *testing.T) {
		bad := []string{"not json", ""}
		for _, s := range bad {
			_, err := FromJSON([]byte(s))
			if err == nil {
				t.Errorf("FromJSON(%q): expected error, got nil", s)
				continue
			}
			var je *JSONError
			if !errors.As(err, &je) {
				t.Errorf("FromJSON(%q): expected *JSONError, got %T: %v", s, err, err)
			}
		}
	})

	t.Run("from_json rejects non-object top level", func(t *testing.T) {
		bad := []string{"[1,2,3]", "42"}
		for _, s := range bad {
			_, err := FromJSON([]byte(s))
			if err == nil {
				t.Errorf("FromJSON(%q): expected error, got nil", s)
				continue
			}
			var je *JSONError
			if !errors.As(err, &je) {
				t.Errorf("FromJSON(%q): expected *JSONError, got %T: %v", s, err, err)
			}
		}
	})

	t.Run("from_json rejects missing version", func(t *testing.T) {
		_, err := FromJSON([]byte(`{"operations":[]}`))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var je *JSONError
		if !errors.As(err, &je) {
			t.Errorf("expected *JSONError, got %T: %v", err, err)
		}
	})

	t.Run("from_json rejects unsupported version", func(t *testing.T) {
		_, err := FromJSON([]byte(`{"version":999,"operations":[]}`))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var je *JSONError
		if !errors.As(err, &je) {
			t.Errorf("expected *JSONError, got %T: %v", err, err)
		}
	})

	t.Run("from_json rejects missing operations", func(t *testing.T) {
		_, err := FromJSON([]byte(`{"version":1}`))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var je *JSONError
		if !errors.As(err, &je) {
			t.Errorf("expected *JSONError, got %T: %v", err, err)
		}
	})

	t.Run("from_json rejects operation with missing fields", func(t *testing.T) {
		// Missing "type" field.
		missingType := `{"version":1,"operations":[` +
			`{"object_name":"t","description":"d","sql":["CREATE TABLE t (id INTEGER)"],"destructive":false}` +
			`]}`
		_, err := FromJSON([]byte(missingType))
		if err == nil {
			t.Error("missing type: expected error, got nil")
		} else {
			var je *JSONError
			if !errors.As(err, &je) {
				t.Errorf("missing type: expected *JSONError, got %T: %v", err, err)
			}
		}

		// Missing "sql" field.
		missingSQL := `{"version":1,"operations":[` +
			`{"type":"CreateTable","object_name":"t","description":"d","destructive":false}` +
			`]}`
		_, err = FromJSON([]byte(missingSQL))
		if err == nil {
			t.Error("missing sql: expected error, got nil")
		} else {
			var je *JSONError
			if !errors.As(err, &je) {
				t.Errorf("missing sql: expected *JSONError, got %T: %v", err, err)
			}
		}
	})

	t.Run("from_json rejects unknown OpType string", func(t *testing.T) {
		tampered := `{"version":1,"operations":[` +
			`{"type":"Bogus","object_name":"t","description":"d","sql":[],"destructive":false}` +
			`]}`
		_, err := FromJSON([]byte(tampered))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var je *JSONError
		if !errors.As(err, &je) {
			t.Errorf("expected *JSONError, got %T: %v", err, err)
		}
	})

	t.Run("json round-trip: warnings preserved", func(t *testing.T) {
		s := mustParse(t,
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"+
				"CREATE INDEX idx_id ON users(id);")
		plan := mustDiff(t, Schema{}, s)
		if len(plan.Warnings()) != 1 {
			t.Fatalf("expected 1 warning, got %d", len(plan.Warnings()))
		}

		data, err := ToJSON(plan)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := FromJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		if len(restored.Warnings()) != 1 {
			t.Fatalf("expected 1 warning after round-trip, got %d", len(restored.Warnings()))
		}
		if restored.Warnings()[0].IndexName != "idx_id" {
			t.Errorf("expected IndexName 'idx_id', got %q", restored.Warnings()[0].IndexName)
		}
		if restored.Warnings()[0].CoveredBy != "PRIMARY KEY" {
			t.Errorf("expected CoveredBy 'PRIMARY KEY', got %q", restored.Warnings()[0].CoveredBy)
		}
	})

	t.Run("from_json: missing warnings field is ok", func(t *testing.T) {
		data := []byte(`{"version":1,"operations":[]}`)
		plan, err := FromJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Warnings()) != 0 {
			t.Errorf("expected 0 warnings, got %d", len(plan.Warnings()))
		}
	})

	t.Run("from_json rejects tampered plan with mismatched type and sql", func(t *testing.T) {
		cases := []string{
			// CreateTable with DROP TABLE sql.
			`{"version":1,"operations":[{"type":"CreateTable","object_name":"t","description":"d","sql":["DROP TABLE t"],"destructive":false}]}`,
			// DropTable with CREATE TABLE sql.
			`{"version":1,"operations":[{"type":"DropTable","object_name":"t","description":"d","sql":["CREATE TABLE t (id INTEGER)"],"destructive":true}]}`,
			// RebuildTable with DROP TABLE sql (should start with PRAGMA foreign_keys).
			`{"version":1,"operations":[{"type":"RebuildTable","object_name":"t","description":"d","sql":["DROP TABLE t"],"destructive":false}]}`,
			// AddColumn with DROP TABLE sql (should start with ALTER TABLE).
			`{"version":1,"operations":[{"type":"AddColumn","object_name":"t","description":"d","sql":["DROP TABLE t"],"destructive":false}]}`,
		}
		for _, s := range cases {
			_, err := FromJSON([]byte(s))
			if err == nil {
				t.Errorf("FromJSON(%q): expected error, got nil", s)
				continue
			}
			var je *JSONError
			if !errors.As(err, &je) {
				t.Errorf("FromJSON(%q): expected *JSONError, got %T: %v", s, err, err)
			}
		}
	})
}
