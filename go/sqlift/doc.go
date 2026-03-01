// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package sqlift provides declarative SQLite schema migration.
//
// The core workflow is:
//
//  1. [Parse] desired DDL into a [Schema]
//  2. [Extract] the current schema from a live database
//  3. [Diff] the two schemas to produce a [MigrationPlan]
//  4. [Apply] the plan to the database
//
// Diff never touches a database. Apply stores a SHA-256 hash in
// _sqlift_state for drift detection.
package sqlift
