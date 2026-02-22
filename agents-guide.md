# sqlift -- Agent Guide

Declarative SQLite schema migration. Two files: `sqlift.h` + `sqlift.cpp`.
Requires C++23 and SQLite3. No other dependencies.

```cpp
#include "sqlift.h"
```

Everything is in `namespace sqlift`.

## Core workflow

```cpp
// 1. Declare desired schema as plain SQL
sqlift::Schema desired = sqlift::parse(R"(
    CREATE TABLE users (
        id INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        email TEXT UNIQUE
    );
    CREATE INDEX idx_email ON users(email);
)");

// 2. Extract current schema from a live database
sqlift::Database db("app.db");
sqlift::Schema current = sqlift::extract(db);

// 3. Diff (pure function, no DB access)
sqlift::MigrationPlan plan = sqlift::diff(current, desired);

// 4. Apply
if (!plan.empty())
    sqlift::apply(db, plan);
```

## API surface

### Functions

| Function | Signature | Does |
|----------|-----------|------|
| `parse` | `Schema parse(const string& sql)` | Parse DDL into Schema. Throws `ParseError`. |
| `extract` | `Schema extract(sqlite3* db)` | Read schema from live DB via sqlite_master + PRAGMAs. |
| `diff` | `MigrationPlan diff(const Schema& current, const Schema& desired)` | Pure diff. Returns empty plan if identical. |
| `apply` | `void apply(sqlite3* db, const MigrationPlan& plan, const ApplyOptions& opts = {})` | Execute plan. Throws `DestructiveError`, `DriftError`, `ApplyError`. |
| `to_json` | `string to_json(const MigrationPlan& plan)` | Serialize plan to JSON string. |
| `from_json` | `MigrationPlan from_json(const string& json_str)` | Deserialize plan from JSON. Throws `JsonError`. |
| `to_string` | `string to_string(OpType type)` | OpType to PascalCase string (e.g. `"CreateTable"`). |
| `op_type_from_string` | `OpType op_type_from_string(const string& s)` | Parse string to OpType. Throws `JsonError`. |

### Schema types

```cpp
struct Column {
    string name;
    string type;            // Uppercase ("INTEGER", "TEXT"). Empty if untyped.
    bool notnull = false;
    string default_value;   // Raw SQL expression. Empty if none.
    int pk = 0;             // 0 = not PK. 1+ = position in composite PK.
};

struct ForeignKey {
    vector<string> from_columns;
    string to_table;
    vector<string> to_columns;
    string on_update = "NO ACTION";  // "CASCADE", "SET NULL", etc.
    string on_delete = "NO ACTION";
};

struct Table {
    string name;
    vector<Column> columns;           // Ordered by column ID.
    vector<ForeignKey> foreign_keys;
    bool without_rowid = false;
    string raw_sql;                   // Excluded from operator==.
};

struct Index {
    string name;
    string table_name;
    vector<string> columns;
    bool unique = false;
    string where_clause;   // Partial index. Empty if not partial.
    string raw_sql;        // Excluded from operator==.
};

struct View    { string name; string sql; };
struct Trigger { string name; string table_name; string sql; };

struct Schema {
    map<string, Table>   tables;
    map<string, Index>   indexes;
    map<string, View>    views;
    map<string, Trigger> triggers;
    string hash() const;  // SHA-256 hex string.
};
```

### Migration types

```cpp
enum class OpType {
    CreateTable, DropTable, RebuildTable, AddColumn,
    CreateIndex, DropIndex,
    CreateView, DropView,
    CreateTrigger, DropTrigger,
};

struct Operation {
    OpType type;
    string object_name;
    string description;        // Human-readable summary.
    vector<string> sql;        // Exact SQL statements to execute.
    bool destructive = false;  // True if operation drops data.
};

class MigrationPlan {
    const vector<Operation>& operations() const;
    bool has_destructive_operations() const;
    bool empty() const;
};

struct ApplyOptions {
    bool allow_destructive = false;  // Must be true to drop tables/columns.
};
```

### RAII wrappers

```cpp
// Database: opens on construction, closes on destruction. Move-only.
sqlift::Database db("app.db");
db.exec("INSERT INTO t (x) VALUES (1)");
sqlite3* raw = db.get();  // or implicit conversion

// Statement: prepares on construction, finalizes on destruction. Move-only.
sqlift::Statement stmt(db, "SELECT x FROM t WHERE id = ?");
stmt.bind_int(1, 42);
if (stmt.step()) {              // true = row available, false = done
    int x = stmt.column_int(0);
    string s = stmt.column_text(0);
}
```

### Exceptions

All inherit from `sqlift::Error` (inherits `std::runtime_error`):

| Exception | When |
|-----------|------|
| `ParseError` | Invalid SQL in `parse()` |
| `ExtractError` | Schema extraction fails |
| `DiffError` | Internal diff error |
| `ApplyError` | SQL fails during `apply()` (e.g. FK violation) |
| `DestructiveError` | Plan has destructive ops, `allow_destructive` is false |
| `DriftError` | Schema modified outside sqlift since last `apply()` |
| `JsonError` | Invalid JSON or missing fields in `from_json()` / `op_type_from_string()` |

## Key behaviours

- **AddColumn fast path**: When the only change is appending nullable columns (or NOT NULL with DEFAULT) at the end, uses `ALTER TABLE ADD COLUMN`.
- **12-step table rebuild**: Any other table change uses SQLite's recommended rebuild (disable FKs, savepoint, create new, copy data, drop old, rename, recreate indexes/triggers/views, FK check, release, re-enable FKs).
- **Destructive guard**: `apply()` throws `DestructiveError` unless `{.allow_destructive = true}`.
- **Drift detection**: Stores SHA-256 hash in `_sqlift_state` table after each apply. Throws `DriftError` if schema changed outside sqlift.
- **No rename detection**: A removed + added column is always a drop + add.
- **Operation order**: Drop triggers/views/indexes, then table ops, then create indexes/views/triggers.
- **`raw_sql` excluded from equality**: SQLite doesn't update `sqlite_master.sql` after `ALTER TABLE ADD COLUMN`, so Table/Index equality is structural only.

## Common patterns

```cpp
// Inspect plan before applying
for (const auto& op : plan.operations()) {
    std::cout << op.description << "\n";
    if (op.destructive) std::cout << "  [DESTRUCTIVE]\n";
}

// Allow destructive operations (drops)
sqlift::apply(db, plan, {.allow_destructive = true});

// Handle drift
try {
    sqlift::apply(db, plan);
} catch (const sqlift::DriftError& e) {
    // Schema was modified outside sqlift
}

// Serialize plan to JSON (for transmission, storage, or review)
std::string json = sqlift::to_json(plan);

// Deserialize and apply on another machine
auto restored = sqlift::from_json(json);
sqlift::apply(db, restored);
```
