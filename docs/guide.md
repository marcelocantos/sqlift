# Guide

sqlift is a declarative SQLite schema migration library for C++ and Go.

## The problem with numbered migrations

Most migration tools -- goose, golang-migrate, Flyway, or hand-rolled scripts
-- work the same way. You write numbered SQL files: `001_create_users.sql`,
`007_add_email.sql`, `023_add_avatar.sql`, `031_rename_username.sql`,
`042_add_last_login.sql`. Each one is a delta. Your schema is the sum of every
delta ever written.

This model is broken in several ways.

**Your schema is scattered across dozens of files.** To understand what `users`
looks like today, you mentally replay every migration that ever touched it.
The actual structure exists nowhere as a single readable artefact. Hand a new
team member your migration directory and ask "what does our database look
like?" They cannot answer without reading every file in order.

**Merge conflicts on migration ordering.** Two developers on separate branches
both create `047_*.sql`. They discover the conflict at merge time. If the tool
uses timestamps instead of sequence numbers, conflicts are silent and ordering
is fragile -- two migrations that needed to run in a specific order may execute
in the wrong one.

**Migrations accumulate forever.** A three-year-old project has 200 migration
files. They can never be pruned because any database in the wild might be at
any version. The migration directory is append-only dead weight that nobody
reads but everybody ships.

**Errors surface at deploy time.** You write a migration, it passes on your
empty dev database, and it fails in production because existing data violates
the new constraint. You find out at 2am during a release.

**No single source of truth.** There is no file you can point to and say "this
is our database schema." The truth is a procedural history, not a declaration.

## The declarative alternative

sqlift takes a fundamentally different approach.

**One file, not forty.** You maintain a single `.sql` file containing `CREATE
TABLE`, `CREATE INDEX`, `CREATE VIEW`, and `CREATE TRIGGER` statements. This
file is your schema. It is also your documentation. A new team member reads one
file and knows exactly what the database looks like.

**Diff is automatic.** sqlift compares your declared schema against the live
database and computes the exact SQL operations needed to bring them in line.
You never write `ALTER TABLE` by hand.

**Diff is pure.** The comparison is a pure function that never touches a
database. You can diff two schemas in a unit test, in CI, in a pre-commit hook
-- anywhere you can run code.

**Errors surface at diff time.** If your schema change would fail on a
populated database -- nullable to NOT NULL, new foreign key, new NOT NULL
column without a default -- `diff()` rejects it immediately with a clear
error. Not at deploy time. Not at 2am.

**No ordering conflicts.** There are no numbered files. Two developers modify
the schema SQL independently. Merge conflicts are normal text conflicts on a
single file, resolved with normal merge tools.

For a hands-on walkthrough, see [Getting Started](getting-started.md).

## Core concepts

sqlift has four operations that form a pipeline:

```
schema.sql ──parse()──▶ Schema (desired)
                                         ╲
                                          diff() ──▶ MigrationPlan ──apply()──▶ DB updated
                                         ╱
live database ──extract()──▶ Schema (current)
```

**parse** loads DDL text into an in-memory SQLite database and extracts the
resulting schema. SQLite itself validates the DDL -- if it is not valid SQLite,
`parse()` reports an error immediately.

**extract** reads `sqlite_master` and PRAGMAs from a live database to build a
`Schema` value. sqlift's internal `_sqlift_state` table and SQLite's
auto-generated indexes are excluded automatically.

**diff** compares two `Schema` values and produces a `MigrationPlan` -- an
ordered list of SQL operations. This is a pure function. It never opens a
database connection, never reads a file, never performs I/O of any kind. You
can call it in a unit test with two in-memory schemas.

**apply** executes a plan's SQL against a live database. After a successful
migration, it stores a SHA-256 hash of the resulting schema in `_sqlift_state`
for drift detection on the next run.

Here is the basic workflow in both languages.

**C++:**

```cpp
sqlift::Schema desired = sqlift::parse(R"(
    CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
)");
sqlift::Database db("app.db");
sqlift::Schema current = sqlift::extract(db);
sqlift::MigrationPlan plan = sqlift::diff(current, desired);
if (!plan.empty())
    sqlift::apply(db, plan);
```

**Go:**

```go
desired, err := sqlift.Parse(`
    CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
`)
// handle err
db, _ := sqlift.Open("app.db")
defer db.Close()
current, err := sqlift.Extract(db)
plan, err := sqlift.Diff(current, desired)
if !plan.Empty() {
    err = sqlift.Apply(db, plan, sqlift.ApplyOptions{})
}
```

On first run against an empty database, the diff produces a `CreateTable`
operation. On subsequent runs with an unchanged schema, the diff is empty.
When the schema changes, sqlift computes the minimal set of operations to
bring the database in line.

## Schema input

sqlift accepts plain SQL -- the same DDL you would use to create the database
from scratch:

```sql
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT UNIQUE,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE posts (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    body TEXT
);

CREATE INDEX idx_posts_user ON posts(user_id);

CREATE VIEW recent_posts AS
    SELECT * FROM posts ORDER BY id DESC LIMIT 100;

CREATE TRIGGER on_user_delete AFTER DELETE ON users
BEGIN
    DELETE FROM posts WHERE user_id = OLD.id;
END;
```

Internally, sqlift loads this SQL into an in-memory SQLite database and reads
back the schema using `sqlite_master` and PRAGMAs. This means SQLite itself
validates your DDL -- if it is not valid SQLite, `parse()` raises a
`ParseError`.

## Inspecting plans

Every `MigrationPlan` is fully inspectable before execution. You can iterate
its operations and decide whether to proceed, log the plan, or present it for
human review.

**C++:**

```cpp
sqlift::MigrationPlan plan = sqlift::diff(current, desired);
for (const auto& op : plan.operations()) {
    std::cout << op.description << "\n";
    for (const auto& sql : op.sql)
        std::cout << "  " << sql << "\n";
    if (op.destructive)
        std::cout << "  [DESTRUCTIVE]\n";
}
```

**Go:**

```go
plan, err := sqlift.Diff(current, desired)
for _, op := range plan.Operations() {
    fmt.Println(op.Description)
    for _, sql := range op.SQL {
        fmt.Println("  " + sql)
    }
    if op.Destructive {
        fmt.Println("  [DESTRUCTIVE]")
    }
}
```

Each `Operation` carries:

- `type` / `Type` -- what kind of operation (see [Operation types](#operation-types-and-ordering))
- `object_name` / `ObjectName` -- the table, index, view, or trigger being affected
- `description` / `Description` -- a human-readable summary
- `sql` / `SQL` -- the exact SQL statements that will be executed
- `destructive` / `Destructive` -- whether this operation drops data

## Destructive operations

sqlift distinguishes between safe and destructive operations. Destructive
operations are those that lose data:

- Dropping a table
- Dropping an index
- Removing a column (via table rebuild)

By default, `apply()` refuses to execute a plan that contains destructive
operations. This is a safety net: in development you typically allow them, while
in production the refusal gives you a chance to review the plan and confirm that
dropping data is intentional.

**C++:**

```cpp
// This throws DestructiveError if the plan drops anything
sqlift::apply(db, plan);

// Opt in to destructive operations
sqlift::apply(db, plan, {.allow_destructive = true});
```

**Go:**

```go
// This returns a *DestructiveError if the plan drops anything
err := sqlift.Apply(db, plan, sqlift.ApplyOptions{})

// Opt in to destructive operations
err = sqlift.Apply(db, plan, sqlift.ApplyOptions{AllowDestructive: true})
```

## Breaking change detection

`diff()` detects schema changes whose success depends on existing data --
changes that might work on an empty database but fail on a populated one. When
detected, it reports a `BreakingChangeError` instead of producing a plan.

There are four detected cases:

1. **Nullable to NOT NULL.** An existing nullable column becomes NOT NULL.
   Rows containing NULL values in that column would cause the table rebuild to
   fail. Whether this succeeds depends entirely on the data -- it might work on
   your dev database and fail in production.

2. **New foreign key on existing table.** Adding an FK constraint to a table
   that already has data. If any rows contain values that do not match the
   referenced table, the FK check at the end of the rebuild fails.

3. **New CHECK constraint on existing table.** Adding a CHECK constraint to a
   table that already has data. Existing rows may violate the new constraint,
   and there is no way to know without scanning every row.

4. **New NOT NULL column without DEFAULT.** Adding a column that is NOT NULL
   with no DEFAULT value. Existing rows cannot be populated -- SQLite has no
   value to put in the new column.

**C++:**

```cpp
try {
    auto plan = sqlift::diff(current, desired);
} catch (const sqlift::BreakingChangeError& e) {
    std::cerr << e.what() << "\n";
}
```

**Go:**

```go
plan, err := sqlift.Diff(current, desired)
var breakErr *sqlift.BreakingChangeError
if errors.As(err, &breakErr) {
    log.Println(breakErr.Msg)
}
```

The recommended workaround is a two-step migration: first add a new table or
column with the desired schema and migrate data at the application level, then
retire the old structure in a subsequent release.

## Redundant index detection

`diff()` analyses the desired schema for redundant indexes and includes
warnings in the returned plan. Two types of redundancy are detected:

**Prefix-duplicate indexes.** If index A covers columns `(name, email)` and
index B covers only `(name)` on the same table, B is redundant -- A already
handles lookups on `name` alone. However, if B is `UNIQUE`, it is NOT flagged
because it enforces a uniqueness constraint that A does not.

**PK-duplicate indexes.** An explicit index whose columns match or are a prefix
of the table's `PRIMARY KEY`. SQLite maintains an implicit index on PK columns,
so an explicit index duplicating them wastes space. A `UNIQUE` index that
exactly matches the PK columns is also flagged (the PK already implies
uniqueness), but a `UNIQUE` index on a strict prefix of the PK is not (it
enforces a tighter constraint).

Warnings are informational -- they do not prevent `diff()` or `apply()` from
succeeding. Inspect them to clean up unnecessary indexes:

**C++:**

```cpp
auto plan = sqlift::diff(current, desired);
for (const auto& w : plan.warnings())
    std::cerr << w.message << "\n";
```

**Go:**

```go
plan, err := sqlift.Diff(current, desired)
for _, w := range plan.Warnings() {
    log.Println(w.Message)
}
```

You can also analyse a schema independently:

**C++:**

```cpp
auto warnings = sqlift::detect_redundant_indexes(desired);
```

**Go:**

```go
warnings := sqlift.DetectRedundantIndexes(desired)
```

## Drift detection

sqlift stores a SHA-256 hash of the schema in a `_sqlift_state` table after
each successful migration. On the next `apply()`, it compares the stored hash
against the actual database schema. If they differ -- meaning someone or
something modified the schema outside of sqlift -- it reports a `DriftError`.

This catches accidental manual `ALTER TABLE` statements, other tools modifying
the schema, or bugs that issue raw DDL.

The `_sqlift_state` table is automatically excluded from schema extraction, so
it does not appear in diffs or interfere with your schema definitions.

**C++:**

```cpp
try {
    sqlift::apply(db, plan);
} catch (const sqlift::DriftError& e) {
    std::cerr << "Schema was modified outside of sqlift: " << e.what() << "\n";
}
```

**Go:**

```go
err := sqlift.Apply(db, plan, sqlift.ApplyOptions{})
var driftErr *sqlift.DriftError
if errors.As(err, &driftErr) {
    log.Println("Schema was modified outside of sqlift:", driftErr.Msg)
}
```

## How changes are applied

### AddColumn fast path

When the only change to a table is appending new nullable columns (or NOT NULL
columns with a DEFAULT value), sqlift uses `ALTER TABLE ... ADD COLUMN`. This
is fast and does not touch existing data.

### 12-step table rebuild

For any other table modification -- removing a column, changing a type, changing
nullability, modifying foreign keys, adding constraints -- sqlift uses SQLite's
recommended [12-step table rebuild](https://www.sqlite.org/lang_altertable.html):

1. Disable foreign keys
2. Begin a savepoint
3. Create a new table with the desired schema
4. Copy data from the old table (common columns only)
5. Drop the old table
6. Rename the new table
7. Recreate indexes
8. Recreate triggers
9. Recreate views referencing the table
10. Run foreign key check
11. Release the savepoint
12. Re-enable foreign keys

Data in columns that exist in both the old and new schemas is preserved.
Columns that only exist in the old schema are dropped. New columns get their
DEFAULT value (or NULL if no default is specified).

### Indexes, views, and triggers

Changed indexes are dropped and recreated. Views and triggers are compared by
their normalised SQL text; if the text differs, they are dropped and recreated.

## Operation types and ordering

| Type | Description | Destructive |
|------|-------------|-------------|
| `CreateTable` | Create a new table | No |
| `DropTable` | Drop a table | Yes |
| `RebuildTable` | Rebuild a table (12-step) | If columns are removed |
| `AddColumn` | Add a column via ALTER TABLE | No |
| `CreateIndex` | Create a new index | No |
| `DropIndex` | Drop an index | Yes |
| `CreateView` | Create a new view | No |
| `DropView` | Drop a view | Yes (if permanently removed) |
| `CreateTrigger` | Create a new trigger | No |
| `DropTrigger` | Drop a trigger | Yes (if permanently removed) |

sqlift orders operations to avoid constraint violations:

1. Drop triggers
2. Drop views
3. Drop indexes
4. Table operations (creates, rebuilds, drops)
5. Create indexes
6. Create views
7. Create triggers

Within table operations, new tables are created before tables that reference
them via foreign keys. Views and triggers are ordered by dependency analysis --
sqlift extracts SQL references from each object and uses topological sort
(Kahn's algorithm) to ensure dependents are dropped before their dependencies
and created after them. A circular dependency raises a `DiffError`.

## Feature details

### CHECK constraints

sqlift detects `CHECK` constraints, both unnamed and named (via `CONSTRAINT
name CHECK(...)`):

```sql
CREATE TABLE products (
    id INTEGER PRIMARY KEY,
    price REAL CHECK(price > 0),
    CONSTRAINT valid_name CHECK(length(name) > 0)
);
```

CHECK constraints are included in structural equality comparisons. Changing a
CHECK expression triggers a table rebuild. Adding a CHECK constraint to an
existing table is a breaking change -- existing rows may violate the new
constraint, so `diff()` raises `BreakingChangeError`.

### COLLATE clauses

Column collation sequences are extracted and compared. The default collation
(BINARY) is stored as an empty string. Changing a column's collation triggers
a table rebuild.

```sql
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT COLLATE NOCASE
);
```

### GENERATED columns

sqlift supports `GENERATED ALWAYS AS (expr) STORED` and `GENERATED ALWAYS AS
(expr) VIRTUAL` columns:

```sql
CREATE TABLE people (
    id INTEGER PRIMARY KEY,
    first_name TEXT,
    last_name TEXT,
    full_name TEXT GENERATED ALWAYS AS (first_name || ' ' || last_name) STORED
);
```

Generated columns cannot be added via `ALTER TABLE ADD COLUMN` -- they always
require a table rebuild. During rebuilds, generated columns are excluded from
the `INSERT INTO ... SELECT` data copy since their values are computed
automatically.

### STRICT tables

sqlift detects the `STRICT` table option:

```sql
CREATE TABLE data (
    id INTEGER PRIMARY KEY,
    value TEXT NOT NULL
) STRICT;
```

The `strict` flag is included in structural equality. Changing it triggers a
table rebuild.

## JSON serialisation

Migration plans can be serialised to JSON for storage, review in CI, or
transmission between environments. The JSON format is versioned (`"version":
1`). Deserialisation validates all required fields and raises `JsonError` /
`JSONError` on any parsing or validation failure.

**C++:**

```cpp
std::string json = sqlift::to_json(plan);

sqlift::MigrationPlan restored = sqlift::from_json(json);
sqlift::apply(db, restored);
```

**Go:**

```go
data, err := sqlift.ToJSON(plan)

restored, err := sqlift.FromJSON(data)
err = sqlift.Apply(db, restored, sqlift.ApplyOptions{})
```

## Cross-language compatibility

The C++ and Go implementations produce identical SHA-256 schema hashes. A
database migrated by one implementation can be continued by the other without
triggering drift detection. JSON-serialised migration plans are interchangeable
between the two implementations as well -- you can generate a plan in Go,
serialise it, and apply it from C++, or vice versa.

## What sqlift does not do

**Rename detection.** If a column disappears and a new one appears, sqlift
treats this as a drop and an add. It will never guess that you meant to rename
something. If you need to preserve data across a schema change that resembles a
rename, do it in two releases:

1. Add the new column/table, deploy application code that copies data
2. Remove the old column/table in a subsequent release

**Data migration.** sqlift is a schema tool. It does not transform, backfill,
or migrate data. If a schema change requires data transformation (e.g.
splitting a name column into first and last), handle that in application code
between the diff and the apply, or in a separate migration step.

**Cross-database support.** sqlift is SQLite-only by design. It exploits
SQLite-specific PRAGMAs and behaviours rather than abstracting across database
engines.

## Error handling

Errors are reported via exceptions in C++ and error values in Go.

**C++ exceptions** (all inherit from `sqlift::Error`, which inherits from
`std::runtime_error`):

| Exception | Thrown when |
|-----------|------------|
| `ParseError` | SQL passed to `parse()` is invalid |
| `ExtractError` | Schema extraction from a live database fails |
| `DiffError` | Schema comparison encounters an internal error |
| `ApplyError` | SQL execution fails during `apply()` (e.g. FK violation) |
| `DestructiveError` | Plan has destructive ops and `allow_destructive` is false |
| `DriftError` | Schema was modified outside of sqlift since last apply |
| `BreakingChangeError` | Schema change depends on existing data |
| `JsonError` | Invalid JSON or missing fields in `from_json()` |

All exceptions carry a descriptive `what()` message.

**Go error types** (independent struct types implementing `error`; use
`errors.As` to match):

| Error type | Returned when |
|------------|---------------|
| `*ParseError` | DDL passed to `Parse()` is invalid |
| `*ExtractError` | Schema extraction from a live database fails |
| `*DiffError` | Schema comparison encounters an internal error |
| `*ApplyError` | SQL execution fails during `Apply()` (e.g. FK violation) |
| `*DestructiveError` | Plan has destructive ops and `AllowDestructive` is false |
| `*DriftError` | Schema was modified outside of sqlift since last apply |
| `*BreakingChangeError` | Schema change depends on existing data |
| `*JSONError` | Invalid JSON or missing fields in `FromJSON()` |

Each error type has a `Msg` field with a descriptive message.

## Utility classes (C++ only)

sqlift includes lightweight RAII wrappers for `sqlite3*` and `sqlite3_stmt*`.
The Go implementation wraps the C++ library via an `extern "C"` interface,
providing its own `Database` type.

```cpp
// Database opens on construction, closes on destruction
sqlift::Database db("app.db");
db.exec("INSERT INTO users (name) VALUES ('Alice')");

// Statement prepares on construction, finalises on destruction
sqlift::Statement stmt(db, "SELECT name FROM users WHERE id = ?");
stmt.bind_int(1, 42);
if (stmt.step()) {
    std::string name = stmt.column_text(0);
}
```

Both are move-only (no copy). The `Database` class implicitly converts to
`sqlite3*`, so you can pass it directly to any function expecting a raw SQLite
handle.

---

For complete API details, see [C++ Reference](reference.md) and
[Go Reference](reference-go.md).
