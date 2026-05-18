// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// The bundled sqlift.cpp and sqlift.h here are copies of dist/sqlift.cpp
// and dist/sqlift.h; `mk bundle` keeps them in sync. The single-header
// nlohmann/json dependency is bundled under include/nlohmann/ so consumers
// can `go get` and build with cgo and no extra setup.
//
// Note: sqlift does NOT add `-lsqlite3` to LDFLAGS. The bundled sqlift.cpp
// uses sqlite3 symbols, but where those symbols come from is the consumer's
// choice. Common arrangements:
//   - import _ "github.com/mattn/go-sqlite3" — mattn bundles sqlite3.c
//     statically; sqlift's symbols resolve against mattn's copy.
//   - System sqlite3: set CGO_LDFLAGS=-lsqlite3 (works on Linux/macOS).
//   - Bundled amalgamation: link your own sqlite3.c via your package's cgo.
//
// Hardcoding -lsqlite3 here breaks cross-compile to targets without a
// matching libsqlite3 (e.g. windows/arm64 with llvm-mingw), and it
// duplicates the linker work for consumers who already bring sqlite3
// statically.

package sqlift

//#cgo CFLAGS:   -I${SRCDIR}
//#cgo CXXFLAGS: -std=c++23 -I${SRCDIR} -I${SRCDIR}/include
//#cgo LDFLAGS:  -lstdc++
//#include "sqlift.h"
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
	case C.SQLIFT_REBUILD_ERROR:
		return &RebuildError{Msg: msg}
	case C.SQLIFT_JSON_ERROR:
		return &JSONError{Msg: msg}
	default:
		return &ApplyError{Msg: msg}
	}
}
