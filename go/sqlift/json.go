// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import (
	"encoding/json"
	"fmt"
	"strings"
)

// String returns the human-readable name of an OpType.
func (t OpType) String() string {
	switch t {
	case CreateTable:
		return "CreateTable"
	case DropTable:
		return "DropTable"
	case RebuildTable:
		return "RebuildTable"
	case AddColumn:
		return "AddColumn"
	case CreateIndex:
		return "CreateIndex"
	case DropIndex:
		return "DropIndex"
	case CreateView:
		return "CreateView"
	case DropView:
		return "DropView"
	case CreateTrigger:
		return "CreateTrigger"
	case DropTrigger:
		return "DropTrigger"
	default:
		return fmt.Sprintf("OpType(%d)", int(t))
	}
}

// ParseOpType converts a string produced by [OpType.String] back to an OpType.
// It returns a [JSONError] for unrecognised strings.
func ParseOpType(s string) (OpType, error) {
	switch s {
	case "CreateTable":
		return CreateTable, nil
	case "DropTable":
		return DropTable, nil
	case "RebuildTable":
		return RebuildTable, nil
	case "AddColumn":
		return AddColumn, nil
	case "CreateIndex":
		return CreateIndex, nil
	case "DropIndex":
		return DropIndex, nil
	case "CreateView":
		return CreateView, nil
	case "DropView":
		return DropView, nil
	case "CreateTrigger":
		return CreateTrigger, nil
	case "DropTrigger":
		return DropTrigger, nil
	default:
		return 0, &JSONError{Msg: "Unknown OpType string: " + s}
	}
}

// jsonOperation is the wire representation of an [Operation].
type jsonOperation struct {
	Type        string   `json:"type"`
	ObjectName  string   `json:"object_name"`
	Description string   `json:"description"`
	SQL         []string `json:"sql"`
	Destructive bool     `json:"destructive"`
}

// jsonWarning is the wire representation of a [Warning].
type jsonWarning struct {
	Type      string `json:"type"`
	Message   string `json:"message"`
	IndexName string `json:"index_name"`
	CoveredBy string `json:"covered_by"`
	TableName string `json:"table_name"`
}

// jsonPlan is the wire representation of a [MigrationPlan].
type jsonPlan struct {
	Version    int             `json:"version"`
	Operations []jsonOperation `json:"operations"`
	Warnings   []jsonWarning   `json:"warnings,omitempty"`
}

// ToJSON serialises plan to indented JSON (version 1 format).
//
// Port of C++ to_json() (dist/sqlift.cpp lines 1595-1613).
func ToJSON(plan MigrationPlan) ([]byte, error) {
	jp := jsonPlan{
		Version:    1,
		Operations: make([]jsonOperation, 0, len(plan.Operations())),
	}
	for _, op := range plan.Operations() {
		sql := op.SQL
		if sql == nil {
			sql = []string{}
		}
		jp.Operations = append(jp.Operations, jsonOperation{
			Type:        op.Type.String(),
			ObjectName:  op.ObjectName,
			Description: op.Description,
			SQL:         sql,
			Destructive: op.Destructive,
		})
	}
	for _, w := range plan.Warnings() {
		jp.Warnings = append(jp.Warnings, jsonWarning{
			Type:      "RedundantIndex",
			Message:   w.Message,
			IndexName: w.IndexName,
			CoveredBy: w.CoveredBy,
			TableName: w.TableName,
		})
	}
	return json.MarshalIndent(jp, "", "  ")
}

// FromJSON deserialises a [MigrationPlan] from JSON produced by [ToJSON].
//
// Port of C++ from_json() (dist/sqlift.cpp lines 1615-1693).
//
// Validation:
//   - Must be a JSON object.
//   - Must have "version": 1 (integer).
//   - Must have "operations" array.
//   - Each operation must have: type (string), object_name (string),
//     description (string), sql (array of strings), destructive (bool).
//   - The first SQL statement of each operation must start with the prefix
//     expected for its OpType.
func FromJSON(data []byte) (MigrationPlan, error) {
	// Unmarshal into a raw map first so we can give precise per-field errors.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return MigrationPlan{}, &JSONError{Msg: "Invalid JSON: " + err.Error()}
	}

	// Validate version.
	versionRaw, ok := raw["version"]
	if !ok {
		return MigrationPlan{}, &JSONError{Msg: "Missing or invalid 'version' field"}
	}
	var version int
	if err := json.Unmarshal(versionRaw, &version); err != nil {
		return MigrationPlan{}, &JSONError{Msg: "Missing or invalid 'version' field"}
	}
	if version != 1 {
		return MigrationPlan{}, &JSONError{Msg: fmt.Sprintf("Unsupported version: %d", version)}
	}

	// Validate operations array.
	opsRaw, ok := raw["operations"]
	if !ok {
		return MigrationPlan{}, &JSONError{Msg: "Missing or invalid 'operations' array"}
	}
	var rawOps []json.RawMessage
	if err := json.Unmarshal(opsRaw, &rawOps); err != nil {
		return MigrationPlan{}, &JSONError{Msg: "Missing or invalid 'operations' array"}
	}

	ops := make([]Operation, 0, len(rawOps))
	for _, rawOp := range rawOps {
		var opMap map[string]json.RawMessage
		if err := json.Unmarshal(rawOp, &opMap); err != nil {
			return MigrationPlan{}, &JSONError{Msg: "Each operation must be a JSON object"}
		}

		// type
		typeRaw, ok := opMap["type"]
		if !ok {
			return MigrationPlan{}, &JSONError{Msg: "Operation missing 'type' string field"}
		}
		var typeStr string
		if err := json.Unmarshal(typeRaw, &typeStr); err != nil {
			return MigrationPlan{}, &JSONError{Msg: "Operation missing 'type' string field"}
		}
		opType, err := ParseOpType(typeStr)
		if err != nil {
			return MigrationPlan{}, err
		}

		// object_name
		onRaw, ok := opMap["object_name"]
		if !ok {
			return MigrationPlan{}, &JSONError{Msg: "Operation missing 'object_name' string field"}
		}
		var objectName string
		if err := json.Unmarshal(onRaw, &objectName); err != nil {
			return MigrationPlan{}, &JSONError{Msg: "Operation missing 'object_name' string field"}
		}

		// description
		descRaw, ok := opMap["description"]
		if !ok {
			return MigrationPlan{}, &JSONError{Msg: "Operation missing 'description' string field"}
		}
		var description string
		if err := json.Unmarshal(descRaw, &description); err != nil {
			return MigrationPlan{}, &JSONError{Msg: "Operation missing 'description' string field"}
		}

		// sql
		sqlRaw, ok := opMap["sql"]
		if !ok {
			return MigrationPlan{}, &JSONError{Msg: "Operation missing 'sql' array field"}
		}
		var rawSQLItems []json.RawMessage
		if err := json.Unmarshal(sqlRaw, &rawSQLItems); err != nil {
			return MigrationPlan{}, &JSONError{Msg: "Operation missing 'sql' array field"}
		}
		sqlStmts := make([]string, 0, len(rawSQLItems))
		for _, item := range rawSQLItems {
			var s string
			if err := json.Unmarshal(item, &s); err != nil {
				return MigrationPlan{}, &JSONError{Msg: "'sql' array must contain only strings"}
			}
			sqlStmts = append(sqlStmts, s)
		}

		// destructive
		destRaw, ok := opMap["destructive"]
		if !ok {
			return MigrationPlan{}, &JSONError{Msg: "Operation missing 'destructive' boolean field"}
		}
		var destructive bool
		if err := json.Unmarshal(destRaw, &destructive); err != nil {
			return MigrationPlan{}, &JSONError{Msg: "Operation missing 'destructive' boolean field"}
		}

		// Validate that the first SQL statement starts with the expected prefix.
		if len(sqlStmts) > 0 {
			prefix := sqlPrefixForOpType(opType)
			if !strings.HasPrefix(sqlStmts[0], prefix) {
				return MigrationPlan{}, &JSONError{
					Msg: fmt.Sprintf(
						"Operation %q on %q: first SQL statement does not start with %q",
						opType.String(), objectName, prefix),
				}
			}
		}

		ops = append(ops, Operation{
			Type:        opType,
			ObjectName:  objectName,
			Description: description,
			SQL:         sqlStmts,
			Destructive: destructive,
		})
	}

	// Parse warnings (optional for backward compatibility).
	var warns []Warning
	if warnsRaw, ok := raw["warnings"]; ok {
		var rawWarns []json.RawMessage
		if err := json.Unmarshal(warnsRaw, &rawWarns); err == nil {
			for _, rw := range rawWarns {
				var jw jsonWarning
				if err := json.Unmarshal(rw, &jw); err == nil {
					warns = append(warns, Warning{
						Type:      RedundantIndex,
						Message:   jw.Message,
						IndexName: jw.IndexName,
						CoveredBy: jw.CoveredBy,
						TableName: jw.TableName,
					})
				}
			}
		}
	}

	return MigrationPlan{operations: ops, warnings: warns}, nil
}

// sqlPrefixForOpType returns the expected SQL prefix for the first statement of
// an operation with the given OpType.
func sqlPrefixForOpType(t OpType) string {
	switch t {
	case CreateTable:
		return "CREATE TABLE"
	case DropTable:
		return "DROP TABLE"
	case RebuildTable:
		return "PRAGMA foreign_keys"
	case AddColumn:
		return "ALTER TABLE"
	case CreateIndex:
		// Could be "CREATE INDEX" or "CREATE UNIQUE INDEX".
		return "CREATE"
	case DropIndex:
		return "DROP INDEX"
	case CreateView:
		return "CREATE VIEW"
	case DropView:
		return "DROP VIEW"
	case CreateTrigger:
		return "CREATE TRIGGER"
	case DropTrigger:
		return "DROP TRIGGER"
	default:
		return ""
	}
}
