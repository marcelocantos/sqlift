# Stability

## Commitment

Version 1.0 represents a backwards-compatibility contract. After 1.0, any
breaking change to the public API (C++ or Go), error hierarchy, JSON
serialization format, or `_sqlift_state` schema would require forking the
project into a new product (e.g. `sqlift2`). The pre-1.0 period exists to get
these surfaces right.

Both the C++ and Go implementations share the same schema hash serialization
format for cross-language drift detection.

## Interaction surface catalogue

Snapshot as of v0.9.0.

### C++ API

#### Version macros

```cpp
#define SQLIFT_VERSION       "0.9.0"
#define SQLIFT_VERSION_MAJOR 0
#define SQLIFT_VERSION_MINOR 9
#define SQLIFT_VERSION_PATCH 0
```

**Stable.** Mechanical; updated each release.

#### Core functions

```cpp
Schema        parse(const std::string& sql);
Schema        extract(sqlite3* db);
MigrationPlan diff(const Schema& current, const Schema& desired);
void          apply(sqlite3* db, const MigrationPlan& plan, const ApplyOptions& opts = {});
int64_t       migration_version(sqlite3* db);
```

**Stable.** These five functions are the entire public workflow. Core four
signatures have not changed since v0.1.0; `migration_version` added in v0.4.0.

#### Schema types

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
    std::string pk_constraint_name;        // Cosmetic; excluded from == and hash().
    bool without_rowid = false;
    bool strict = false;
    std::string raw_sql;
    bool operator==(const Table& o) const;  // excludes raw_sql, pk_constraint_name
};
```

**Stable.** Fields have only grown additively (check_constraints, strict, and
pk_constraint_name added in v0.6.0). Equality excludes raw_sql and
pk_constraint_name by design (both are cosmetic).

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
    std::string constraint_name;             // Cosmetic; excluded from == and hash().
    std::vector<std::string> from_columns;
    std::string to_table;
    std::vector<std::string> to_columns;
    std::string on_update = "NO ACTION";
    std::string on_delete = "NO ACTION";
    bool operator==(const ForeignKey& o) const;  // excludes constraint_name
};
```

**Stable.** constraint_name added in v0.6.0. Equality excludes constraint_name
(cosmetic only).

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

#### Migration plan and operations

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

#### JSON serialization

```cpp
std::string to_string(OpType type);
OpType      op_type_from_string(const std::string& s);
std::string to_json(const MigrationPlan& plan);
MigrationPlan from_json(const std::string& json_str);
```

**Stable.** JSON format is versioned (`"version": 1`). New fields can be added
without breaking existing consumers.

#### Exception hierarchy

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

#### Utility classes

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

#### Internal utilities

`sha256()` was moved to a file-local function in sqlift.cpp in v0.6.0. It is no
longer part of the public API.

### Go API

Module: `github.com/marcelocantos/sqlift/go/sqlift`

#### Core functions

```go
func Parse(ddl string) (Schema, error)
func Extract(ctx context.Context, db *sql.DB) (Schema, error)
func Diff(current, desired Schema) (MigrationPlan, error)
func Apply(ctx context.Context, db *sql.DB, plan MigrationPlan, opts ApplyOptions) error
func MigrationVersion(ctx context.Context, db *sql.DB) (int64, error)
```

**Stable.** Direct ports of the C++ functions. `Extract` and `Apply` take
`context.Context` and `*sql.DB` (standard Go database patterns). `Apply`
returns `error` instead of throwing.

#### Schema types

```go
type GeneratedType int

const (
    GeneratedNormal  GeneratedType = 0
    GeneratedVirtual GeneratedType = 2
    GeneratedStored  GeneratedType = 3
)

type Column struct {
    Name, Type    string
    NotNull       bool
    DefaultValue  string
    PK            int
    Collation     string
    Generated     GeneratedType
    GeneratedExpr string
}

type CheckConstraint struct {
    Name       string
    Expression string
}

type ForeignKey struct {
    ConstraintName string     // Cosmetic; excluded from Equal and Hash.
    FromColumns    []string
    ToTable        string
    ToColumns      []string
    OnUpdate       string     // Default "NO ACTION".
    OnDelete       string     // Default "NO ACTION".
}

type Table struct {
    Name             string
    Columns          []Column
    ForeignKeys      []ForeignKey
    CheckConstraints []CheckConstraint
    PKConstraintName string   // Cosmetic; excluded from Equal and Hash.
    WithoutRowid     bool
    Strict           bool
    RawSQL           string   // Excluded from Equal and Hash.
}

type Index struct {
    Name, TableName string
    Columns         []string
    Unique          bool
    WhereClause     string
    RawSQL          string   // Excluded from Equal and Hash.
}

type View    struct { Name, SQL string }
type Trigger struct { Name, TableName, SQL string }

type Schema struct {
    Tables   map[string]Table
    Indexes  map[string]Index
    Views    map[string]View
    Triggers map[string]Trigger
}

func (s Schema) Equal(o Schema) bool
func (s Schema) Hash() string
```

**Stable.** Mirrors the C++ types with Go naming conventions.

#### Migration plan and operations

```go
type OpType int

const (
    CreateTable OpType = iota
    DropTable
    RebuildTable
    AddColumn
    CreateIndex
    DropIndex
    CreateView
    DropView
    CreateTrigger
    DropTrigger
)

func (t OpType) String() string

type Operation struct {
    Type        OpType
    ObjectName  string
    Description string
    SQL         []string
    Destructive bool
}

type MigrationPlan struct { /* unexported fields */ }

func (p MigrationPlan) Operations() []Operation
func (p MigrationPlan) HasDestructiveOperations() bool
func (p MigrationPlan) Empty() bool

type ApplyOptions struct {
    AllowDestructive bool
}
```

**Stable.** Mirrors the C++ types.

#### JSON serialization

```go
func ToJSON(plan MigrationPlan) ([]byte, error)
func FromJSON(data []byte) (MigrationPlan, error)
func ParseOpType(s string) (OpType, error)
```

**Stable.** Same JSON wire format (`"version": 1`) as C++.

#### Error types

```go
*ParseError
*ExtractError
*DiffError
*ApplyError
*DriftError
*DestructiveError
*BreakingChangeError
*JSONError
```

**Stable.** Each has a `Msg string` field and implements `error`. Mirrors the
C++ exception hierarchy. Use `errors.As` for type assertions.

### Cross-language compatibility

The hash serialization format is identical between C++ and Go. A database
migrated by the C++ library can be read by the Go library (and vice versa)
without triggering drift detection. This is verified by
`TestCrossLanguageHash` (Go) and `"cross-language hash"` (C++ doctest).

**Stable.**

## Gaps and prerequisites

- **Go package documentation**: Go doc comments are present but could benefit
  from runnable examples (`Example*` test functions).

## Out of scope for 1.0

- **Rename detection.** By design, sqlift treats disappearing + appearing
  columns as drop + add.
- **Data migration.** sqlift is schema-only; data transforms are the caller's
  responsibility.
- **Cross-database support.** SQLite-only by design.
- **Redundant index detection.** Warning when desired schema has prefix-
  duplicate or PK-duplicate indexes.
