# sqlift -- Agent Guide

Declarative SQLite schema migration. Available as a C library (with Go bindings).

## C API

Two files: `dist/sqlift.h` (C-only header) + `dist/sqlift.cpp` (implementation).
Requires C++23 (for the implementation) and SQLite3.

```c
#include "sqlift.h"
```

All functions use `extern "C"` linkage. Data interchange is JSON strings.
Callers must free returned strings with `sqlift_free()`.

### Core workflow

```c
// 1. Open a database
int err_type;
char* err_msg;
sqlift_db* db = sqlift_db_open("app.db", 0, &err_type, &err_msg);

// 2. Declare desired schema as plain SQL, get schema JSON
char* desired = sqlift_parse(
    "CREATE TABLE users ("
    "    id INTEGER PRIMARY KEY,"
    "    name TEXT NOT NULL,"
    "    email TEXT UNIQUE"
    ");"
    "CREATE INDEX idx_email ON users(email);",
    &err_type, &err_msg);

// 3. Extract current schema from the live database
char* current = sqlift_extract(db, &err_type, &err_msg);

// 4. Diff (pure function, no DB access) -- returns plan JSON
char* plan = sqlift_diff(current, desired, &err_type, &err_msg);

// 5. Apply
sqlift_apply(db, plan, /*allow_destructive=*/0, &err_type, &err_msg);

// 6. Clean up
sqlift_free(plan);
sqlift_free(current);
sqlift_free(desired);
sqlift_db_close(db);
```

### API surface

#### Error codes

```c
enum sqlift_error_type {
    SQLIFT_OK               = 0,
    SQLIFT_ERROR            = 1,
    SQLIFT_PARSE_ERROR      = 2,
    SQLIFT_EXTRACT_ERROR    = 3,
    SQLIFT_DIFF_ERROR       = 4,
    SQLIFT_APPLY_ERROR      = 5,
    SQLIFT_DRIFT_ERROR      = 6,
    SQLIFT_DESTRUCTIVE_ERROR = 7,
    SQLIFT_BREAKING_CHANGE_ERROR = 8,
    SQLIFT_JSON_ERROR       = 9,
};
```

#### Functions

| Function | Returns | Does |
|----------|---------|------|
| `sqlift_db_open(path, flags, &et, &em)` | `sqlift_db*` | Open a database. NULL on error. |
| `sqlift_db_close(db)` | void | Close a database handle. |
| `sqlift_db_exec(db, sql, &em)` | int | Execute SQL with no result. 0 = success. |
| `sqlift_parse(ddl, &et, &em)` | `char*` | Parse DDL → schema JSON. NULL on error. |
| `sqlift_extract(db, &et, &em)` | `char*` | Extract schema from live DB → JSON. |
| `sqlift_diff(cur, des, &et, &em)` | `char*` | Diff two schema JSONs → plan JSON. |
| `sqlift_apply(db, plan, destr, &et, &em)` | int | Apply plan JSON to DB. 0 = success. |
| `sqlift_migration_version(db, &et, &em)` | `int64_t` | Migration version counter (0 if none). |
| `sqlift_detect_redundant_indexes(json, &et, &em)` | `char*` | Detect redundant indexes → warnings JSON. |
| `sqlift_schema_hash(json, &et, &em)` | `char*` | SHA-256 hash of a schema JSON. |
| `sqlift_db_query_int64(db, sql, &result, &em)` | int | Query returning single int64. |
| `sqlift_db_query_text(db, sql, &em)` | `char*` | Query returning single text value. |
| `sqlift_free(ptr)` | void | Free any string returned by the C API. |

All `char*` return values must be freed with `sqlift_free()`. Error outputs
(`err_type`, `err_msg`) are set on failure; `err_msg` must also be freed.

### Schema JSON format

`sqlift_parse` and `sqlift_extract` return JSON with this structure:

```json
{
  "tables": {
    "users": {
      "name": "users",
      "columns": [
        {
          "name": "id", "type": "INTEGER", "notnull": false,
          "default_value": "", "pk": 1, "collation": "",
          "generated": 0, "generated_expr": ""
        },
        {
          "name": "name", "type": "TEXT", "notnull": true,
          "default_value": "", "pk": 0, "collation": "",
          "generated": 0, "generated_expr": ""
        }
      ],
      "foreign_keys": [],
      "check_constraints": [],
      "pk_constraint_name": "",
      "without_rowid": false,
      "strict": false,
      "raw_sql": "CREATE TABLE users (...)"
    }
  },
  "indexes": { ... },
  "views": { ... },
  "triggers": { ... }
}
```

Column `generated` values: 0 = normal, 2 = virtual, 3 = stored.

### Plan JSON format

`sqlift_diff` returns:

```json
{
  "version": 1,
  "operations": [
    {
      "type": "CreateTable",
      "object_name": "users",
      "description": "Create table users",
      "sql": ["CREATE TABLE ..."],
      "destructive": false
    }
  ],
  "warnings": [
    {
      "type": "RedundantIndex",
      "message": "...",
      "index_name": "idx_foo",
      "covered_by": "PRIMARY KEY",
      "table_name": "t"
    }
  ]
}
```

Operation type strings: `CreateTable`, `DropTable`, `RebuildTable`,
`AddColumn`, `CreateIndex`, `DropIndex`, `CreateView`, `DropView`,
`CreateTrigger`, `DropTrigger`.

### Key behaviours

- **AddColumn fast path**: When the only change is appending nullable columns (or NOT NULL with DEFAULT) at the end, uses `ALTER TABLE ADD COLUMN`.
- **12-step table rebuild**: Any other table change uses SQLite's recommended rebuild (disable FKs, savepoint, create new, copy data, drop old, rename, recreate indexes/triggers/views, FK check, release, re-enable FKs).
- **Destructive guard**: `sqlift_apply()` returns `SQLIFT_DESTRUCTIVE_ERROR` unless `allow_destructive` is non-zero.
- **Drift detection**: Stores SHA-256 hash in `_sqlift_state` table after each apply. Returns `SQLIFT_DRIFT_ERROR` if schema changed outside sqlift.
- **Breaking change detection**: `sqlift_diff()` returns `SQLIFT_BREAKING_CHANGE_ERROR` for schema changes whose success depends on existing data. Detected cases: (1) existing nullable column becomes NOT NULL, (2) new FK constraint added to existing table, (3) new CHECK constraint added to existing table, (4) new NOT NULL column without DEFAULT.
- **No rename detection**: A removed + added column is always a drop + add.
- **Operation order**: Drop triggers/views/indexes, then table ops, then create indexes/views/triggers.

## Go

Module: `github.com/marcelocantos/sqlift/go/sqlift`. Requires CGo. Wraps
the C implementation via `#include "sqlift.h"` (no `database/sql` or
third-party driver).

```go
import "github.com/marcelocantos/sqlift/go/sqlift"
```

### Core workflow

```go
// 1. Declare desired schema as plain SQL
desired, err := sqlift.Parse(`
    CREATE TABLE users (
        id INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        email TEXT UNIQUE
    );
    CREATE INDEX idx_email ON users(email);
`)

// 2. Extract current schema from a live database
db, _ := sqlift.Open("app.db")
defer db.Close()
current, err := sqlift.Extract(db)

// 3. Diff (pure function, no DB access)
plan, err := sqlift.Diff(current, desired)

// 4. Apply
if !plan.Empty() {
    err = sqlift.Apply(db, plan, sqlift.ApplyOptions{})
}
```

### API surface

#### Functions

| Function | Signature | Does |
|----------|-----------|------|
| `Open` | `func Open(path string) (*Database, error)` | Open a SQLite database. |
| `Parse` | `func Parse(ddl string) (Schema, error)` | Parse DDL into Schema. Returns `*ParseError`. |
| `Extract` | `func Extract(db *Database) (Schema, error)` | Read schema from live DB. |
| `Diff` | `func Diff(current, desired Schema) (MigrationPlan, error)` | Pure diff. Returns `*BreakingChangeError` on unsafe changes. Populates warnings for redundant indexes. |
| `DetectRedundantIndexes` | `func DetectRedundantIndexes(schema Schema) []Warning` | Detect prefix-duplicate and PK-duplicate indexes. |
| `Apply` | `func Apply(db *Database, plan MigrationPlan, opts ApplyOptions) error` | Execute plan. Returns `*DestructiveError`, `*DriftError`, `*ApplyError`. |
| `ToJSON` | `func ToJSON(plan MigrationPlan) ([]byte, error)` | Serialize plan to JSON bytes. |
| `FromJSON` | `func FromJSON(data []byte) (MigrationPlan, error)` | Deserialize plan from JSON. Returns `*JSONError`. |
| `ParseOpType` | `func ParseOpType(s string) (OpType, error)` | Parse string to OpType. Returns `*JSONError`. |
| `MigrationVersion` | `func MigrationVersion(db *Database) (int64, error)` | Migration version counter. |

#### Database type

```go
type Database struct { /* opaque C handle */ }

func Open(path string) (*Database, error)
func (d *Database) Close()
func (d *Database) Exec(sql string) error
func (d *Database) QueryInt64(sql string) (int64, error)
func (d *Database) QueryText(sql string) (string, error)
```

#### Error types

All implement `error` with a `Msg string` field. Use `errors.As` for type assertions.

| Error type | When |
|------------|------|
| `*ParseError` | Invalid SQL in `Parse()` |
| `*ExtractError` | Schema extraction fails |
| `*DiffError` | Internal diff error |
| `*BreakingChangeError` | Schema change is backwards-incompatible |
| `*ApplyError` | SQL fails during `Apply()` |
| `*DestructiveError` | Plan has destructive ops, `AllowDestructive` is false |
| `*DriftError` | Schema modified outside sqlift since last `Apply()` |
| `*JSONError` | Invalid JSON in `FromJSON()` / `ParseOpType()` |

### Common patterns

```go
// Inspect plan before applying
for _, op := range plan.Operations() {
    fmt.Println(op.Description)
    if op.Destructive {
        fmt.Println("  [DESTRUCTIVE]")
    }
}

// Allow destructive operations (drops)
err := sqlift.Apply(db, plan, sqlift.ApplyOptions{AllowDestructive: true})

// Handle drift
var driftErr *sqlift.DriftError
if errors.As(err, &driftErr) {
    // Schema was modified outside sqlift
}

// Serialize plan to JSON
data, _ := sqlift.ToJSON(plan)

// Deserialize and apply on another machine
restored, _ := sqlift.FromJSON(data)
sqlift.Apply(db, restored, sqlift.ApplyOptions{})
```

### Cross-language compatibility

The Go and C implementations produce identical schema hashes. A database
migrated by one implementation can be read and further migrated by the other
without triggering drift detection.

## Agentic migration guidance

When `sqlift_diff()` returns `SQLIFT_BREAKING_CHANGE_ERROR`, agentic coding tools can suggest safe alternatives. Common patterns:

- **Nullable → NOT NULL**: Create a new table with the desired schema, write application code to copy data (coalescing NULLs), then drop the old table in a later release.
- **Adding FK constraints**: Create a new table with the FK, migrate data at the application level (filtering orphans), retire the old table.
- **New NOT NULL column without DEFAULT**: Add the column as nullable first, populate it via application code, then create a new table with the NOT NULL constraint in a later release.
