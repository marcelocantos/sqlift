# C API Reference

Include `"sqlift.h"` to access the complete API. All functions use `extern "C"`
linkage. Data interchange is JSON strings -- callers must free returned strings
with `sqlift_free()`.

For the Go API, see [Go API Reference](reference-go.md). For conceptual
background, see [Guide](guide.md).

## Version macros

```c
#define SQLIFT_VERSION       "0.11.0"
#define SQLIFT_VERSION_MAJOR 0
#define SQLIFT_VERSION_MINOR 11
#define SQLIFT_VERSION_PATCH 0
```

---

## Error codes

All functions report errors via an `int* err_type` output parameter. On
success, `*err_type` is set to `SQLIFT_OK`. On failure, it is set to one of:

```c
enum sqlift_error_type {
    SQLIFT_OK                    = 0,
    SQLIFT_ERROR                 = 1,
    SQLIFT_PARSE_ERROR           = 2,
    SQLIFT_EXTRACT_ERROR         = 3,
    SQLIFT_DIFF_ERROR            = 4,
    SQLIFT_APPLY_ERROR           = 5,
    SQLIFT_DRIFT_ERROR           = 6,
    SQLIFT_DESTRUCTIVE_ERROR     = 7,
    SQLIFT_BREAKING_CHANGE_ERROR = 8,
    SQLIFT_JSON_ERROR            = 9,
};
```

When an error occurs, the accompanying `char** err_msg` output is set to a
heap-allocated string describing the failure. The caller must free it with
`sqlift_free()`.

| Code | Meaning |
|------|---------|
| `SQLIFT_OK` | Success |
| `SQLIFT_ERROR` | General error (e.g. SQLite failure) |
| `SQLIFT_PARSE_ERROR` | Invalid DDL passed to `sqlift_parse()` |
| `SQLIFT_EXTRACT_ERROR` | Schema extraction from a live database failed |
| `SQLIFT_DIFF_ERROR` | Internal error during schema comparison |
| `SQLIFT_APPLY_ERROR` | SQL execution failed during `sqlift_apply()` (e.g. FK violation) |
| `SQLIFT_DRIFT_ERROR` | Schema was modified outside sqlift since the last `sqlift_apply()` |
| `SQLIFT_DESTRUCTIVE_ERROR` | Plan has destructive operations and `allow_destructive` is 0 |
| `SQLIFT_BREAKING_CHANGE_ERROR` | Schema change depends on existing data (see [Breaking change detection](guide.md#breaking-change-detection)) |
| `SQLIFT_JSON_ERROR` | Invalid JSON or missing fields in plan JSON |

---

## Database handle

### `sqlift_db_open`

```c
sqlift_db* sqlift_db_open(const char* path, int flags,
                          int* err_type, char** err_msg);
```

Open a SQLite database and return an opaque handle.

**Parameters:**
- `path` -- database file path. Use `":memory:"` for an in-memory database.
- `flags` -- SQLite open flags. Pass 0 for the default
  (`SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE`).
- `err_type` -- receives the error code on failure.
- `err_msg` -- receives a heap-allocated error message on failure (free with
  `sqlift_free()`).

**Returns:** an opaque `sqlift_db*` handle, or `NULL` on error.

---

### `sqlift_db_close`

```c
void sqlift_db_close(sqlift_db* db);
```

Close a database handle and release its resources. Safe to call with `NULL`.

---

### `sqlift_db_exec`

```c
int sqlift_db_exec(sqlift_db* db, const char* sql, char** err_msg);
```

Execute one or more SQL statements with no result rows.

**Parameters:**
- `db` -- an open database handle.
- `sql` -- SQL to execute.
- `err_msg` -- receives a heap-allocated error message on failure (free with
  `sqlift_free()`).

**Returns:** 0 on success, non-zero on error.

---

### `sqlift_db_query_int64`

```c
int sqlift_db_query_int64(sqlift_db* db, const char* sql,
                          int64_t* result, char** err_msg);
```

Execute a query that returns a single `int64` value.

**Parameters:**
- `db` -- an open database handle.
- `sql` -- a SQL query returning one row with one integer column.
- `result` -- receives the value.
- `err_msg` -- receives a heap-allocated error message on failure.

**Returns:** 0 on success, non-zero on error.

---

### `sqlift_db_query_text`

```c
char* sqlift_db_query_text(sqlift_db* db, const char* sql, char** err_msg);
```

Execute a query that returns a single text value.

**Parameters:**
- `db` -- an open database handle.
- `sql` -- a SQL query returning one row with one text column.
- `err_msg` -- receives a heap-allocated error message on failure.

**Returns:** a heap-allocated string (free with `sqlift_free()`), or `NULL` on
error. Returns an empty string if the query produces no rows.

---

## Core functions

### `sqlift_parse`

```c
char* sqlift_parse(const char* ddl, int* err_type, char** err_msg);
```

Parse SQL DDL statements into a schema. Internally creates a `:memory:` SQLite
database, executes the DDL, and extracts the resulting schema.

**Parameters:**
- `ddl` -- one or more DDL statements (`CREATE TABLE`, `CREATE INDEX`,
  `CREATE VIEW`, `CREATE TRIGGER`). Statements are separated by semicolons.
- `err_type` -- receives `SQLIFT_PARSE_ERROR` on failure.
- `err_msg` -- receives a heap-allocated error message on failure.

**Returns:** a heap-allocated JSON string representing the schema (see
[Schema JSON format](#schema-json-format)), or `NULL` on error. Free with
`sqlift_free()`.

**Example:**
```c
int err_type;
char* err_msg = NULL;
char* schema = sqlift_parse(
    "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);"
    "CREATE INDEX idx_name ON users(name);",
    &err_type, &err_msg);
if (!schema) {
    fprintf(stderr, "Parse error: %s\n", err_msg);
    sqlift_free(err_msg);
}
// ... use schema ...
sqlift_free(schema);
```

---

### `sqlift_extract`

```c
char* sqlift_extract(sqlift_db* db, int* err_type, char** err_msg);
```

Extract the current schema from a live SQLite database by querying
`sqlite_master` and PRAGMAs (`table_xinfo`, `foreign_key_list`, `index_info`).

The `_sqlift_state` table and `sqlite_autoindex_*` entries are excluded.

**Parameters:**
- `db` -- an open database handle.
- `err_type` -- receives `SQLIFT_EXTRACT_ERROR` on failure.
- `err_msg` -- receives a heap-allocated error message on failure.

**Returns:** a heap-allocated JSON string representing the schema, or `NULL` on
error. Free with `sqlift_free()`.

---

### `sqlift_diff`

```c
char* sqlift_diff(const char* current_json, const char* desired_json,
                  int* err_type, char** err_msg);
```

Compare two schemas and produce a migration plan. This is a pure function -- it
does not access any database.

**Parameters:**
- `current_json` -- schema JSON as it exists now (typically from
  `sqlift_extract()`).
- `desired_json` -- schema JSON you want (typically from `sqlift_parse()`).
- `err_type` -- receives `SQLIFT_DIFF_ERROR` or `SQLIFT_BREAKING_CHANGE_ERROR`
  on failure.
- `err_msg` -- receives a heap-allocated error message on failure.

**Returns:** a heap-allocated JSON string representing the migration plan (see
[Plan JSON format](#plan-json-format)), or `NULL` on error. Free with
`sqlift_free()`.

Returns a plan with an empty `operations` array if the schemas are identical.
Any redundant indexes in the desired schema are reported in the plan's
`warnings` array.

---

### `sqlift_apply`

```c
int sqlift_apply(sqlift_db* db, const char* plan_json, int allow_destructive,
                 int* err_type, char** err_msg);
```

Execute a migration plan against a live database.

**Parameters:**
- `db` -- an open database handle.
- `plan_json` -- the plan JSON to execute (from `sqlift_diff()` or
  deserialized).
- `allow_destructive` -- if 0, returns `SQLIFT_DESTRUCTIVE_ERROR` when the plan
  contains destructive operations. Set to non-zero to permit drops.
- `err_type` -- receives the error code on failure.
- `err_msg` -- receives a heap-allocated error message on failure.

**Returns:** 0 on success, non-zero on error.

**Possible errors:**
- `SQLIFT_DESTRUCTIVE_ERROR` if the plan contains destructive operations and
  `allow_destructive` is 0.
- `SQLIFT_DRIFT_ERROR` if the database schema has been modified since the last
  `sqlift_apply()` (detected via stored hash in `_sqlift_state`).
- `SQLIFT_APPLY_ERROR` if any SQL statement fails during execution (e.g. a
  foreign key check violation during a table rebuild).

After successful execution, updates the schema hash and increments the
migration version counter in `_sqlift_state`.

---

## Utility functions

### `sqlift_migration_version`

```c
int64_t sqlift_migration_version(sqlift_db* db, int* err_type, char** err_msg);
```

Return the migration version counter. Starts at 0 (no migrations have run) and
increments by 1 each time `sqlift_apply()` executes a non-empty plan.

**Parameters:**
- `db` -- an open database handle.
- `err_type` -- receives the error code on failure.
- `err_msg` -- receives a heap-allocated error message on failure.

**Returns:** the current migration version (0 if no migrations have been
applied).

---

### `sqlift_detect_redundant_indexes`

```c
char* sqlift_detect_redundant_indexes(const char* schema_json,
                                      int* err_type, char** err_msg);
```

Analyse a schema for redundant indexes. Detects two kinds:

- **Prefix-duplicate:** a non-unique index whose columns are a prefix of
  another index on the same table (with the same `WHERE` clause).
- **PK-duplicate:** an index whose columns are a prefix of the table's
  `PRIMARY KEY` columns (non-unique), or an exact match (even if unique, since
  the PK already implies uniqueness).

`sqlift_diff()` calls this on the desired schema automatically, but it is also
available as a standalone function for direct schema analysis.

**Parameters:**
- `schema_json` -- schema JSON string.
- `err_type` -- receives the error code on failure.
- `err_msg` -- receives a heap-allocated error message on failure.

**Returns:** a heap-allocated JSON array of warnings (see
[Warning JSON format](#warning-json-format)), or `NULL` on error. Free with
`sqlift_free()`.

---

### `sqlift_schema_hash`

```c
char* sqlift_schema_hash(const char* schema_json,
                         int* err_type, char** err_msg);
```

Compute a deterministic SHA-256 hash of a schema. Used internally for drift
detection; also available for cross-language hash verification.

**Parameters:**
- `schema_json` -- schema JSON string.
- `err_type` -- receives the error code on failure.
- `err_msg` -- receives a heap-allocated error message on failure.

**Returns:** a heap-allocated hex-encoded SHA-256 hash string, or `NULL` on
error. Free with `sqlift_free()`.

---

## Memory management

### `sqlift_free`

```c
void sqlift_free(void* ptr);
```

Free any heap-allocated string or buffer returned by the C API. This includes
return values from `sqlift_parse()`, `sqlift_extract()`, `sqlift_diff()`,
`sqlift_detect_redundant_indexes()`, `sqlift_schema_hash()`,
`sqlift_db_query_text()`, and error messages.

Safe to call with `NULL`.

---

## JSON data formats

All data interchange between the caller and the C API uses JSON strings.

### Schema JSON format

Returned by `sqlift_parse()` and `sqlift_extract()`. Accepted by
`sqlift_diff()`, `sqlift_detect_redundant_indexes()`, and
`sqlift_schema_hash()`.

```json
{
  "tables": {
    "users": {
      "name": "users",
      "columns": [
        {
          "name": "id",
          "type": "INTEGER",
          "notnull": false,
          "default_value": "",
          "pk": 1,
          "collation": "",
          "generated": 0,
          "generated_expr": ""
        }
      ],
      "foreign_keys": [
        {
          "constraint_name": "",
          "from_columns": ["user_id"],
          "to_table": "users",
          "to_columns": ["id"],
          "on_update": "NO ACTION",
          "on_delete": "CASCADE"
        }
      ],
      "check_constraints": [
        {
          "name": "",
          "expression": "price > 0"
        }
      ],
      "pk_constraint_name": "",
      "without_rowid": false,
      "strict": false,
      "raw_sql": "CREATE TABLE users (...)"
    }
  },
  "indexes": {
    "idx_name": {
      "name": "idx_name",
      "table_name": "users",
      "columns": ["name"],
      "unique": false,
      "where_clause": "",
      "raw_sql": "CREATE INDEX idx_name ON users(name)"
    }
  },
  "views": {
    "recent_posts": {
      "name": "recent_posts",
      "sql": "CREATE VIEW recent_posts AS ..."
    }
  },
  "triggers": {
    "on_delete": {
      "name": "on_delete",
      "table_name": "users",
      "sql": "CREATE TRIGGER on_delete ..."
    }
  }
}
```

**Column fields:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Column name |
| `type` | string | Uppercase type (e.g. `"INTEGER"`, `"TEXT"`). Empty if untyped. |
| `notnull` | bool | Whether the column has a NOT NULL constraint |
| `default_value` | string | Raw SQL expression (e.g. `"0"`, `"'hello'"`). Empty if none. |
| `pk` | int | 0 = not primary key. 1+ = position in composite PK. |
| `collation` | string | e.g. `"NOCASE"`. Empty = default (BINARY). |
| `generated` | int | 0 = normal, 2 = virtual, 3 = stored |
| `generated_expr` | string | e.g. `"first_name \|\| ' ' \|\| last_name"`. Empty if not generated. |

**Table fields:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Table name |
| `columns` | array | Ordered by column ID |
| `foreign_keys` | array | Foreign key constraints |
| `check_constraints` | array | CHECK constraints |
| `pk_constraint_name` | string | Empty if unnamed (cosmetic only) |
| `without_rowid` | bool | `WITHOUT ROWID` table option |
| `strict` | bool | `STRICT` table option |
| `raw_sql` | string | Original `CREATE TABLE` from `sqlite_master` |

Equality comparison is structural -- it compares `name`, `columns`,
`foreign_keys`, `check_constraints`, `without_rowid`, and `strict`. The
`raw_sql` and `pk_constraint_name` fields are excluded from equality (cosmetic
only) but included in `sqlift_schema_hash()`.

**Foreign key fields:**

| Field | Type | Description |
|-------|------|-------------|
| `constraint_name` | string | Empty if unnamed (cosmetic only, excluded from equality) |
| `from_columns` | array | Source columns |
| `to_table` | string | Referenced table |
| `to_columns` | array | Referenced columns |
| `on_update` | string | `"NO ACTION"`, `"CASCADE"`, `"SET NULL"`, etc. |
| `on_delete` | string | `"NO ACTION"`, `"CASCADE"`, `"SET NULL"`, etc. |

**Index fields:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Index name |
| `table_name` | string | Table the index belongs to |
| `columns` | array | Indexed columns |
| `unique` | bool | Whether the index enforces uniqueness |
| `where_clause` | string | Partial index `WHERE` clause. Empty if not partial. |
| `raw_sql` | string | Original `CREATE INDEX` from `sqlite_master` (excluded from equality) |

---

### Plan JSON format

Returned by `sqlift_diff()`. Accepted by `sqlift_apply()`.

```json
{
  "version": 1,
  "operations": [
    {
      "type": "CreateTable",
      "object_name": "users",
      "description": "Create table users",
      "sql": ["CREATE TABLE users (...)"],
      "destructive": false
    }
  ],
  "warnings": [
    {
      "type": "RedundantIndex",
      "message": "index idx_name is a prefix duplicate of idx_name_email",
      "index_name": "idx_name",
      "covered_by": "idx_name_email",
      "table_name": "users"
    }
  ]
}
```

**Operation fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Operation type (see below) |
| `object_name` | string | The table, index, view, or trigger name |
| `description` | string | Human-readable summary |
| `sql` | array | SQL statements to execute (in order) |
| `destructive` | bool | Whether this operation drops data |

**Operation types:** `CreateTable`, `DropTable`, `RebuildTable`, `AddColumn`,
`CreateIndex`, `DropIndex`, `CreateView`, `DropView`, `CreateTrigger`,
`DropTrigger`.

---

### Warning JSON format

Returned in the plan's `warnings` array and by
`sqlift_detect_redundant_indexes()`.

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Warning type (currently only `"RedundantIndex"`) |
| `message` | string | Human-readable description |
| `index_name` | string | The redundant index |
| `covered_by` | string | The covering index name, or `"PRIMARY KEY"` |
| `table_name` | string | The table both indexes belong to |
