# sqlift

Declarative SQLite schema migration for C and Go.

Maintain your schema as a single SQL file. sqlift diffs it against your database
and applies the changes -- no numbered migration files, no ordering conflicts, no
mental replay of fifty ALTER TABLEs to understand your schema.

### C

```c
#include "sqlift.h"

int err_type;
char* err_msg = NULL;
char* desired = sqlift_parse(
    "CREATE TABLE users ("
    "    id INTEGER PRIMARY KEY,"
    "    name TEXT NOT NULL,"
    "    email TEXT UNIQUE"
    ");"
    "CREATE INDEX idx_users_email ON users(email);",
    &err_type, &err_msg);

sqlift_db* db = sqlift_db_open("app.db", 0, &err_type, &err_msg);
char* current = sqlift_extract(db, &err_type, &err_msg);
char* plan = sqlift_diff(current, desired, &err_type, &err_msg);
sqlift_apply(db, plan, 0, &err_type, &err_msg);

sqlift_free(plan);
sqlift_free(current);
sqlift_free(desired);
sqlift_db_close(db);
```

### Go

```go
desired, _ := sqlift.Parse(`
    CREATE TABLE users (
        id INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        email TEXT UNIQUE
    );
    CREATE INDEX idx_users_email ON users(email);
`)

db, _ := sqlift.Open("app.db")
defer db.Close()
current, _ := sqlift.Extract(db)
plan, _ := sqlift.Diff(current, desired)
sqlift.Apply(db, plan, sqlift.ApplyOptions{})
```

## Features

- **Declarative** -- describe the end state, not the steps to get there
- **Plain SQL** -- no custom DSL; your schema files are valid SQLite DDL
- **Inspectable plans** -- review every SQL statement before execution
- **Destructive operation guard** -- dropping tables or columns requires explicit opt-in
- **Drift detection** -- detects out-of-band schema changes
- **Cross-language compatibility** -- C and Go produce identical schema hashes; databases are interchangeable

## Installation

### C

Copy `dist/sqlift.h` and `dist/sqlift.cpp` into your project. Compile
`sqlift.cpp` alongside your other sources and link against SQLite3.

Requirements: C++23 compiler (GCC 13+, Clang 16+, Apple Clang 15+), SQLite3.
The header is pure C; the implementation requires C++23.

### Go

```sh
go get github.com/marcelocantos/sqlift/go/sqlift
```

Requires CGo. The Go package wraps the C library directly (no `database/sql`
or third-party driver).

### Agent guide

If you use an agentic coding tool (Claude Code, Cursor, Copilot, etc.), include
[`dist/sqlift-agents-guide.md`](dist/sqlift-agents-guide.md) in your project context for a
condensed API reference covering both C and Go.

## Documentation

- **[Getting Started](docs/getting-started.md)** -- step-by-step tutorial with C and Go examples
- **[Guide](docs/guide.md)** -- design rationale, core concepts, and feature reference
- **[C API Reference](docs/reference.md)** -- complete C API reference
- **[Go API Reference](docs/reference-go.md)** -- complete Go API reference
- **[Agent Guide](dist/sqlift-agents-guide.md)** -- condensed reference for AI coding agents
- **[Changelog](https://github.com/marcelocantos/sqlift/releases)** -- release history with notes

## Building and testing

### C/C++

```sh
mk            # build library
mk test       # build and run tests
mk lib        # build static library only
mk clean      # remove build artifacts
```

Requires [mk](https://github.com/marcelocantos/mk) build tool and
[doctest](https://github.com/doctest/doctest) (vendored).

### Go

```sh
cd go/sqlift
go test ./...
```

## Related projects

- **[sqldeep](https://github.com/marcelocantos/sqldeep)** — JSON5-like SQL syntax transpiler. Write nested JSON queries naturally; sqldeep rewrites them into SQLite JSON functions.
- **[sqlpipe](https://github.com/marcelocantos/sqlpipe)** — Streaming SQLite replication protocol. Keeps two databases in sync over any transport.

## License

Apache 2.0 -- see [LICENSE](LICENSE).
