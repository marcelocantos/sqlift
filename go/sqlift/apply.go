// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

//#include "sqlift_c.h"
//#include <stdlib.h>
import "C"
import "unsafe"

// Apply executes plan against db.
//
// Steps:
//  1. If the plan is empty, return nil immediately.
//  2. If the plan contains destructive operations and opts.AllowDestructive is
//     false, return a [DestructiveError].
//  3. Extract the current schema and compare the stored hash with the actual
//     hash. If they differ, return a [DriftError].
//  4. Execute each operation's SQL statements.
//  5. On success: extract the updated schema and store its hash.
func Apply(db *Database, plan MigrationPlan, opts ApplyOptions) error {
	planJSON, err := ToJSON(plan)
	if err != nil {
		return &ApplyError{Msg: "failed to encode plan: " + err.Error()}
	}

	cplan := C.CString(string(planJSON))
	defer C.free(unsafe.Pointer(cplan))

	allowDestructive := C.int(0)
	if opts.AllowDestructive {
		allowDestructive = 1
	}

	var errType C.int
	var errMsg *C.char
	C.sqlift_apply(db.db, cplan, allowDestructive, &errType, &errMsg)
	if errType != C.SQLIFT_OK {
		return goError(errType, errMsg)
	}
	return nil
}

// MigrationVersion returns the current migration version stored in
// _sqlift_state, or 0 if the table does not exist or the key is absent.
func MigrationVersion(db *Database) (int64, error) {
	var errType C.int
	var errMsg *C.char
	v := C.sqlift_migration_version(db.db, &errType, &errMsg)
	if errType != C.SQLIFT_OK {
		return 0, goError(errType, errMsg)
	}
	return int64(v), nil
}
