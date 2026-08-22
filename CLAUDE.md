# sqlift

Declarative SQLite schema migration library. Two files in `dist/`: `sqlift.h` (C-only public header) and `sqlift.cpp` (all implementation).

## Build

```sh
cv test       # build and run tests
cv lib        # build static library only
cv sanitize   # build and run tests with ASan/UBSan
cv clean      # remove build artifacts
```

Uses [cv](https://github.com/marcelocantos/cv) as the build tool (`cvfile`). Requires C++23 and system SQLite3. doctest and nlohmann/json are vendored in `vendor/include/`.

## Architecture

The public API is C-only (`extern "C"`) for FFI compatibility. Data interchange is JSON strings. C++ types exist only inside `sqlift.cpp`.

Three core operations exposed via the C API:

1. **`sqlift_parse(ddl)`** -- parse DDL, return schema as JSON
2. **`sqlift_extract(db)`** -- query live DB, return schema as JSON
3. **`sqlift_diff(current_json, desired_json)`** -- pure function, compare two schema JSONs, return migration plan as JSON

Plus one action:

4. **`sqlift_apply(db, plan_json, opts)`** -- execute the plan's SQL. `opts.allow` is a bitmask (`SQLIFT_ALLOW_REBUILD`, `SQLIFT_ALLOW_DESTRUCTIVE`, `SQLIFT_ALLOW_LOOSEN`, `SQLIFT_ALLOW_DATA_DEPENDENT`, `SQLIFT_ALLOW_ALL`); zero is the strictest policy (additive changes only).

`sqlift_diff()` never touches a database. `sqlift_apply()` stores a SHA-256 hash in `_sqlift_state` for drift detection.

## Key design decisions

- **C-only public API.** The header is plain C with `extern "C"` guards. All data interchange uses JSON strings. C++ types are implementation-internal.
- **Data-dependent change detection.** `sqlift_diff()` tags rebuild ops with `data_dependent: true` for schema changes whose success depends on existing data: nullable→NOT NULL, adding FK/CHECK constraints to an existing table, new NOT NULL column without DEFAULT.
  `sqlift_apply()` returns `SQLIFT_BREAKING_CHANGE_ERROR` unless `SQLIFT_ALLOW_DATA_DEPENDENT` is set. These are rejected by default because they may succeed on one database instance but fail on another.
- **Virtual tables.** `CREATE VIRTUAL TABLE` is extracted into schema JSON `virtual_tables` (FTS shadow tables with `sql IS NULL` are filtered). Diff treats any module/args change as drop+recreate (`DropVirtualTable` + `CreateVirtualTable`); there is no in-place alter.
- **No rename detection.** A disappearing column + appearing column = drop + add. Always.
- **`raw_sql` excluded from equality.** SQLite doesn't update `sqlite_master.sql` after `ALTER TABLE ADD COLUMN`, so `Table`/`Index` equality is structural only.
- **AddColumn fast path.** If the only change is appending nullable (or NOT NULL + DEFAULT) columns, use `ALTER TABLE ADD COLUMN` instead of a full rebuild.
- **12-step rebuild.** Any other table modification uses SQLite's recommended rebuild sequence (disable FKs, savepoint, create new table, copy data, drop old, rename, recreate indexes/triggers, FK check, release, re-enable FKs).
- **No logging dependency.** This is a library; the consumer owns logging. Errors are reported via error codes and message strings.

## Diff operation ordering

1. Drop triggers  2. Drop views  3. Drop indexes  4. Table ops (create, rebuild, drop)  5. Virtual table ops (drop then create)  6. Create indexes  7. Create views  8. Create triggers

## File layout

```
dist/
  sqlift.h        # C-only public header (error codes, opaque handle, function declarations)
  sqlift.cpp      # All implementation (C++ types, logic, C wrapper)
  sqlift-agents-guide.md # Quick-start guide for AI coding agents
cvfile            # cv build file
tests/            # doctest suites (8 files, 152 tests) using C API with JSON
  test_helpers.h  # Test utilities (RAII wrappers, JSON convenience functions)
docs/guide.md     # Concepts and workflows
docs/reference.md # Complete API reference
go/sqlift/        # Go module — cgo wrapper
  sqlift.cpp      # Copy of dist/sqlift.cpp (kept in sync by `cv bundle`)
  sqlift.h        # Copy of dist/sqlift.h (kept in sync by `cv bundle`)
  include/nlohmann/json.hpp # Copy of vendored nlohmann/json (kept in sync by `cv bundle`)
```

## Go module bundling

The Go module at `go/sqlift/` bundles its own copies of `sqlift.cpp`,
`sqlift.h`, and the vendored `nlohmann/json.hpp`. This is intentional:
cgo compiles `sqlift.cpp` directly as part of the package build, so
downstream Go consumers can `go get` and build without any separate
`cv lib` step.

**dist/ is the source of truth.** When you edit `dist/sqlift.cpp`,
`dist/sqlift.h`, or `vendor/include/nlohmann/json.hpp`, run `cv bundle`
to refresh the copies under `go/sqlift/`, then commit the result. CI
asserts the copies are in sync (the test-go job runs `cv bundle` and
fails if `git diff` reports any change).

## TODOs

Tracked in `docs/TODO.md`.

## Testing

All tests use the C API with JSON interchange. Tests run in-memory (`:memory:` databases), no filesystem artifacts. Test files:

- `test_parse.cpp` -- parsing DDL, checking schema JSON
- `test_extract.cpp` -- extracting schema from live DB via C API
- `test_diff.cpp` -- diffing two schema JSONs
- `test_apply.cpp` -- applying plan JSON, destructive guard, drift detection
- `test_json.cpp` -- JSON validation, round-trips, and error cases
- `test_roundtrip.cpp` -- end-to-end parse/diff/apply/extract cycles
- `test_virtual.cpp` -- CREATE VIRTUAL TABLE parse/extract/diff/apply, shadow-table filter, hash snapshots
