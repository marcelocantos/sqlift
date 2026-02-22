# sqlift

Declarative SQLite schema migration library. Two canonical source files: `sqlift.h` (header) and `sqlift.cpp` (implementation).

## Build

```sh
make          # build library + run tests
make lib      # build static library only
make test     # build and run tests
make clean    # remove build artifacts
```

Requires C++23, system SQLite3, and [doctest](https://github.com/doctest/doctest) (for tests only).

## Architecture

Three core operations, each a free function in `namespace sqlift`:

1. **`parse(sql)`** -- load DDL into `:memory:` SQLite DB, call `extract()` on it
2. **`extract(db)`** -- query `sqlite_master` + PRAGMAs to build a `Schema`
3. **`diff(current, desired)`** -- pure function, compares two `Schema` values, returns `MigrationPlan`

Plus one action:

4. **`apply(db, plan)`** -- execute the plan's SQL, with destructive guard and drift detection

`diff()` never touches a database. `apply()` stores a SHA-256 hash in `_sqlift_state` for drift detection.

## Key design decisions

- **No rename detection.** A disappearing column + appearing column = drop + add. Always.
- **`raw_sql` excluded from equality.** SQLite doesn't update `sqlite_master.sql` after `ALTER TABLE ADD COLUMN`, so `Table`/`Index` equality is structural only.
- **AddColumn fast path.** If the only change is appending nullable (or NOT NULL + DEFAULT) columns, use `ALTER TABLE ADD COLUMN` instead of a full rebuild.
- **12-step rebuild.** Any other table modification uses SQLite's recommended rebuild sequence (disable FKs, savepoint, create new table, copy data, drop old, rename, recreate indexes/triggers, FK check, release, re-enable FKs).
- **No logging dependency.** This is a library; the consumer owns logging. Errors are reported via exceptions.

## Diff operation ordering

1. Drop triggers  2. Drop views  3. Drop indexes  4. Table ops (create, rebuild, drop)  5. Create indexes  6. Create views  7. Create triggers

## File layout

```
sqlift.h          # All declarations
sqlift.cpp        # All implementations (~920 lines)
Makefile
tests/            # doctest suites (6 files, 41 tests)
docs/guide.md     # Concepts and workflows
docs/reference.md # Complete API reference
```

## Testing

All tests run in-memory (`:memory:` databases), no filesystem artifacts. Test files:

- `test_parse.cpp` -- parsing DDL into Schema
- `test_extract.cpp` -- extracting schema from live DB
- `test_diff.cpp` -- diffing two schemas
- `test_apply.cpp` -- applying plans, destructive guard, drift detection
- `test_roundtrip.cpp` -- end-to-end parse/diff/apply/extract cycles
