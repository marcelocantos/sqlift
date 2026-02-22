# Guide

## Overview

sqlift is built around three operations:

1. **Parse** -- turn SQL DDL text into a `Schema` object
2. **Extract** -- read the current schema from a live SQLite database
3. **Diff** -- compare two `Schema` objects and produce a `MigrationPlan`

Plus one action:

4. **Apply** -- execute a `MigrationPlan` against a database

The diff step is a pure function. It never touches a database. This makes it
trivially testable and enables workflows like diffing two `.sql` files with no
database involved.

## Basic workflow

The typical usage pattern looks like this:

```cpp
#include "sqlift.h"

void migrate(sqlite3* db) {
    // 1. Declare your desired schema
    sqlift::Schema desired = sqlift::parse(R"(
        CREATE TABLE users (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            email TEXT
        );
    )");

    // 2. Extract what's currently in the database
    sqlift::Schema current = sqlift::extract(db);

    // 3. Compute the diff
    sqlift::MigrationPlan plan = sqlift::diff(current, desired);

    // 4. Apply it
    if (!plan.empty())
        sqlift::apply(db, plan);
}
```

On first run against an empty database, the diff will produce a `CreateTable`
operation. On subsequent runs with an unchanged schema, the diff will be empty.
When the schema changes, sqlift computes the minimal set of operations to bring
the database in line.

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
validates your DDL -- if it's not valid SQLite, `parse()` will throw a
`ParseError`.

## Inspecting a migration plan

Every `MigrationPlan` is fully inspectable before execution:

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

Each `Operation` carries:

- `type` -- what kind of operation (see [Operation types](#operation-types))
- `object_name` -- the table, index, view, or trigger being affected
- `description` -- a human-readable summary
- `sql` -- the exact SQL statements that will be executed
- `destructive` -- whether this operation drops data

## Destructive operations

sqlift distinguishes between safe and destructive operations. Destructive
operations are those that lose data:

- Dropping a table
- Dropping an index
- Removing a column (via table rebuild)

By default, `apply()` refuses to execute a plan that contains destructive
operations:

```cpp
// This throws DestructiveError if the plan drops anything
sqlift::apply(db, plan);

// Opt in to destructive operations
sqlift::apply(db, plan, {.allow_destructive = true});
```

This is a safety net. In development you will typically pass
`allow_destructive = true`. In production, the refusal gives you a chance to
review the plan and confirm that dropping data is intentional.

## Drift detection

sqlift stores a SHA-256 hash of the schema in a `_sqlift_state` table after
each successful migration. On the next `apply()`, it compares the stored hash
against the actual database schema. If they differ -- meaning someone or
something modified the schema outside of sqlift -- it throws a `DriftError`.

```cpp
try {
    sqlift::apply(db, plan);
} catch (const sqlift::DriftError& e) {
    // Schema was modified outside of sqlift
    std::cerr << e.what() << "\n";
}
```

This catches accidental manual changes, other tools modifying the schema, or
bugs that issue raw DDL.

The `_sqlift_state` table is automatically excluded from schema extraction, so
it does not appear in diffs or interfere with your schema definitions.

## How changes are applied

### Simple column additions

When the only change to a table is appending new nullable columns (or NOT NULL
columns with a DEFAULT), sqlift uses `ALTER TABLE ... ADD COLUMN`. This is fast
and does not touch existing data.

### Table rebuilds

For any other table modification -- removing a column, changing a type, changing
nullability, modifying foreign keys -- sqlift uses SQLite's recommended
[12-step table rebuild](https://www.sqlite.org/lang_altertable.html):

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
their normalized SQL text; if the text differs, they are dropped and recreated.

## Operation types

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

## Operation ordering

sqlift orders operations to avoid constraint violations:

1. Drop triggers
2. Drop views
3. Drop indexes
4. Table operations (creates, rebuilds, drops)
5. Create indexes
6. Create views
7. Create triggers

Within table operations, new tables are created before tables that reference
them via foreign keys.

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
SQLite-specific PRAGMAs and behaviours rather than abstracting across databases.

## Using the RAII wrappers

sqlift includes lightweight RAII wrappers for `sqlite3*` and `sqlite3_stmt*`
that you may find useful even outside of schema migration:

```cpp
// Database opens on construction, closes on destruction
sqlift::Database db("app.db");
db.exec("INSERT INTO users (name) VALUES ('Alice')");

// Statement prepares on construction, finalizes on destruction
sqlift::Statement stmt(db, "SELECT name FROM users WHERE id = ?");
stmt.bind_int(1, 42);
if (stmt.step()) {
    std::string name = stmt.column_text(0);
}
```

Both are move-only (no copy). The `Database` class implicitly converts to
`sqlite3*`, so you can pass it directly to any function expecting a raw
SQLite handle.

## Error handling

All errors are reported via exceptions derived from `sqlift::Error` (which
derives from `std::runtime_error`):

| Exception | Thrown when |
|-----------|------------|
| `ParseError` | SQL passed to `parse()` is invalid |
| `ExtractError` | Schema extraction from a live database fails |
| `DiffError` | Schema comparison encounters an internal error |
| `ApplyError` | SQL execution fails during `apply()` (e.g. FK violation) |
| `DestructiveError` | Plan has destructive ops and `allow_destructive` is false |
| `DriftError` | Schema was modified outside of sqlift since last apply |

All exceptions carry a descriptive `what()` message.
