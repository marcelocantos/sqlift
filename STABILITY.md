# Stability

## Commitment

Version 1.0 represents a backwards-compatibility contract. After 1.0, any
breaking change to the public API, exception hierarchy, JSON serialization
format, or `_sqlift_state` schema would require forking the project into a new
product (e.g. `sqlift2`). The pre-1.0 period exists to get these surfaces right.

## Interaction surface catalogue

### Version macros

```cpp
#define SQLIFT_VERSION       "0.6.0"
#define SQLIFT_VERSION_MAJOR 0
#define SQLIFT_VERSION_MINOR 6
#define SQLIFT_VERSION_PATCH 0
```

**Stable.** Mechanical; updated each release.

### Core functions

```cpp
Schema        parse(const std::string& sql);
Schema        extract(sqlite3* db);
MigrationPlan diff(const Schema& current, const Schema& desired);
void          apply(sqlite3* db, const MigrationPlan& plan, const ApplyOptions& opts = {});
```

**Stable.** These four functions are the entire public workflow. Signatures have
not changed since v0.1.0.

### Schema types

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

**Stable.**

```cpp
struct Table {
    std::string name;
    std::vector<Column> columns;
    std::vector<ForeignKey> foreign_keys;
    std::vector<CheckConstraint> check_constraints;
    bool without_rowid = false;
    bool strict = false;
    std::string raw_sql;
    bool operator==(const Table& o) const;  // excludes raw_sql
};
```

**Stable.** Fields have only grown additively (check_constraints and strict
added in v0.6.0). Equality excludes raw_sql by design.

```cpp
enum class GeneratedType { Normal = 0, Virtual = 2, Stored = 3 };

struct Column {
    std::string name;
    std::string type;
    bool notnull = false;
    std::string default_value;
    int pk = 0;
    std::string collation;
    GeneratedType generated = GeneratedType::Normal;
    std::string generated_expr;
    bool operator==(const Column&) const = default;
};
```

**Stable.** The `generated` field was changed from `int` to `GeneratedType`
enum in v0.6.0. Enum values match SQLite's `table_xinfo` PRAGMA.

```cpp
struct CheckConstraint {
    std::string name;
    std::string expression;
    bool operator==(const CheckConstraint&) const = default;
};
```

**Stable.** Simple value type, unlikely to change.

```cpp
struct ForeignKey {
    std::vector<std::string> from_columns;
    std::string to_table;
    std::vector<std::string> to_columns;
    std::string on_update = "NO ACTION";
    std::string on_delete = "NO ACTION";
    bool operator==(const ForeignKey&) const = default;
};
```

**Stable.**

```cpp
struct Index {
    std::string name;
    std::string table_name;
    std::vector<std::string> columns;
    bool unique = false;
    std::string where_clause;
    std::string raw_sql;
    bool operator==(const Index& o) const;  // excludes raw_sql
};
```

**Stable.**

```cpp
struct View {
    std::string name;
    std::string sql;
    bool operator==(const View&) const = default;
};

struct Trigger {
    std::string name;
    std::string table_name;
    std::string sql;
    bool operator==(const Trigger&) const = default;
};
```

**Stable.**

### Migration plan and operations

```cpp
enum class OpType {
    CreateTable, DropTable, RebuildTable, AddColumn,
    CreateIndex, DropIndex,
    CreateView, DropView,
    CreateTrigger, DropTrigger,
};

struct Operation {
    OpType type;
    std::string object_name;
    std::string description;
    std::vector<std::string> sql;
    bool destructive = false;
};

class MigrationPlan {
public:
    const std::vector<Operation>& operations() const;
    bool has_destructive_operations() const;
    bool empty() const;
};
```

**Stable.** OpType may gain new variants in future (additive, not breaking).

```cpp
struct ApplyOptions {
    bool allow_destructive = false;
};
```

**Stable.** New fields can be added with defaults (additive).

### JSON serialization

```cpp
std::string to_string(OpType type);
OpType      op_type_from_string(const std::string& s);
std::string to_json(const MigrationPlan& plan);
MigrationPlan from_json(const std::string& json_str);
```

**Stable.** JSON format is versioned (`"version": 1`). New fields can be added
without breaking existing consumers.

### Exception hierarchy

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

**Stable.** New exception types can be added under `Error` (additive). Existing
types will not be removed or reparented.

### Utility classes

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

**Stable.** These are thin RAII wrappers. The only change since v0.1.0 was
widening int to int64_t in v0.4.0.

### Internal utilities

`sha256()` was moved to a file-local function in sqlift.cpp in v0.6.0. It is no
longer part of the public API.

## Gaps and prerequisites

None. All documentation is current, API design decisions are resolved, and
third-party attribution is in place (THIRD_PARTY_LICENSES.md).

## Out of scope for 1.0

- **Rename detection.** By design, sqlift treats disappearing + appearing
  columns as drop + add.
- **Data migration.** sqlift is schema-only; data transforms are the caller's
  responsibility.
- **Cross-database support.** SQLite-only by design.
- **Named table-level constraint preservation.** `CONSTRAINT pk_foo PRIMARY
  KEY(a, b)` names are lost during extraction.
- **Redundant index detection.** Warning when desired schema has prefix-
  duplicate or PK-duplicate indexes.
- **Schema version counter in `_sqlift_state`.** Monotonic counter alongside
  the hash.
- **`mk install` target.**
- **`mk sanitize` target** (ASan/UBSan build variant).
