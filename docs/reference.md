# API Reference

All types and functions live in the `sqlift` namespace. Include `"sqlift.h"` to
access everything.

## Core functions

### `parse`

```cpp
Schema parse(const std::string& sql);
```

Parse SQL DDL statements into a `Schema`. Internally creates a `:memory:`
SQLite database, executes the SQL, and extracts the resulting schema.

**Parameters:**
- `sql` -- one or more DDL statements (`CREATE TABLE`, `CREATE INDEX`,
  `CREATE VIEW`, `CREATE TRIGGER`). Statements are separated by semicolons.

**Returns:** a `Schema` representing the declared objects.

**Throws:** `ParseError` if the SQL is invalid.

**Example:**
```cpp
auto schema = sqlift::parse(
    "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);"
    "CREATE INDEX idx_name ON users(name);"
);
```

---

### `extract`

```cpp
Schema extract(sqlite3* db);
```

Extract the current schema from a live SQLite database by querying
`sqlite_master` and PRAGMAs (`table_xinfo`, `foreign_key_list`, `index_info`).

The `_sqlift_state` table and `sqlite_autoindex_*` entries are excluded.

**Parameters:**
- `db` -- an open SQLite database handle.

**Returns:** a `Schema` representing the current database objects.

**Throws:** `ExtractError` on failure.

---

### `diff`

```cpp
MigrationPlan diff(const Schema& current, const Schema& desired);
```

Compare two schemas and produce a migration plan. This is a pure function -- it
does not access any database.

**Parameters:**
- `current` -- the schema as it exists now (typically from `extract()`).
- `desired` -- the schema you want (typically from `parse()`).

**Returns:** a `MigrationPlan` containing the operations needed to transform
`current` into `desired`. Returns an empty plan if the schemas are identical.

---

### `apply`

```cpp
void apply(sqlite3* db, const MigrationPlan& plan,
           const ApplyOptions& opts = {});
```

Execute a migration plan against a live database.

**Parameters:**
- `db` -- an open SQLite database handle.
- `plan` -- the plan to execute.
- `opts` -- options controlling behaviour (see `ApplyOptions`).

**Throws:**
- `DestructiveError` if the plan contains destructive operations and
  `opts.allow_destructive` is `false`.
- `DriftError` if the database schema has been modified since the last
  `apply()` (detected via stored hash in `_sqlift_state`).
- `ApplyError` if any SQL statement fails during execution (e.g. a foreign key
  check violation during a table rebuild).

After successful execution, updates the schema hash and increments the
migration version counter in `_sqlift_state`.

---

### `migration_version`

```cpp
int64_t migration_version(sqlite3* db);
```

Return the migration version counter. Starts at 0 (no migrations have run) and
increments by 1 each time `apply()` executes a non-empty plan.

**Parameters:**
- `db` -- an open SQLite database handle.

**Returns:** the current migration version (0 if no migrations have been applied).

---

## Types

### `Schema`

```cpp
struct Schema {
    std::map<std::string, Table>   tables;
    std::map<std::string, Index>   indexes;
    std::map<std::string, View>    views;
    std::map<std::string, Trigger> triggers;

    bool operator==(const Schema&) const = default;
    std::string hash() const;
};
```

A complete representation of a SQLite database schema. Maps are keyed by object
name and sorted lexicographically for deterministic iteration.

`hash()` returns a hex-encoded SHA-256 hash of a deterministic serialization of
the schema. Used internally for drift detection.

---

### `Table`

```cpp
struct Table {
    std::string name;
    std::vector<Column> columns;                    // Ordered by column ID.
    std::vector<ForeignKey> foreign_keys;
    std::vector<CheckConstraint> check_constraints;
    std::string pk_constraint_name;                 // Empty if unnamed.
    bool without_rowid = false;
    bool strict = false;
    std::string raw_sql;                            // Original CREATE TABLE from sqlite_master.
};
```

Equality comparison is structural -- it compares `name`, `columns`,
`foreign_keys`, `check_constraints`, `without_rowid`, and `strict`. The
`raw_sql` and `pk_constraint_name` fields are excluded from equality (cosmetic
only) but included in `Schema::hash()`.

`raw_sql` is used during table rebuilds to reconstruct the desired table
structure.

---

### `GeneratedType`

```cpp
enum class GeneratedType {
    Normal  = 0,   // Not a generated column.
    Virtual = 2,   // GENERATED ALWAYS AS (...) VIRTUAL
    Stored  = 3,   // GENERATED ALWAYS AS (...) STORED
};
```

Values match SQLite's `table_xinfo` hidden field.

---

### `Column`

```cpp
struct Column {
    std::string name;
    std::string type;            // Uppercase (e.g. "INTEGER", "TEXT"). Empty if untyped.
    bool notnull = false;
    std::string default_value;   // Raw SQL expression (e.g. "0", "'hello'"). Empty if none.
    int pk = 0;                  // 0 = not primary key. 1+ = position in composite PK.
    std::string collation;       // e.g. "NOCASE". Empty = default (BINARY).
    GeneratedType generated = GeneratedType::Normal;
    std::string generated_expr;  // e.g. "first_name || ' ' || last_name". Empty if not generated.
};
```

---

### `CheckConstraint`

```cpp
struct CheckConstraint {
    std::string name;        // Empty if unnamed.
    std::string expression;  // e.g. "age > 0"
};
```

---

### `ForeignKey`

```cpp
struct ForeignKey {
    std::string constraint_name;                    // Empty if unnamed.
    std::vector<std::string> from_columns;
    std::string to_table;
    std::vector<std::string> to_columns;
    std::string on_update = "NO ACTION";
    std::string on_delete = "NO ACTION";
};
```

Supports composite foreign keys. `on_update` and `on_delete` are stored
uppercase (e.g. `"CASCADE"`, `"SET NULL"`, `"NO ACTION"`). `constraint_name`
is excluded from equality (cosmetic only) but included in `Schema::hash()`.

---

### `Index`

```cpp
struct Index {
    std::string name;
    std::string table_name;
    std::vector<std::string> columns;
    bool unique = false;
    std::string where_clause;  // Partial index WHERE clause. Empty if not partial.
    std::string raw_sql;       // Original CREATE INDEX from sqlite_master.
};
```

Equality comparison is structural -- excludes `raw_sql`.

---

### `View`

```cpp
struct View {
    std::string name;
    std::string sql;  // Full CREATE VIEW statement as normalized by SQLite.
};
```

---

### `Trigger`

```cpp
struct Trigger {
    std::string name;
    std::string table_name;
    std::string sql;  // Full CREATE TRIGGER statement as normalized by SQLite.
};
```

---

### `MigrationPlan`

```cpp
class MigrationPlan {
public:
    const std::vector<Operation>& operations() const;
    bool has_destructive_operations() const;
    bool empty() const;
};
```

An ordered sequence of operations produced by `diff()`. The plan is immutable
once created.

- `operations()` -- returns the full list of operations in execution order.
- `has_destructive_operations()` -- returns `true` if any operation has
  `destructive == true`.
- `empty()` -- returns `true` if there are no operations (schemas are
  identical).

---

### `Operation`

```cpp
struct Operation {
    OpType type;
    std::string object_name;
    std::string description;
    std::vector<std::string> sql;
    bool destructive = false;
};
```

A single migration step.

- `type` -- the kind of operation (see `OpType`).
- `object_name` -- the table, index, view, or trigger name.
- `description` -- human-readable summary (e.g. `"Add column email to users"`).
- `sql` -- ordered list of SQL statements to execute. For simple operations
  this is a single statement; for `RebuildTable` it contains the full 12-step
  sequence.
- `destructive` -- `true` if this operation drops data.

---

### `OpType`

```cpp
enum class OpType {
    CreateTable,
    DropTable,
    RebuildTable,
    AddColumn,
    CreateIndex,
    DropIndex,
    CreateView,
    DropView,
    CreateTrigger,
    DropTrigger,
};
```

---

### `ApplyOptions`

```cpp
struct ApplyOptions {
    bool allow_destructive = false;
};
```

- `allow_destructive` -- if `false` (default), `apply()` throws
  `DestructiveError` when the plan contains destructive operations. Set to
  `true` to permit dropping tables, columns, and indexes.

---

## JSON serialization

### `to_string(OpType)`

```cpp
std::string to_string(OpType type);
```

Convert an `OpType` value to its string representation. The string matches the
enumerator name in PascalCase (e.g. `OpType::CreateTable` -> `"CreateTable"`).

**Throws:** `JsonError` if the value is not a recognized `OpType`.

---

### `op_type_from_string`

```cpp
OpType op_type_from_string(const std::string& s);
```

Parse a string into an `OpType`. Case-sensitive; must match an enumerator name
exactly.

**Throws:** `JsonError` if the string is not recognized.

---

### `to_json`

```cpp
std::string to_json(const MigrationPlan& plan);
```

Serialize a `MigrationPlan` to a JSON string. Produces a pretty-printed JSON
object with 2-space indentation.

**Returns:** a JSON string with this structure:

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
  ]
}
```

---

### `from_json`

```cpp
MigrationPlan from_json(const std::string& json_str);
```

Deserialize a `MigrationPlan` from a JSON string. All fields in each operation
are required. Unknown fields are ignored.

**Throws:** `JsonError` on any parsing or validation failure (invalid JSON,
missing fields, unknown `OpType`, unsupported version).

The deserialized plan can be passed directly to `apply()`.

---

## Exceptions

All exceptions inherit from `sqlift::Error`, which inherits from
`std::runtime_error`.

```
std::runtime_error
  sqlift::Error
    sqlift::ParseError
    sqlift::ExtractError
    sqlift::DiffError
    sqlift::ApplyError
    sqlift::DriftError
    sqlift::DestructiveError
    sqlift::BreakingChangeError
    sqlift::JsonError
```

- `BreakingChangeError` -- thrown by `diff()` when the desired schema contains
  changes whose success depends on existing data. Detected cases: existing
  nullable column becomes NOT NULL, new FK constraint on existing table, new
  CHECK constraint on existing table, new NOT NULL column without DEFAULT.

---

## Utility classes

### `Database`

```cpp
class Database {
public:
    explicit Database(const std::string& path,
                      int flags = SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE);
    ~Database();

    Database(Database&&) noexcept;
    Database& operator=(Database&&) noexcept;

    sqlite3* get() const;
    operator sqlite3*() const;

    void exec(const std::string& sql);
};
```

RAII wrapper for `sqlite3*`. Opens the database on construction, closes on
destruction. Move-only.

- `get()` / `operator sqlite3*()` -- access the underlying handle. The
  implicit conversion allows passing a `Database` directly to any function
  expecting `sqlite3*`.
- `exec(sql)` -- execute SQL with no result rows. Throws `Error` on failure.

---

### `Statement`

```cpp
class Statement {
public:
    Statement(sqlite3* db, const std::string& sql);
    ~Statement();

    Statement(Statement&&) noexcept;
    Statement& operator=(Statement&&) noexcept;

    bool step();

    int64_t column_int(int col) const;
    std::string column_text(int col) const;

    void bind_text(int param, const std::string& value);
    void bind_int(int param, int64_t value);

    sqlite3_stmt* get() const;
};
```

RAII wrapper for `sqlite3_stmt*`. Prepares on construction, finalizes on
destruction. Move-only.

- `step()` -- advance the statement. Returns `true` if a row is available
  (`SQLITE_ROW`), `false` if done (`SQLITE_DONE`). Throws `Error` on failure.
- `column_int(col)` / `column_text(col)` -- read column values (0-indexed).
- `bind_text(param, value)` / `bind_int(param, value)` -- bind parameters
  (1-indexed).
