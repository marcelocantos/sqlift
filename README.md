# sqlift

A C++ library for declarative SQLite schema migration.

Declare your desired schema as plain SQL. sqlift computes the diff against a live
database and applies the changes.

```cpp
#include "sqlift.h"

// Declare desired schema as plain SQL
sqlift::Schema desired = sqlift::parse(R"(
    CREATE TABLE users (
        id INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        email TEXT UNIQUE
    );
    CREATE INDEX idx_users_email ON users(email);
)");

// Open database and extract current schema
sqlift::Database db("app.db");
sqlift::Schema current = sqlift::extract(db);

// Compute diff (pure function, no DB access)
sqlift::MigrationPlan plan = sqlift::diff(current, desired);

// Inspect what will happen
for (const auto& op : plan.operations()) {
    std::cout << op.description << "\n";
    for (const auto& sql : op.sql)
        std::cout << "  " << sql << "\n";
}

// Apply
sqlift::apply(db, plan);
```

## Features

- **Declarative** -- describe the end state, not the steps to get there
- **Plain SQL** -- no custom DSL; your schema files are valid SQLite DDL
- **Inspectable plans** -- review every SQL statement before execution
- **Destructive operation guard** -- dropping tables or columns requires explicit opt-in
- **Drift detection** -- detects out-of-band schema changes
- **Two files** -- the entire library is `sqlift.h` + `sqlift.cpp`
- **No runtime dependencies** beyond SQLite3

## Installation

Copy `sqlift.h` and `sqlift.cpp` into your project. Compile `sqlift.cpp`
alongside your other sources and link against SQLite3. That's it.

If you use an agentic coding tool (Claude Code, Cursor, Copilot, etc.), include
[`agents-guide.md`](agents-guide.md) in your project context for a condensed API
reference.

### Requirements

- C++23 compiler
- SQLite3 (headers and library)
- [doctest](https://github.com/doctest/doctest) (for running tests only; vendored)
- [mk](https://github.com/marcelocantos/mk) build tool (for building from source)

## Documentation

- **[Guide](docs/guide.md)** -- walkthrough of core concepts and common workflows
- **[Reference](docs/reference.md)** -- complete API reference

## Building and testing

```sh
mk            # build library
mk test       # build and run tests
mk lib        # build static library only
mk clean      # remove build artifacts
```

## License

Apache 2.0 -- see [LICENSE](LICENSE).
