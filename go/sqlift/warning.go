// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

//#include "sqlift_c.h"
//#include <stdlib.h>
import "C"
import (
	"encoding/json"
	"unsafe"
)

// WarningType classifies the kind of schema warning.
type WarningType int

const (
	// RedundantIndex indicates an index whose columns duplicate or are a
	// prefix of another index or the table's PRIMARY KEY.
	RedundantIndex WarningType = iota
)

// Warning describes a non-fatal issue detected in a schema.
type Warning struct {
	Type      WarningType
	Message   string // Human-readable description.
	IndexName string // The redundant index.
	CoveredBy string // The covering index name, or "PRIMARY KEY".
	TableName string // The table both indexes belong to.
}

// DetectRedundantIndexes analyses a schema for prefix-duplicate and
// PK-duplicate indexes.
func DetectRedundantIndexes(schema Schema) []Warning {
	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		return nil
	}

	cschema := C.CString(string(schemaJSON))
	defer C.free(unsafe.Pointer(cschema))

	var errType C.int
	var errMsg *C.char
	result := C.sqlift_detect_redundant_indexes(cschema, &errType, &errMsg)
	if result == nil {
		return nil
	}
	defer C.sqlift_free(unsafe.Pointer(result))

	// Parse the JSON array of warnings.
	var jwarnings []struct {
		Type      string `json:"type"`
		Message   string `json:"message"`
		IndexName string `json:"index_name"`
		CoveredBy string `json:"covered_by"`
		TableName string `json:"table_name"`
	}
	if err := json.Unmarshal([]byte(C.GoString(result)), &jwarnings); err != nil {
		return nil
	}

	warnings := make([]Warning, len(jwarnings))
	for i, jw := range jwarnings {
		warnings[i] = Warning{
			Type:      RedundantIndex,
			Message:   jw.Message,
			IndexName: jw.IndexName,
			CoveredBy: jw.CoveredBy,
			TableName: jw.TableName,
		}
	}
	return warnings
}
