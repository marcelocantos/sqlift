// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

//#include "sqlift.h"
//#include <stdlib.h>
import "C"
import (
	"encoding/json"
	"unsafe"
)

// OpType classifies the kind of schema operation.
type OpType int

const (
	CreateTable  OpType = iota // Create a new table.
	DropTable                  // Drop an existing table.
	RebuildTable               // Rebuild (recreate) a table.
	AddColumn                  // Add a column via ALTER TABLE.
	CreateIndex                // Create a new index.
	DropIndex                  // Drop an existing index.
	CreateView                 // Create a new view.
	DropView                   // Drop an existing view.
	CreateTrigger              // Create a new trigger.
	DropTrigger                // Drop an existing trigger.
)

// Operation describes a single migration step.
type Operation struct {
	Type        OpType
	ObjectName  string
	Description string
	SQL         []string
	Destructive bool
}

// MigrationPlan holds an ordered list of operations produced by [Diff].
type MigrationPlan struct {
	operations []Operation
	warnings   []Warning
}

// Operations returns the ordered list of migration operations.
func (p MigrationPlan) Operations() []Operation { return p.operations }

// Warnings returns any schema warnings detected during diff.
func (p MigrationPlan) Warnings() []Warning { return p.warnings }

// HasDestructiveOperations reports whether any operation in the plan is
// destructive (drops data).
func (p MigrationPlan) HasDestructiveOperations() bool {
	for i := range p.operations {
		if p.operations[i].Destructive {
			return true
		}
	}
	return false
}

// Empty reports whether the plan contains no operations.
func (p MigrationPlan) Empty() bool { return len(p.operations) == 0 }

// ApplyOptions controls the behavior of [Apply].
type ApplyOptions struct {
	AllowDestructive bool
}

// Diff compares two schemas and produces a [MigrationPlan] that migrates
// current to desired. It is a pure function and never touches a database.
//
// It returns a [*BreakingChangeError] if the desired schema contains changes
// whose success depends on existing data (e.g. nullable to NOT NULL).
func Diff(current, desired Schema) (MigrationPlan, error) {
	currentJSON, err := json.Marshal(current)
	if err != nil {
		return MigrationPlan{}, &DiffError{Msg: "failed to encode current schema: " + err.Error()}
	}
	desiredJSON, err := json.Marshal(desired)
	if err != nil {
		return MigrationPlan{}, &DiffError{Msg: "failed to encode desired schema: " + err.Error()}
	}

	ccurrent := C.CString(string(currentJSON))
	defer C.free(unsafe.Pointer(ccurrent))
	cdesired := C.CString(string(desiredJSON))
	defer C.free(unsafe.Pointer(cdesired))

	var errType C.int
	var errMsg *C.char
	result := C.sqlift_diff(ccurrent, cdesired, &errType, &errMsg)
	if result == nil {
		return MigrationPlan{}, goError(errType, errMsg)
	}
	defer C.sqlift_free(unsafe.Pointer(result))

	return FromJSON([]byte(C.GoString(result)))
}
