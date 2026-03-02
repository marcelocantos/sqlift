// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

//#include "sqlift_c.h"
//#include <stdlib.h>
import "C"
import "unsafe"

// Database wraps a SQLite database handle managed by the C++ library.
type Database struct {
	db *C.sqlift_db
}

// Open opens a SQLite database at path.
func Open(path string) (*Database, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	var errType C.int
	var errMsg *C.char
	db := C.sqlift_db_open(cpath, 0, &errType, &errMsg)
	if db == nil {
		return nil, goError(errType, errMsg)
	}
	return &Database{db: db}, nil
}

// Close closes the database.
func (d *Database) Close() {
	if d.db != nil {
		C.sqlift_db_close(d.db)
		d.db = nil
	}
}

// Exec executes SQL with no result.
func (d *Database) Exec(sql string) error {
	csql := C.CString(sql)
	defer C.free(unsafe.Pointer(csql))

	var errMsg *C.char
	rc := C.sqlift_db_exec(d.db, csql, &errMsg)
	if rc != 0 {
		msg := C.GoString(errMsg)
		C.sqlift_free(unsafe.Pointer(errMsg))
		return &ApplyError{Msg: msg}
	}
	return nil
}

// QueryInt64 executes a query that returns a single integer value.
func (d *Database) QueryInt64(sql string) (int64, error) {
	csql := C.CString(sql)
	defer C.free(unsafe.Pointer(csql))

	var result C.int64_t
	var errMsg *C.char
	rc := C.sqlift_db_query_int64(d.db, csql, &result, &errMsg)
	if rc != 0 {
		msg := C.GoString(errMsg)
		C.sqlift_free(unsafe.Pointer(errMsg))
		return 0, &ApplyError{Msg: msg}
	}
	return int64(result), nil
}

// QueryText executes a query that returns a single text value.
func (d *Database) QueryText(sql string) (string, error) {
	csql := C.CString(sql)
	defer C.free(unsafe.Pointer(csql))

	var errMsg *C.char
	result := C.sqlift_db_query_text(d.db, csql, &errMsg)
	if result == nil {
		msg := C.GoString(errMsg)
		C.sqlift_free(unsafe.Pointer(errMsg))
		return "", &ApplyError{Msg: msg}
	}
	defer C.sqlift_free(unsafe.Pointer(result))
	return C.GoString(result), nil
}
