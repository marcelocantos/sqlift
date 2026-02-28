# sqlift

Declarative SQLite schema migration library. Two canonical source files: `dist/sqlift.h` (header) and `dist/sqlift.cpp` (implementation).

## Build

```sh
mk            # build library
mk test       # build and run tests
mk lib        # build static library only
mk clean      # remove build artifacts
```

Uses [mk](https://github.com/marcelocantos/mk) as the build tool (`mkfile`). Requires C++23 and system SQLite3. doctest is vendored in `vendor/include/`.

## Architecture

Three core operations, each a free function in `namespace sqlift`:

1. **`parse(sql)`** -- load DDL into `:memory:` SQLite DB, call `extract()` on it
2. **`extract(db)`** -- query `sqlite_master` + PRAGMAs to build a `Schema`
3. **`diff(current, desired)`** -- pure function, compares two `Schema` values, returns `MigrationPlan`

Plus one action:

4. **`apply(db, plan)`** -- execute the plan's SQL, with destructive guard and drift detection

`diff()` never touches a database. `apply()` stores a SHA-256 hash in `_sqlift_state` for drift detection.

## Key design decisions

- **Breaking change detection.** `diff()` throws `BreakingChangeError` for schema changes whose success depends on existing data: nullable→NOT NULL, adding FK constraints, new NOT NULL column without DEFAULT. These are rejected because they may succeed on one database instance but fail on another.
- **No rename detection.** A disappearing column + appearing column = drop + add. Always.
- **`raw_sql` excluded from equality.** SQLite doesn't update `sqlite_master.sql` after `ALTER TABLE ADD COLUMN`, so `Table`/`Index` equality is structural only.
- **AddColumn fast path.** If the only change is appending nullable (or NOT NULL + DEFAULT) columns, use `ALTER TABLE ADD COLUMN` instead of a full rebuild.
- **12-step rebuild.** Any other table modification uses SQLite's recommended rebuild sequence (disable FKs, savepoint, create new table, copy data, drop old, rename, recreate indexes/triggers, FK check, release, re-enable FKs).
- **No logging dependency.** This is a library; the consumer owns logging. Errors are reported via exceptions.

## Diff operation ordering

1. Drop triggers  2. Drop views  3. Drop indexes  4. Table ops (create, rebuild, drop)  5. Create indexes  6. Create views  7. Create triggers

## File layout

```
dist/
  sqlift.h        # All declarations
  sqlift.cpp      # All implementations (~1624 lines)
  agents-guide.md # Quick-start guide for AI coding agents
mkfile            # mk build file
tests/            # doctest suites (7 files, 108 tests)
docs/guide.md     # Concepts and workflows
docs/reference.md # Complete API reference
```

## TODOs

Tracked in `docs/TODO.md`.

## Testing

All tests run in-memory (`:memory:` databases), no filesystem artifacts. Test files:

- `test_parse.cpp` -- parsing DDL into Schema
- `test_extract.cpp` -- extracting schema from live DB
- `test_diff.cpp` -- diffing two schemas
- `test_apply.cpp` -- applying plans, destructive guard, drift detection
- `test_json.cpp` -- JSON serialization round-trips and error cases
- `test_roundtrip.cpp` -- end-to-end parse/diff/apply/extract cycles
