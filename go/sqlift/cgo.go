// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

//#cgo CFLAGS: -I${SRCDIR}/../../dist
//#cgo LDFLAGS: ${SRCDIR}/../../build/libsqlift.a -lsqlite3 -lstdc++
//#include "sqlift_c.h"
//#include <stdlib.h>
import "C"
import (
	"unsafe"
)

// goError converts C error type + message to Go error. Frees the C message.
func goError(errType C.int, errMsg *C.char) error {
	if errType == C.SQLIFT_OK {
		return nil
	}
	msg := C.GoString(errMsg)
	C.sqlift_free(unsafe.Pointer(errMsg))

	switch errType {
	case C.SQLIFT_PARSE_ERROR:
		return &ParseError{Msg: msg}
	case C.SQLIFT_EXTRACT_ERROR:
		return &ExtractError{Msg: msg}
	case C.SQLIFT_DIFF_ERROR:
		return &DiffError{Msg: msg}
	case C.SQLIFT_APPLY_ERROR:
		return &ApplyError{Msg: msg}
	case C.SQLIFT_DRIFT_ERROR:
		return &DriftError{Msg: msg}
	case C.SQLIFT_DESTRUCTIVE_ERROR:
		return &DestructiveError{Msg: msg}
	case C.SQLIFT_BREAKING_CHANGE_ERROR:
		return &BreakingChangeError{Msg: msg}
	case C.SQLIFT_JSON_ERROR:
		return &JSONError{Msg: msg}
	default:
		return &ApplyError{Msg: msg}
	}
}
