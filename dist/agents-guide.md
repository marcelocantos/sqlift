# sqlift -- Agent Guide

Declarative SQLite schema migration. Available in C++ and Go.

## C++

Two files: `dist/sqlift.h` + `dist/sqlift.cpp`. Requires C++23 and SQLite3.

```cpp
#include "sqlift.h"
```

Everything is in `namespace sqlift`.

### Core workflow

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

### API surface

#### Functions

| Function | Signature | Does |
|----------|-----------|------|
| `parse` | `Schema parse(const string& sql)` | Parse DDL into Schema. Throws `ParseError`. |
| `extract` | `Schema extract(sqlite3* db)` | Read schema from live DB via sqlite_master + PRAGMAs. |
| `diff` | `MigrationPlan diff(const Schema& current, const Schema& desired)` | Pure diff. Returns empty plan if identical. Populates warnings for redundant indexes. |
| `detect_redundant_indexes` | `vector<Warning> detect_redundant_indexes(const Schema& schema)` | Detect prefix-duplicate and PK-duplicate indexes. |
| `apply` | `void apply(sqlite3* db, const MigrationPlan& plan, const ApplyOptions& opts = {})` | Execute plan. Throws `DestructiveError`, `DriftError`, `ApplyError`. |
| `to_json` | `string to_json(const MigrationPlan& plan)` | Serialize plan to JSON string. |
| `from_json` | `MigrationPlan from_json(const string& json_str)` | Deserialize plan from JSON. Throws `JsonError`. |
| `to_string` | `string to_string(OpType type)` | OpType to PascalCase string (e.g. `"CreateTable"`). |
| `op_type_from_string` | `OpType op_type_from_string(const string& s)` | Parse string to OpType. Throws `JsonError`. |
| `migration_version` | `int64_t migration_version(sqlite3* db)` | Migration version counter (0 if no migrations have run, increments by 1 on each non-empty apply). |

#### Schema types

```cpp
enum class GeneratedType { Normal = 0, Virtual = 2, Stored = 3 };

struct Column {
    string name;
    string type;            // Uppercase ("INTEGER", "TEXT"). Empty if untyped.
    bool notnull = false;
    string default_value;   // Raw SQL expression. Empty if none.
    int pk = 0;             // 0 = not PK. 1+ = position in composite PK.
    string collation;       // e.g. "NOCASE". Empty = default (BINARY).
    GeneratedType generated = GeneratedType::Normal;
    string generated_expr;  // e.g. "first_name || ' ' || last_name". Empty if not generated.
};

struct CheckConstraint {
    string name;        // Empty if unnamed.
    string expression;  // e.g. "age > 0"
};

struct ForeignKey {
    string constraint_name;  // empty if unnamed
    vector<string> from_columns;
    string to_table;
    vector<string> to_columns;
    string on_update = "NO ACTION";  // "CASCADE", "SET NULL", etc.
    string on_delete = "NO ACTION";
};

struct Table {
    string name;
    vector<Column> columns;                    // Ordered by column ID.
    vector<ForeignKey> foreign_keys;
    vector<CheckConstraint> check_constraints;
    string pk_constraint_name;  // empty if unnamed
    bool without_rowid = false;
    bool strict = false;
    string raw_sql;                            // Excluded from operator==.
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

#### Migration types

```cpp
enum class WarningType { RedundantIndex };

struct Warning {
    WarningType type;
    string message;        // Human-readable description.
    string index_name;     // The redundant index.
    string covered_by;     // Covering index name or "PRIMARY KEY".
    string table_name;
};

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
    const vector<Warning>& warnings() const;
    bool has_destructive_operations() const;
    bool empty() const;
};

struct ApplyOptions {
    bool allow_destructive = false;  // Must be true to drop tables/columns.
};
```

#### RAII wrappers

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

#### Exceptions

All inherit from `sqlift::Error` (inherits `std::runtime_error`):

| Exception | When |
|-----------|------|
| `ParseError` | Invalid SQL in `parse()` |
| `ExtractError` | Schema extraction fails |
| `DiffError` | Internal diff error |
| `BreakingChangeError` | Schema change is backwards-incompatible (see below) |
| `ApplyError` | SQL fails during `apply()` (e.g. FK violation) |
| `DestructiveError` | Plan has destructive ops, `allow_destructive` is false |
| `DriftError` | Schema modified outside sqlift since last `apply()` |
| `JsonError` | Invalid JSON or missing fields in `from_json()` / `op_type_from_string()` |

### Key behaviours

- **AddColumn fast path**: When the only change is appending nullable columns (or NOT NULL with DEFAULT) at the end, uses `ALTER TABLE ADD COLUMN`.
- **12-step table rebuild**: Any other table change uses SQLite's recommended rebuild (disable FKs, savepoint, create new, copy data, drop old, rename, recreate indexes/triggers/views, FK check, release, re-enable FKs).
- **Destructive guard**: `apply()` throws `DestructiveError` unless `{.allow_destructive = true}`.
- **Drift detection**: Stores SHA-256 hash in `_sqlift_state` table after each apply. Throws `DriftError` if schema changed outside sqlift.
- **Breaking change detection**: `diff()` throws `BreakingChangeError` for schema changes whose success depends on existing data — i.e., changes that might work on one database but fail on another. Detected cases: (1) existing nullable column becomes NOT NULL, (2) new FK constraint added to existing table, (3) new CHECK constraint added to existing table, (4) new NOT NULL column without DEFAULT. The engineer must find a safe alternative (e.g., create a new table and migrate data at the application level).
- **No rename detection**: A removed + added column is always a drop + add.
- **Operation order**: Drop triggers/views/indexes, then table ops, then create indexes/views/triggers.
- **`raw_sql` excluded from equality**: SQLite doesn't update `sqlite_master.sql` after `ALTER TABLE ADD COLUMN`, so Table/Index equality is structural only.

### Common patterns

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

## Go

Module: `github.com/marcelocantos/sqlift/go/sqlift`. Requires CGo. Wraps
the C++ implementation via an `extern "C"` interface (no `database/sql` or
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

The Go and C++ implementations produce identical schema hashes. A database
migrated by C++ sqlift can be read and further migrated by the Go library
(and vice versa) without triggering drift detection.

## Agentic migration guidance

When `diff()` throws `BreakingChangeError`, agentic coding tools can suggest safe alternatives. Common patterns:

- **Nullable → NOT NULL**: Create a new table with the desired schema, write application code to copy data (coalescing NULLs), then drop the old table in a later release.
- **Adding FK constraints**: Create a new table with the FK, migrate data at the application level (filtering orphans), retire the old table.
- **New NOT NULL column without DEFAULT**: Add the column as nullable first, populate it via application code, then create a new table with the NOT NULL constraint in a later release.
