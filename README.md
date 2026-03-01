# sqlift

Declarative SQLite schema migration for C++ and Go.

Declare your desired schema as plain SQL. sqlift computes the diff against a live
database and applies the changes.

### C++

```cpp
#include "sqlift.h"

sqlift::Schema desired = sqlift::parse(R"(
    CREATE TABLE users (
        id INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        email TEXT UNIQUE
    );
    CREATE INDEX idx_users_email ON users(email);
)");

sqlift::Database db("app.db");
sqlift::Schema current = sqlift::extract(db);
sqlift::MigrationPlan plan = sqlift::diff(current, desired);
sqlift::apply(db, plan);
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

db, _ := sql.Open("sqlite3", "app.db")
current, _ := sqlift.Extract(ctx, db)
plan, _ := sqlift.Diff(current, desired)
sqlift.Apply(ctx, db, plan, sqlift.ApplyOptions{})
```

## Features

- **Declarative** -- describe the end state, not the steps to get there
- **Plain SQL** -- no custom DSL; your schema files are valid SQLite DDL
- **Inspectable plans** -- review every SQL statement before execution
- **Destructive operation guard** -- dropping tables or columns requires explicit opt-in
- **Drift detection** -- detects out-of-band schema changes
- **Cross-language compatibility** -- C++ and Go produce identical schema hashes; databases are interchangeable

## Installation

### C++

Copy `dist/sqlift.h` and `dist/sqlift.cpp` into your project. Compile
`sqlift.cpp` alongside your other sources and link against SQLite3.

Requirements: C++23 compiler (GCC 13+, Clang 16+, Apple Clang 15+), SQLite3.

### Go

```sh
go get github.com/marcelocantos/sqlift/go/sqlift
```

Requires CGo (uses [mattn/go-sqlite3](https://github.com/mattn/go-sqlite3)).

### Agent guide

If you use an agentic coding tool (Claude Code, Cursor, Copilot, etc.), include
[`dist/agents-guide.md`](dist/agents-guide.md) in your project context for a
condensed API reference covering both C++ and Go.

## Documentation

- **[Guide](docs/guide.md)** -- walkthrough of core concepts and common workflows (C++)
- **[Reference](docs/reference.md)** -- complete API reference (C++)
- **[Agent Guide](dist/agents-guide.md)** -- condensed reference for AI coding agents (C++ and Go)
- **[Changelog](https://github.com/marcelocantos/sqlift/releases)** -- release history with notes

## Building and testing

### C++

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

## License

Apache 2.0 -- see [LICENSE](LICENSE).
