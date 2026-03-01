# Go API Reference

Package `sqlift` provides declarative SQLite schema migration for Go.

```go
import "github.com/marcelocantos/sqlift/go/sqlift"
```

Requires CGo ([mattn/go-sqlite3](https://github.com/mattn/go-sqlite3)).

For the C++ API, see [C++ API Reference](reference.md). For conceptual
background, see [Guide](guide.md).

---

## Core functions

### `Parse`

```go
func Parse(ddl string) (Schema, error)
```

Parse SQL DDL statements into a `Schema`. Internally opens a temporary
`:memory:` SQLite database with a single connection, executes the DDL, and
calls `Extract` on the resulting database.

**Parameters:**
- `ddl` -- one or more DDL statements (`CREATE TABLE`, `CREATE INDEX`,
  `CREATE VIEW`, `CREATE TRIGGER`). Statements are separated by semicolons.

**Returns:** a `Schema` representing the declared objects.

**Errors:** `*ParseError` if the SQL is invalid or the database cannot be
opened.

**Example:**

```go
schema, err := sqlift.Parse(`
    CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
    CREATE INDEX idx_name ON users(name);
`)
if err != nil {
    log.Fatal(err)
}
```

---

### `Extract`

```go
func Extract(ctx context.Context, db *sql.DB) (Schema, error)
```

Extract the current schema from a live SQLite database by querying
`sqlite_master` and PRAGMAs (`table_xinfo`, `foreign_key_list`,
`index_list`, `index_info`). The `_sqlift_state` table and
`sqlite_autoindex_*` entries are excluded.

A single `*sql.Conn` is acquired for the duration of the call so that all
PRAGMA queries execute on the same connection and results are coherent.

**Parameters:**
- `ctx` -- context for cancellation and deadline propagation.
- `db` -- an open `*sql.DB` connected to a SQLite database.

**Returns:** a `Schema` representing the current database objects.

**Errors:** `*ExtractError` on failure.

**Example:**

```go
db, _ := sql.Open("sqlite3", "app.db")
schema, err := sqlift.Extract(context.Background(), db)
if err != nil {
    log.Fatal(err)
}
```

---

### `Diff`

```go
func Diff(current, desired Schema) (MigrationPlan, error)
```

Compare two schemas and produce a migration plan that migrates `current` to
`desired`. This is a pure function -- it never touches a database.

Operations are ordered to preserve referential integrity:

1. Drop triggers (removed or changed)
2. Drop views (removed or changed)
3. Drop indexes (removed, changed, or on tables being rebuilt)
4. Table operations (create, add column, rebuild, drop)
5. Create indexes
6. Create views
7. Create triggers

Views and triggers within each phase are ordered topologically by dependency.

`Diff` rejects schema changes whose success depends on existing data. The
following are detected as breaking:

- An existing nullable column becomes `NOT NULL`.
- A new foreign key constraint is added to an existing table.
- A new `CHECK` constraint is added to an existing table.
- A new `NOT NULL` column without a `DEFAULT` is added to an existing table.

**Parameters:**
- `current` -- the schema as it exists now (typically from `Extract`).
- `desired` -- the schema you want (typically from `Parse`).

**Returns:** a `MigrationPlan` containing the ordered operations needed to
transform `current` into `desired`. Returns an empty plan if the schemas are
structurally identical.

**Errors:** `*BreakingChangeError` if any breaking change is detected.

**Example:**

```go
current, _ := sqlift.Extract(ctx, db)
desired, _ := sqlift.Parse(ddl)
plan, err := sqlift.Diff(current, desired)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("%d operation(s)\n", len(plan.Operations()))
```

---

### `Apply`

```go
func Apply(ctx context.Context, db *sql.DB, plan MigrationPlan, opts ApplyOptions) error
```

Execute a migration plan against a live database. If the plan is empty,
`Apply` returns `nil` immediately without touching the database.

A single `*sql.Conn` is acquired for the entire operation to ensure
PRAGMA coherence (savepoints, `foreign_keys` state, and schema hash reads
all share the same connection).

**Behaviour:**

1. If the plan contains destructive operations and `opts.AllowDestructive`
   is `false`, return `*DestructiveError` immediately.
2. Extract the current schema and read the stored hash from `_sqlift_state`.
   If a stored hash exists and differs from the current schema hash, return
   `*DriftError` (the schema was modified outside sqlift).
3. Record the current `PRAGMA foreign_keys` state so it can be restored on
   failure.
4. Execute each operation's SQL statements. `PRAGMA foreign_key_check`
   statements are handled specially: any returned rows indicate violations
   and cause `*ApplyError`.
5. On any error: roll back the `sqlift_rebuild` savepoint (if open), release
   it, restore the FK enforcement state, and return the original error.
6. On success: extract the updated schema, store its SHA-256 hash in
   `_sqlift_state`, and increment the migration version counter.

**Parameters:**
- `ctx` -- context for cancellation and deadline propagation.
- `db` -- an open `*sql.DB` connected to the target SQLite database.
- `plan` -- the plan to execute (from `Diff`).
- `opts` -- options controlling behaviour (see `ApplyOptions`).

**Errors:**
- `*DestructiveError` -- plan has destructive operations and
  `AllowDestructive` is `false`.
- `*DriftError` -- schema was modified outside sqlift since the last `Apply`.
- `*ApplyError` -- a SQL statement failed during execution (including FK
  violations detected by `PRAGMA foreign_key_check`).

**Example:**

```go
err := sqlift.Apply(ctx, db, plan, sqlift.ApplyOptions{AllowDestructive: false})
if err != nil {
    var de *sqlift.DestructiveError
    if errors.As(err, &de) {
        log.Fatal("destructive migration requires explicit opt-in")
    }
    log.Fatal(err)
}
```

---

### `MigrationVersion`

```go
func MigrationVersion(ctx context.Context, db *sql.DB) (int64, error)
```

Return the current migration version stored in `_sqlift_state`. The counter
starts at 0 (no migrations have been applied) and increments by 1 each time
`Apply` executes a non-empty plan.

**Parameters:**
- `ctx` -- context for cancellation and deadline propagation.
- `db` -- an open `*sql.DB` connected to the target SQLite database.

**Returns:** the current migration version, or `0` if the `_sqlift_state`
table does not exist or the key is absent.

**Errors:** `*ApplyError` if the database connection cannot be acquired.

**Example:**

```go
v, err := sqlift.MigrationVersion(ctx, db)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Applied %d migration(s)\n", v)
```

---

## Types

### `Schema`

```go
type Schema struct {
    Tables   map[string]Table
    Indexes  map[string]Index
    Views    map[string]View
    Triggers map[string]Trigger
}

func (s Schema) Equal(o Schema) bool
func (s Schema) Hash() string
```

A complete representation of a SQLite database schema. Each map is keyed by
object name.

`Equal` performs a structural comparison across all four maps, delegating to
the `Equal` method of each element type.

`Hash` returns a hex-encoded SHA-256 digest of a deterministic serialisation
of the schema. Tables, indexes, views, and triggers are each sorted
lexicographically by name before hashing to ensure a stable result. The
serialisation format is identical to the C++ implementation, enabling
cross-language drift detection when both are used against the same database.
`PKConstraintName` and `ForeignKey.ConstraintName` are excluded from the
hash (they are cosmetic). `RawSQL` fields are also excluded.

---

### `Table`

```go
type Table struct {
    Name             string
    Columns          []Column          // Ordered by column ID (cid).
    ForeignKeys      []ForeignKey
    CheckConstraints []CheckConstraint
    PKConstraintName string            // Empty if unnamed. Cosmetic only.
    WithoutRowid     bool
    Strict           bool
    RawSQL           string            // Original CREATE TABLE from sqlite_master.
}

func (t Table) Equal(o Table) bool
```

`Equal` compares `Name`, `Columns`, `ForeignKeys`, `CheckConstraints`,
`WithoutRowid`, and `Strict`. The `RawSQL` and `PKConstraintName` fields are
excluded -- they are cosmetic and do not affect schema semantics.

`RawSQL` is used internally during table rebuilds to reconstruct the desired
table definition.

---

### `GeneratedType`

```go
type GeneratedType int

const (
    GeneratedNormal  GeneratedType = 0 // Not a generated column.
    GeneratedVirtual GeneratedType = 2 // GENERATED ALWAYS AS (...) VIRTUAL
    GeneratedStored  GeneratedType = 3 // GENERATED ALWAYS AS (...) STORED
)
```

Values match the `hidden` field of SQLite's `PRAGMA table_xinfo`.

---

### `Column`

```go
type Column struct {
    Name          string
    Type          string        // Uppercase (e.g. "INTEGER", "TEXT"). Empty if untyped.
    NotNull       bool
    DefaultValue  string        // Raw SQL expression (e.g. "0", "'hello'"). Empty if none.
    PK            int           // 0 = not primary key; 1+ = position in composite PK.
    Collation     string        // e.g. "NOCASE". Empty = default (BINARY).
    Generated     GeneratedType // Normal, Virtual, or Stored.
    GeneratedExpr string        // e.g. "first_name || ' ' || last_name". Empty if not generated.
}

func (c Column) Equal(o Column) bool
```

`Equal` compares all eight fields.

---

### `CheckConstraint`

```go
type CheckConstraint struct {
    Name       string // Empty if unnamed.
    Expression string // e.g. "age > 0"
}

func (c CheckConstraint) Equal(o CheckConstraint) bool
```

`Equal` compares both `Name` and `Expression`.

---

### `ForeignKey`

```go
type ForeignKey struct {
    ConstraintName string   // Empty if unnamed. Cosmetic only.
    FromColumns    []string
    ToTable        string
    ToColumns      []string
    OnUpdate       string   // e.g. "CASCADE", "SET NULL", "NO ACTION" (default).
    OnDelete       string   // e.g. "CASCADE", "SET NULL", "NO ACTION" (default).
}

func (f ForeignKey) Equal(o ForeignKey) bool
```

Supports composite foreign keys. `OnUpdate` and `OnDelete` are stored
uppercase. `ConstraintName` is excluded from `Equal` (cosmetic only); it is
also excluded from `Schema.Hash`.

---

### `Index`

```go
type Index struct {
    Name        string
    TableName   string
    Columns     []string
    Unique      bool
    WhereClause string // Partial index WHERE clause. Empty if not partial.
    RawSQL      string // Original CREATE INDEX from sqlite_master.
}

func (idx Index) Equal(o Index) bool
```

`Equal` compares `Name`, `TableName`, `Columns`, `Unique`, and
`WhereClause`. `RawSQL` is excluded (used internally when recreating indexes
during table rebuilds).

Uniqueness is determined via `PRAGMA index_list`, not by parsing `RawSQL`.

---

### `View`

```go
type View struct {
    Name string
    SQL  string // Full CREATE VIEW statement as normalised by SQLite.
}

func (v View) Equal(o View) bool
```

`Equal` compares both `Name` and `SQL`.

---

### `Trigger`

```go
type Trigger struct {
    Name      string
    TableName string
    SQL       string // Full CREATE TRIGGER statement as normalised by SQLite.
}

func (tr Trigger) Equal(o Trigger) bool
```

`Equal` compares all three fields.

---

### `MigrationPlan`

```go
type MigrationPlan struct { /* unexported */ }

func (p MigrationPlan) Operations() []Operation
func (p MigrationPlan) HasDestructiveOperations() bool
func (p MigrationPlan) Empty() bool
```

An ordered sequence of operations produced by `Diff`. The plan is immutable
once created; the internal slice is not exposed directly.

- `Operations()` -- returns the full list of operations in execution order.
- `HasDestructiveOperations()` -- reports whether any operation has
  `Destructive == true`.
- `Empty()` -- reports whether the plan contains no operations (the schemas
  were structurally identical).

---

### `Operation`

```go
type Operation struct {
    Type        OpType
    ObjectName  string
    Description string
    SQL         []string
    Destructive bool
}
```

A single migration step.

- `Type` -- the kind of operation (see `OpType`).
- `ObjectName` -- the name of the table, index, view, or trigger affected.
- `Description` -- human-readable summary (e.g. `"Add column email to users"`).
- `SQL` -- ordered list of SQL statements to execute. Simple operations
  contain a single statement; `RebuildTable` operations contain the full
  12-step sequence (disable FKs, savepoint, create, copy, drop, rename,
  recreate indexes/triggers, FK check, release, re-enable FKs).
- `Destructive` -- `true` if this operation drops data (dropped tables,
  dropped indexes, table rebuilds that remove columns).

---

### `OpType`

```go
type OpType int

const (
    CreateTable  OpType = iota // Create a new table.
    DropTable                  // Drop an existing table.
    RebuildTable               // Rebuild (recreate) a table via 12-step sequence.
    AddColumn                  // Add a column via ALTER TABLE ADD COLUMN.
    CreateIndex                // Create a new index.
    DropIndex                  // Drop an existing index.
    CreateView                 // Create a new view.
    DropView                   // Drop an existing view.
    CreateTrigger              // Create a new trigger.
    DropTrigger                // Drop an existing trigger.
)

func (t OpType) String() string
```

`String` returns the PascalCase name of the constant (e.g. `"CreateTable"`,
`"RebuildTable"`). For unrecognised values it returns `"OpType(N)"`.

---

### `ApplyOptions`

```go
type ApplyOptions struct {
    AllowDestructive bool
}
```

- `AllowDestructive` -- if `false` (the zero value), `Apply` returns
  `*DestructiveError` when the plan contains any operation with
  `Destructive == true`. Set to `true` to permit dropping tables, columns,
  and indexes.

---

## JSON serialisation

Migration plans can be serialised to JSON for inspection, storage, or
transmission and later deserialised and passed directly to `Apply`.

### `ToJSON`

```go
func ToJSON(plan MigrationPlan) ([]byte, error)
```

Serialise a `MigrationPlan` to indented JSON (version 1 format, 2-space
indentation). `OpType` values are serialised via `OpType.String`.

**Returns:** a JSON byte slice with this structure:

```json
{
  "version": 1,
  "operations": [
    {
      "type": "CreateTable",
      "object_name": "users",
      "description": "Create table users",
      "sql": ["CREATE TABLE \"users\" (id INTEGER PRIMARY KEY, name TEXT NOT NULL)"],
      "destructive": false
    }
  ]
}
```

---

### `FromJSON`

```go
func FromJSON(data []byte) (MigrationPlan, error)
```

Deserialise a `MigrationPlan` from JSON produced by `ToJSON`. All fields in
each operation are required; unrecognised top-level fields are ignored.

**Validation:**
- Root must be a JSON object with `"version": 1`.
- `"operations"` must be a JSON array.
- Each operation must have: `type` (string), `object_name` (string),
  `description` (string), `sql` (array of strings), `destructive` (boolean).
- The first SQL statement of each operation must start with the prefix
  expected for its `OpType` (e.g. `"CREATE TABLE"` for `CreateTable`,
  `"PRAGMA foreign_keys"` for `RebuildTable`).

**Errors:** `*JSONError` on any parsing or validation failure (invalid JSON,
missing fields, unknown `OpType`, unsupported version).

---

### `ParseOpType`

```go
func ParseOpType(s string) (OpType, error)
```

Parse a string produced by `OpType.String` back to an `OpType`.
Case-sensitive; must match an enumerator name exactly.

**Errors:** `*JSONError` if the string is not recognised.

---

### `OpType.String`

```go
func (t OpType) String() string
```

Convert an `OpType` to its PascalCase string representation (e.g.
`CreateTable`, `DropIndex`, `RebuildTable`). For unrecognised values,
returns `"OpType(N)"`.

---

## Error types

All error types have a single exported field `Msg string` and implement the
`error` interface via `Error() string { return e.Msg }`. There is no shared
base type -- each is an independent struct. Use `errors.As` for type
assertions.

| Type | Returned by | Meaning |
|---|---|---|
| `*ParseError` | `Parse` | DDL SQL is invalid or the in-memory DB could not be opened. |
| `*ExtractError` | `Extract` | Failed to read schema from the database. |
| `*DiffError` | `Diff` (internal) | Internal error during schema comparison. |
| `*ApplyError` | `Apply`, `MigrationVersion` | SQL execution failure, FK violation, or connection error. |
| `*DriftError` | `Apply` | Schema was modified outside sqlift since the last `Apply`. |
| `*DestructiveError` | `Apply` | Plan has destructive operations and `AllowDestructive` is `false`. |
| `*BreakingChangeError` | `Diff` | Desired schema contains a data-dependent change (nullable→NOT NULL, new FK, new CHECK, new NOT NULL column without DEFAULT). |
| `*JSONError` | `FromJSON`, `ParseOpType` | JSON parsing or validation failure. |

**Example using `errors.As`:**

```go
plan, err := sqlift.Diff(current, desired)
if err != nil {
    var bce *sqlift.BreakingChangeError
    if errors.As(err, &bce) {
        fmt.Fprintf(os.Stderr, "breaking change: %s\n", bce.Msg)
        os.Exit(1)
    }
    log.Fatal(err)
}

applyErr := sqlift.Apply(ctx, db, plan, sqlift.ApplyOptions{})
if applyErr != nil {
    var drift *sqlift.DriftError
    var destr *sqlift.DestructiveError
    switch {
    case errors.As(applyErr, &drift):
        fmt.Fprintln(os.Stderr, "schema drift detected — manual intervention required")
    case errors.As(applyErr, &destr):
        fmt.Fprintln(os.Stderr, "re-run with AllowDestructive=true to proceed")
    default:
        log.Fatal(applyErr)
    }
}
```
