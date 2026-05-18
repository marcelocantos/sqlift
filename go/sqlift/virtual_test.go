// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"strings"
	"testing"
)

func TestParseVirtualTableFTS5(t *testing.T) {
	s := mustParse(t, "CREATE VIRTUAL TABLE messages_fts USING fts5(text, content);")
	if len(s.Tables) != 0 {
		t.Errorf("expected no regular tables, got %d: %v", len(s.Tables), keys(s.Tables))
	}
	if len(s.VirtualTables) != 1 {
		t.Fatalf("expected 1 virtual table, got %d", len(s.VirtualTables))
	}
	vt, ok := s.VirtualTables["messages_fts"]
	if !ok {
		t.Fatal("messages_fts not in virtual_tables")
	}
	if vt.Module != "fts5" {
		t.Errorf("Module = %q, want %q", vt.Module, "fts5")
	}
	if vt.Args != "text, content" {
		t.Errorf("Args = %q, want %q", vt.Args, "text, content")
	}
}

func TestExtractFiltersFTS5Shadows(t *testing.T) {
	db := openMemory(t)
	mustExec(t, db, "CREATE VIRTUAL TABLE notes USING fts5(body);")
	s := mustExtract(t, db)
	if len(s.Tables) != 0 {
		t.Errorf("shadow tables leaked into Tables: %v", keys(s.Tables))
	}
	if len(s.VirtualTables) != 1 {
		t.Fatalf("expected 1 virtual table, got %d", len(s.VirtualTables))
	}
	if s.VirtualTables["notes"].Module != "fts5" {
		t.Errorf("Module = %q, want fts5", s.VirtualTables["notes"].Module)
	}
}

func TestExtractDistinguishesShadowFromSimilarlyNamedTable(t *testing.T) {
	// foo_data_real is a real user table — its name has the foo_ prefix
	// but the suffix isn't one of FTS5's shadow suffixes, so it survives.
	db := openMemory(t)
	mustExec(t, db, "CREATE VIRTUAL TABLE foo USING fts5(x);")
	mustExec(t, db, "CREATE TABLE foo_data_real (id INTEGER PRIMARY KEY);")
	s := mustExtract(t, db)
	if _, ok := s.Tables["foo_data_real"]; !ok {
		t.Errorf("user table foo_data_real was filtered: %v", keys(s.Tables))
	}
	if _, ok := s.Tables["foo_data"]; ok {
		t.Errorf("shadow foo_data leaked through")
	}
}

func TestDiffCreateVirtualTable(t *testing.T) {
	desired := mustParse(t, "CREATE VIRTUAL TABLE search USING fts5(content);")
	plan := mustDiff(t, Schema{}, desired)
	ops := plan.Operations()
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d: %+v", len(ops), ops)
	}
	if ops[0].Type != CreateVirtualTable {
		t.Errorf("Type = %v, want CreateVirtualTable", ops[0].Type)
	}
	if ops[0].Destructive {
		t.Error("Destructive should be false for create")
	}
	if !strings.HasPrefix(ops[0].SQL[0], "CREATE VIRTUAL TABLE") {
		t.Errorf("SQL[0] = %q, want CREATE VIRTUAL TABLE prefix", ops[0].SQL[0])
	}
}

func TestDiffDropVirtualTableIsDestructive(t *testing.T) {
	current := mustParse(t, "CREATE VIRTUAL TABLE search USING fts5(content);")
	plan := mustDiff(t, current, Schema{})
	ops := plan.Operations()
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	if ops[0].Type != DropVirtualTable {
		t.Errorf("Type = %v, want DropVirtualTable", ops[0].Type)
	}
	if !ops[0].Destructive {
		t.Error("Destructive should be true for drop")
	}
}

func TestDiffIdenticalVirtualTableIsNoOp(t *testing.T) {
	s := mustParse(t, "CREATE VIRTUAL TABLE search USING fts5(content);")
	plan := mustDiff(t, s, s)
	if !plan.Empty() {
		t.Errorf("expected no-op, got %d ops", len(plan.Operations()))
	}
}

func TestDiffChangedArgsIsDropPlusRecreate(t *testing.T) {
	current := mustParse(t, "CREATE VIRTUAL TABLE search USING fts5(content);")
	desired := mustParse(t, "CREATE VIRTUAL TABLE search USING fts5(content, body);")
	plan := mustDiff(t, current, desired)
	ops := plan.Operations()
	if len(ops) != 2 {
		t.Fatalf("expected 2 ops, got %d", len(ops))
	}
	if ops[0].Type != DropVirtualTable || !ops[0].Destructive {
		t.Errorf("op[0] = %v destructive=%v, want DropVirtualTable destructive=true",
			ops[0].Type, ops[0].Destructive)
	}
	if ops[1].Type != CreateVirtualTable {
		t.Errorf("op[1] = %v, want CreateVirtualTable", ops[1].Type)
	}
}

func TestApplyCreateVirtualTableUnderAllowNone(t *testing.T) {
	db := openMemory(t)
	desired := mustParse(t, "CREATE VIRTUAL TABLE search USING fts5(text);")
	plan := mustDiff(t, Schema{}, desired)
	// AllowNone — pure additive virtual table create is permitted.
	if err := Apply(db, plan, ApplyOptions{Allow: AllowNone}); err != nil {
		t.Fatalf("Apply with AllowNone failed: %v", err)
	}
	// Round-trip: extract again, diff against desired = empty plan.
	after := mustExtract(t, db)
	noop := mustDiff(t, after, desired)
	if !noop.Empty() {
		t.Errorf("expected no-op after round-trip, got %d ops", len(noop.Operations()))
	}
}

func TestApplyDropVirtualTableRejectedByAllowNone(t *testing.T) {
	db := openMemory(t)
	mustExec(t, db, "CREATE VIRTUAL TABLE old USING fts5(x);")
	current := mustExtract(t, db)
	plan := mustDiff(t, current, Schema{})

	err := Apply(db, plan, ApplyOptions{Allow: AllowNone})
	if err == nil {
		t.Fatal("expected DestructiveError, got nil")
	}
	if _, ok := err.(*DestructiveError); !ok {
		t.Errorf("error type = %T, want *DestructiveError", err)
	}
}

func TestHashStableForSchemaWithoutVirtualTables(t *testing.T) {
	// A schema with no virtual tables must produce a hash byte-identical
	// to what it would have produced before the feature existed.
	s := Schema{
		Tables: map[string]Table{
			"users": {
				Name: "users",
				Columns: []Column{
					{Name: "id", Type: "INTEGER", PK: 1},
				},
			},
		},
	}
	// The hash with an explicitly-empty VirtualTables must equal the hash
	// with a nil VirtualTables (the omitempty path).
	s2 := s
	s2.VirtualTables = map[string]VirtualTable{}
	if s.Hash() != s2.Hash() {
		t.Errorf("Hash differs between nil and empty VirtualTables:\n  nil=%s\n  empty=%s",
			s.Hash(), s2.Hash())
	}
}

func TestHashIncludesVirtualTablesWhenPresent(t *testing.T) {
	s1 := mustParse(t, "CREATE VIRTUAL TABLE search USING fts5(a);")
	s2 := mustParse(t, "CREATE VIRTUAL TABLE search USING fts5(b);")
	if s1.Hash() == s2.Hash() {
		t.Errorf("Hash should differ between fts5(a) and fts5(b)")
	}
}

func TestHashFormatSnapshot(t *testing.T) {
	// Snapshot the exact hash for a known virtual-table schema. This locks
	// in the wire format so any future drift between Go's Schema.Hash() and
	// dist/sqlift.cpp Schema::hash() is caught here (and by the equivalent
	// C++ snapshot — adjust both when intentionally changing the format).
	s := Schema{
		VirtualTables: map[string]VirtualTable{
			"notes": {Name: "notes", Module: "fts5", Args: "body"},
		},
	}
	// Hash input is "VTABLE notes USING fts5(body)\n" → sha256 hex.
	const expected = "0653de74367965a76a24b08085cfd3840c714c10f8a8463fbdcbf62f33e65a35"
	got := s.Hash()
	if got != expected {
		t.Errorf("Hash format snapshot drifted:\n  got  %s\n  want %s\n  "+
			"(if intentional, update both this constant AND the C++ snapshot)",
			got, expected)
	}
}

func keys(m map[string]Table) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
