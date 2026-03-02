// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

//#include "sqlift_c.h"
import "C"
import (
	"encoding/json"
	"unsafe"
)

// Extract reads the schema from db and returns a Schema value.
func Extract(db *Database) (Schema, error) {
	var errType C.int
	var errMsg *C.char
	result := C.sqlift_extract(db.db, &errType, &errMsg)
	if result == nil {
		return Schema{}, goError(errType, errMsg)
	}
	defer C.sqlift_free(unsafe.Pointer(result))

	var schema Schema
	if err := json.Unmarshal([]byte(C.GoString(result)), &schema); err != nil {
		return Schema{}, &ExtractError{Msg: "failed to decode schema JSON: " + err.Error()}
	}
	return schema, nil
}
