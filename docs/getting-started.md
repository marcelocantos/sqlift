# Getting Started with sqlift

This tutorial walks you through building a task management app with sqlift. You'll evolve the database schema across five iterations, each demonstrating a different capability of the library.

The central idea is that you maintain **one canonical DDL file** describing the schema you want. sqlift works out what has changed and applies only the necessary operations to bring any database up to date — whether it's brand new or running an older version of the schema.

## Setup

See the README for installation instructions. Once sqlift is available, the minimal boilerplate to open a database and import the library looks like this:

**C:**
```c
#include "sqlift.h"

int err_type;
char* err_msg = NULL;
sqlift_db* db = sqlift_db_open("taskflow.db", 0, &err_type, &err_msg);
// use db with sqlift functions
sqlift_db_close(db);
```

**Go:**
```go
import "github.com/marcelocantos/sqlift/go/sqlift"

db, err := sqlift.Open("taskflow.db")
if err != nil {
    // handle error
}
defer db.Close()
```

---

## Iteration 1: Initial schema — users and tasks

TaskFlow needs two tables: one for users, one for tasks.

```sql
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL
);

CREATE TABLE tasks (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_tasks_user ON tasks(user_id);
```

The workflow is always the same four steps: **parse** the desired DDL, **extract** the current schema from the live database, **diff** the two, and **apply** the plan. Here is the full implementation for both languages:

**C:**
```c
#include "sqlift.h"
#include <stdio.h>

static const char* kSchema =
    "CREATE TABLE users ("
    "    id INTEGER PRIMARY KEY,"
    "    username TEXT NOT NULL UNIQUE,"
    "    email TEXT NOT NULL"
    ");"
    "CREATE TABLE tasks ("
    "    id INTEGER PRIMARY KEY,"
    "    title TEXT NOT NULL,"
    "    description TEXT,"
    "    user_id INTEGER NOT NULL REFERENCES users(id),"
    "    created_at TEXT NOT NULL DEFAULT (datetime('now'))"
    ");"
    "CREATE INDEX idx_tasks_user ON tasks(user_id);";

void migrate(sqlift_db* db) {
    int err_type;
    char* err_msg = NULL;

    char* desired = sqlift_parse(kSchema, &err_type, &err_msg);
    char* current = sqlift_extract(db, &err_type, &err_msg);
    char* plan = sqlift_diff(current, desired, &err_type, &err_msg);

    // The plan is a JSON string -- print it to inspect operations.
    printf("%s\n", plan);

    sqlift_apply(db, plan, 0, &err_type, &err_msg);

    sqlift_free(plan);
    sqlift_free(current);
    sqlift_free(desired);
}
```

**Go:**
```go
const schema = `
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL
);

CREATE TABLE tasks (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_tasks_user ON tasks(user_id);
`

func migrate(db *sqlift.Database) error {
    desired, err := sqlift.Parse(schema)
    if err != nil {
        return err
    }

    current, err := sqlift.Extract(db)
    if err != nil {
        return err
    }

    plan, err := sqlift.Diff(current, desired)
    if err != nil {
        return err
    }

    // Inspect the plan before applying.
    for _, op := range plan.Operations() {
        fmt.Printf("%s %s\n", op.Type, op.ObjectName)
    }

    return sqlift.Apply(db, plan, sqlift.ApplyOptions{})
}
```

Against an empty database, the plan contains exactly three operations:

```
CreateTable users
CreateTable tasks
CreateIndex idx_tasks_user
```

Run the same code again on the same database and the plan will be empty — sqlift is idempotent.

---

## Iteration 2: Add columns (fast path)

Requirements change: tasks need a `status` field and an optional `due_date`. Update the `tasks` table in your schema file — you do not write a separate migration script:

```sql
CREATE TABLE tasks (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    status TEXT DEFAULT 'pending',
    due_date TEXT
);
```

The rest of the code stays exactly the same — parse, extract, diff, apply. sqlift inspects the live database, sees that `tasks` is missing two columns, and generates the plan automatically:

```
AddColumn tasks.status
AddColumn tasks.due_date
```

Because both new columns are either nullable or carry a DEFAULT, sqlift uses `ALTER TABLE ADD COLUMN` instead of rebuilding the table. This is the **fast path**: it is safe on large databases, requires no data movement, and holds no table-level locks beyond the normal SQLite write lock. See [guide.md](guide.md) for the conditions that qualify a change for the fast path.

---

## Iteration 3: Views and triggers

The product team wants a summary view and an auto-timestamp on updates. Add an `updated_at` column to `tasks`, then append the view and trigger to the schema file:

```sql
CREATE TABLE tasks (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    status TEXT DEFAULT 'pending',
    due_date TEXT,
    updated_at TEXT
);

CREATE VIEW pending_task_summary AS
    SELECT u.username, COUNT(t.id) AS pending_count
    FROM users u
    LEFT JOIN tasks t ON t.user_id = u.id AND t.status = 'pending'
    GROUP BY u.username;

CREATE TRIGGER tasks_update_timestamp AFTER UPDATE ON tasks
BEGIN
    UPDATE tasks SET updated_at = datetime('now') WHERE id = NEW.id;
END;
```

The same four-step code produces a multi-object plan:

```
AddColumn tasks.updated_at
CreateView pending_task_summary
CreateTrigger tasks_update_timestamp
```

sqlift applies operations in a fixed order that keeps the database consistent: drop triggers, drop views, drop indexes, table operations, create indexes, create views, create triggers. In this case, since nothing is being dropped, only the "create" half runs. See [guide.md](guide.md) for the full ordering rules.

---

## Iteration 4: Rebuilds and breaking changes

### 4a: Breaking change caught

The data team wants `description` to be mandatory. You update the column definition to `description TEXT NOT NULL`. When you call `Diff`, you get an error before any database is touched:

**C:**
```c
char* plan = sqlift_diff(current, desired, &err_type, &err_msg);
if (err_type == SQLIFT_BREAKING_CHANGE_ERROR) {
    fprintf(stderr, "Breaking change: %s\n", err_msg);
    // Breaking change: column tasks.description: nullable -> NOT NULL
    sqlift_free(err_msg);
}
```

**Go:**
```go
plan, err := sqlift.Diff(current, desired)
if err != nil {
    var bce *sqlift.BreakingChangeError
    if errors.As(err, &bce) {
        fmt.Println("Breaking change:", bce.Msg)
        // Breaking change: column tasks.description: nullable -> NOT NULL
    }
    return err
}
```

`Diff` rejects this change because it is **data-dependent**: the operation will succeed on a database where every existing task already has a description, but fail silently or catastrophically on one that doesn't. sqlift refuses to generate a plan whose correctness depends on your data. If you genuinely need this change, the right approach is to backfill NULLs first, then alter the schema — documented in [guide.md](guide.md).

### 4b: Table rebuild with destructive guard

Let's say `due_date` turned out to be unused, and you want to add a `priority` column with a non-nullable integer default instead. Update the `tasks` table:

```sql
CREATE TABLE tasks (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    status TEXT DEFAULT 'pending',
    updated_at TEXT,
    priority INTEGER NOT NULL DEFAULT 1
);
```

Removing `due_date` cannot be expressed as `ALTER TABLE`; SQLite requires recreating the table. The plan now contains a `RebuildTable` operation flagged as destructive:

```
RebuildTable tasks  (destructive)
```

Calling `Apply` without opting in raises a guard error:

**C:**
```c
// Returns SQLIFT_DESTRUCTIVE_ERROR if the plan drops anything
sqlift_apply(db, plan, /*allow_destructive=*/0, &err_type, &err_msg);
if (err_type == SQLIFT_DESTRUCTIVE_ERROR) {
    fprintf(stderr, "Destructive: %s\n", err_msg);
    sqlift_free(err_msg);
}

// Opt in explicitly:
sqlift_apply(db, plan, /*allow_destructive=*/1, &err_type, &err_msg);
```

**Go:**
```go
err = sqlift.Apply(db, plan, sqlift.ApplyOptions{})
if err != nil {
    var de *sqlift.DestructiveError
    if errors.As(err, &de) {
        fmt.Println("Destructive operation:", de.Msg)
    }
}

// Opt in explicitly:
err = sqlift.Apply(db, plan, sqlift.ApplyOptions{AllowDestructive: true})
```

The guard exists so that destructive migrations require a conscious decision at the call site. You might choose to check `plan.HasDestructiveOperations()` (Go) or parse the plan JSON to check for destructive operations (C) before applying, and surface a confirmation prompt to an operator.

---

## Iteration 5: Drift detection and JSON plans

### Drift detection

After your migration runs, a developer connects to the production database directly and adds a column by hand:

```sql
ALTER TABLE tasks ADD COLUMN notes TEXT;
```

The next time your application starts and runs the standard migrate routine, `Apply` detects that the live schema no longer matches the hash it stored after the last successful migration:

**C:**
```c
int rc = sqlift_apply(db, plan, 1, &err_type, &err_msg);
if (err_type == SQLIFT_DRIFT_ERROR) {
    fprintf(stderr, "Drift detected: %s\n", err_msg);
    // Schema has been modified outside of sqlift
    sqlift_free(err_msg);
}
```

**Go:**
```go
err = sqlift.Apply(db, plan, sqlift.ApplyOptions{AllowDestructive: true})
if err != nil {
    var de *sqlift.DriftError
    if errors.As(err, &de) {
        fmt.Println("Drift detected:", de.Msg)
    }
}
```

Drift detection uses a SHA-256 hash of the extracted schema stored in the `_sqlift_state` table. Every successful `Apply` updates the hash. If the hash on disk does not match a freshly extracted hash, `Apply` refuses to proceed — the database is in an unknown state that sqlift did not authorise.

### JSON plans

Plans can be serialised to JSON. This is useful for reviewing migrations in CI before they reach production, transmitting plans between environments, or building an audit trail.

**C:**
```c
// sqlift_diff() returns the plan as a JSON string directly.
// Write to a file, send over HTTP, log it, etc.
char* plan = sqlift_diff(current, desired, &err_type, &err_msg);

// Later, pass the saved JSON string to sqlift_apply():
sqlift_apply(db, plan, 1, &err_type, &err_msg);
sqlift_free(plan);
```

**Go:**
```go
data, err := sqlift.ToJSON(plan)
if err != nil {
    return err
}
// Write to a file, send over HTTP, log it, etc.
fmt.Println(string(data))

// Deserialise:
loaded, err := sqlift.FromJSON(data)
if err != nil {
    return err
}
err = sqlift.Apply(db, loaded, sqlift.ApplyOptions{AllowDestructive: true})
```

A serialised plan looks like this:

```json
{
  "version": 1,
  "operations": [
    {
      "type": "RebuildTable",
      "object_name": "tasks",
      "description": "rebuild tasks: drop column due_date, add column priority",
      "sql": ["..."],
      "destructive": true
    }
  ]
}
```

Plans are **cross-language compatible**: a plan serialised by the C library can be deserialised and applied by the Go library, and vice versa. This makes it practical to generate plans in a CI pipeline using one language and apply them from a deployment tool written in another.

---

## What's next

You've seen the full lifecycle: initial creation, fast-path column additions, multi-object diffs, breaking-change detection, table rebuilds with the destructive guard, drift detection, and portable JSON plans.

For deeper coverage of individual features:

- **[guide.md](guide.md)** — Concepts and workflows: CHECK constraints, COLLATE, GENERATED columns, STRICT tables, foreign key enforcement, the 12-step rebuild sequence, and strategies for breaking changes.
- **[reference.md](reference.md)** — Complete C API reference.
- **[reference-go.md](reference-go.md)** — Complete Go API reference.
