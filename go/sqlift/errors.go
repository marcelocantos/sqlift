// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

// ParseError indicates a failure to parse DDL SQL.
type ParseError struct{ Msg string }

func (e *ParseError) Error() string { return e.Msg }

// ExtractError indicates a failure to extract schema from a database.
type ExtractError struct{ Msg string }

func (e *ExtractError) Error() string { return e.Msg }

// DiffError indicates a failure during schema comparison.
type DiffError struct{ Msg string }

func (e *DiffError) Error() string { return e.Msg }

// ApplyError indicates a failure during plan application.
type ApplyError struct{ Msg string }

func (e *ApplyError) Error() string { return e.Msg }

// DriftError indicates that the database schema was modified outside of sqlift.
type DriftError struct{ Msg string }

func (e *DriftError) Error() string { return e.Msg }

// DestructiveError indicates that a plan contains destructive operations
// and AllowDestructive was not set.
type DestructiveError struct{ Msg string }

func (e *DestructiveError) Error() string { return e.Msg }

// BreakingChangeError indicates that the desired schema contains changes
// whose success depends on existing data.
type BreakingChangeError struct{ Msg string }

func (e *BreakingChangeError) Error() string { return e.Msg }

// JSONError indicates a failure in JSON serialization or deserialization.
type JSONError struct{ Msg string }

func (e *JSONError) Error() string { return e.Msg }
