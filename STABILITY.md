# Stability

## Commitment

Version 1.0 represents a backwards-compatibility contract. After 1.0, any
breaking change to the public API (C or Go), error codes, JSON interchange
format, or `_sqlift_state` schema would require forking the project into a new
product (e.g. `sqlift2`). The pre-1.0 period exists to get these surfaces right.

Both the C and Go implementations share the same schema hash serialization
format for cross-language drift detection.

## Interaction surface catalogue

Snapshot as of v0.12.0.

### C API

Two files: `dist/sqlift.h` (C-only header) + `dist/sqlift.cpp` (implementation).
All functions use `extern "C"` linkage. Data interchange is JSON strings.

#### Version macros

```c
#define SQLIFT_VERSION       "0.12.0"
#define SQLIFT_VERSION_MAJOR 0
#define SQLIFT_VERSION_MINOR 12
#define SQLIFT_VERSION_PATCH 0
```

**Stable.** Mechanical; updated each release.

#### Error codes

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

**Stable.** New error codes may be added (additive). Existing codes and their
numeric values will not change.

#### Database handle

```c
typedef struct sqlift_db sqlift_db;

sqlift_db* sqlift_db_open(const char* path, int flags,
                          int* err_type, char** err_msg);
void       sqlift_db_close(sqlift_db* db);
int        sqlift_db_exec(sqlift_db* db, const char* sql, char** err_msg);
int        sqlift_db_query_int64(sqlift_db* db, const char* sql,
                                 int64_t* result, char** err_msg);
char*      sqlift_db_query_text(sqlift_db* db, const char* sql,
                                char** err_msg);
```

**Stable.** Opaque handle wrapping SQLite. Query functions are convenience
helpers for tests and simple consumers.

#### Core functions

```c
char*   sqlift_parse(const char* ddl, int* err_type, char** err_msg);
char*   sqlift_extract(sqlift_db* db, int* err_type, char** err_msg);
char*   sqlift_diff(const char* current_json, const char* desired_json,
                    int* err_type, char** err_msg);
int     sqlift_apply(sqlift_db* db, const char* plan_json,
                     int allow_destructive, int* err_type, char** err_msg);
int64_t sqlift_migration_version(sqlift_db* db, int* err_type, char** err_msg);
```

**Stable.** The core workflow (parse, extract, diff, apply) plus
migration_version.

#### Utility functions

```c
char* sqlift_detect_redundant_indexes(const char* schema_json,
                                      int* err_type, char** err_msg);
char* sqlift_schema_hash(const char* schema_json,
                         int* err_type, char** err_msg);
```

**Stable.** `detect_redundant_indexes` is also called automatically by
`sqlift_diff()`, with results in the plan's `warnings` array.

#### Memory management

```c
void sqlift_free(void* ptr);
```

**Stable.** All heap-allocated return values (including error messages) must be
freed with this function.

#### JSON interchange formats

**Schema JSON** (returned by `sqlift_parse`, `sqlift_extract`):

Top-level keys: `tables`, `indexes`, `views`, `triggers` (maps keyed by name).

Table fields: `name`, `columns`, `foreign_keys`, `check_constraints`,
`pk_constraint_name`, `without_rowid`, `strict`, `raw_sql`.

Column fields: `name`, `type`, `notnull`, `default_value`, `pk`, `collation`,
`generated` (0/2/3), `generated_expr`.

ForeignKey fields: `constraint_name`, `from_columns`, `to_table`, `to_columns`,
`on_update`, `on_delete`.

CheckConstraint fields: `name`, `expression`.

Index fields: `name`, `table_name`, `columns`, `unique`, `where_clause`,
`raw_sql`.

View fields: `name`, `sql`.

Trigger fields: `name`, `table_name`, `sql`.

**Plan JSON** (returned by `sqlift_diff`, accepted by `sqlift_apply`):

Top-level: `version` (int, currently 1), `operations` (array), `warnings`
(array).

Operation fields: `type`, `object_name`, `description`, `sql`, `destructive`.

Operation types: `CreateTable`, `DropTable`, `RebuildTable`, `AddColumn`,
`CreateIndex`, `DropIndex`, `CreateView`, `DropView`, `CreateTrigger`,
`DropTrigger`.

**Warning JSON** (in plan `warnings` and from `sqlift_detect_redundant_indexes`):

Fields: `type`, `message`, `index_name`, `covered_by`, `table_name`.

Warning types: `RedundantIndex`.

**Stable.** JSON format is versioned. New fields and warning/operation types can
be added without breaking existing consumers.

### Go API

Module: `github.com/marcelocantos/sqlift/go/sqlift`

Requires CGo. Wraps the C implementation via `extern "C"`.

#### Core functions

```go
func Open(path string) (*Database, error)
func Parse(ddl string) (Schema, error)
func Extract(db *Database) (Schema, error)
func Diff(current, desired Schema) (MigrationPlan, error)
func Apply(db *Database, plan MigrationPlan, opts ApplyOptions) error
func MigrationVersion(db *Database) (int64, error)
func DetectRedundantIndexes(schema Schema) []Warning
```

**Stable.** The Go API wraps the C functions directly. `*Database` is an opaque
handle wrapping `sqlift_db*` (no `database/sql` or third-party driver).

#### Database type

```go
type Database struct { /* opaque C handle */ }

func Open(path string) (*Database, error)
func (d *Database) Close()
func (d *Database) Exec(sql string) error
func (d *Database) QueryInt64(sql string) (int64, error)
func (d *Database) QueryText(sql string) (string, error)
```

**Stable.** Wraps the C `sqlift_db` handle with idiomatic Go methods.

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

**Stable.** Mirrors the C JSON schema types with Go naming conventions.

#### Warning types

```go
type WarningType int

const (
    RedundantIndex WarningType = iota
)

type Warning struct {
    Type      WarningType
    Message   string
    IndexName string
    CoveredBy string
    TableName string
}
```

**Stable.** WarningType may gain new variants in future (additive).

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
func (p MigrationPlan) Warnings() []Warning
func (p MigrationPlan) HasDestructiveOperations() bool
func (p MigrationPlan) Empty() bool

type ApplyOptions struct {
    AllowDestructive bool
}
```

**Stable.** OpType may gain new variants in future (additive).

#### JSON serialization

```go
func ToJSON(plan MigrationPlan) ([]byte, error)
func FromJSON(data []byte) (MigrationPlan, error)
func ParseOpType(s string) (OpType, error)
```

**Stable.** Same JSON wire format (`"version": 1`) as the C API.

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

**Stable.** Each has a `Msg string` field and implements `error`. Use
`errors.As` for type assertions. New error types may be added (additive).

### Cross-language compatibility

The hash serialization format is identical between C and Go. A database
migrated by the C library can be read by the Go library (and vice versa)
without triggering drift detection. This is verified by
`TestCrossLanguageHash` (Go) and `"cross-language hash"` (C++ doctest).

**Stable.**

## Gaps and prerequisites

No known gaps remain. All public API surfaces have documentation, tests, and
runnable Go examples.

**Settling threshold**: ~70 surface items → N=4. This release (v0.12.0)
introduces a breaking change (C++ public API replaced with C-only API), so
the counter resets. Four consecutive minor releases with no breaking changes
are needed before 1.0 eligibility.

## Out of scope for 1.0

- **Rename detection.** By design, sqlift treats disappearing + appearing
  columns as drop + add.
- **Data migration.** sqlift is schema-only; data transforms are the caller's
  responsibility.
- **Cross-database support.** SQLite-only by design.
