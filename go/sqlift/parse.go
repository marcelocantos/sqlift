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

// Parse opens a temporary in-memory SQLite database, executes the provided
// DDL, and returns the resulting Schema.
func Parse(ddl string) (Schema, error) {
	cddl := C.CString(ddl)
	defer C.free(unsafe.Pointer(cddl))

	var errType C.int
	var errMsg *C.char
	result := C.sqlift_parse(cddl, &errType, &errMsg)
	if result == nil {
		return Schema{}, goError(errType, errMsg)
	}
	defer C.sqlift_free(unsafe.Pointer(result))

	var schema Schema
	if err := json.Unmarshal([]byte(C.GoString(result)), &schema); err != nil {
		return Schema{}, &ParseError{Msg: "failed to decode schema JSON: " + err.Error()}
	}
	return schema, nil
}
