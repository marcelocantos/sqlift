# sqlift

Declarative SQLite schema migration library. Two files in `dist/`: `sqlift.h` (C-only public header) and `sqlift.cpp` (all implementation).

## Build

```sh
mk            # build library
mk test       # build and run tests
mk lib        # build static library only
mk clean      # remove build artifacts
```

Uses [mk](https://github.com/marcelocantos/mk) as the build tool (`mkfile`). Requires C++23 and system SQLite3. doctest and nlohmann/json are vendored in `vendor/include/`.

## Architecture

The public API is C-only (`extern "C"`) for FFI compatibility. Data interchange is JSON strings. C++ types exist only inside `sqlift.cpp`.

Three core operations exposed via the C API:

1. **`sqlift_parse(ddl)`** -- parse DDL, return schema as JSON
2. **`sqlift_extract(db)`** -- query live DB, return schema as JSON
3. **`sqlift_diff(current_json, desired_json)`** -- pure function, compare two schema JSONs, return migration plan as JSON

Plus one action:

4. **`sqlift_apply(db, plan_json, allow_destructive)`** -- execute the plan's SQL, with destructive guard and drift detection

`sqlift_diff()` never touches a database. `sqlift_apply()` stores a SHA-256 hash in `_sqlift_state` for drift detection.

## Key design decisions

- **C-only public API.** The header is plain C with `extern "C"` guards. All data interchange uses JSON strings. C++ types are implementation-internal.
- **Breaking change detection.** `sqlift_diff()` returns `SQLIFT_BREAKING_CHANGE_ERROR` for schema changes whose success depends on existing data: nullable→NOT NULL, adding FK constraints, new NOT NULL column without DEFAULT. These are rejected because they may succeed on one database instance but fail on another.
- **No rename detection.** A disappearing column + appearing column = drop + add. Always.
- **`raw_sql` excluded from equality.** SQLite doesn't update `sqlite_master.sql` after `ALTER TABLE ADD COLUMN`, so `Table`/`Index` equality is structural only.
- **AddColumn fast path.** If the only change is appending nullable (or NOT NULL + DEFAULT) columns, use `ALTER TABLE ADD COLUMN` instead of a full rebuild.
- **12-step rebuild.** Any other table modification uses SQLite's recommended rebuild sequence (disable FKs, savepoint, create new table, copy data, drop old, rename, recreate indexes/triggers, FK check, release, re-enable FKs).
- **No logging dependency.** This is a library; the consumer owns logging. Errors are reported via error codes and message strings.

## Diff operation ordering

1. Drop triggers  2. Drop views  3. Drop indexes  4. Table ops (create, rebuild, drop)  5. Create indexes  6. Create views  7. Create triggers

## File layout

```
dist/
  sqlift.h        # C-only public header (error codes, opaque handle, function declarations)
  sqlift.cpp      # All implementation (C++ types, logic, C wrapper)
  sqlift-agents-guide.md # Quick-start guide for AI coding agents
mkfile            # mk build file
tests/            # doctest suites (7 files, 125 tests) using C API with JSON
  test_helpers.h  # Test utilities (RAII wrappers, JSON convenience functions)
docs/guide.md     # Concepts and workflows
docs/reference.md # Complete API reference
```

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
