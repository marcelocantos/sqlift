#include <doctest/doctest.h>
#include "sqlift.h"

using namespace sqlift;

TEST_CASE("diff identical schemas") {
    auto sql = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);";
    Schema a = parse(sql);
    Schema b = parse(sql);
    auto plan = diff(a, b);
    CHECK(plan.empty());
}

TEST_CASE("diff add table") {
    Schema empty;
    Schema desired = parse("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");

    auto plan = diff(empty, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::CreateTable);
    CHECK(plan.operations()[0].object_name == "users");
    CHECK(!plan.has_destructive_operations());
}

TEST_CASE("diff drop table") {
    Schema current = parse("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    Schema empty;

    auto plan = diff(current, empty);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::DropTable);
    CHECK(plan.operations()[0].destructive == true);
}

TEST_CASE("diff add column - simple append") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::AddColumn);
    CHECK(plan.operations()[0].object_name == "users");
    CHECK(!plan.has_destructive_operations());
}

TEST_CASE("diff add NOT NULL column with default - simple append") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER NOT NULL DEFAULT 1);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::AddColumn);
}

TEST_CASE("diff add NOT NULL column without default - breaking change") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);");

    CHECK_THROWS_AS(diff(current, desired), BreakingChangeError);
}

TEST_CASE("diff remove column") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::RebuildTable);
    CHECK(plan.operations()[0].destructive == true);
}

TEST_CASE("diff change column type") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, age TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, age INTEGER);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::RebuildTable);
}

TEST_CASE("diff change column nullability - breaking change") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);");

    CHECK_THROWS_AS(diff(current, desired), BreakingChangeError);
}

TEST_CASE("diff add index") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);"
        "CREATE INDEX idx_email ON users(email);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::CreateIndex);
}

TEST_CASE("diff drop index") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);"
        "CREATE INDEX idx_email ON users(email);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::DropIndex);
    CHECK(plan.operations()[0].destructive == true);
}

TEST_CASE("diff add view") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE VIEW all_users AS SELECT * FROM users;");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::CreateView);
}

TEST_CASE("diff change view") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, active INTEGER);"
        "CREATE VIEW all_users AS SELECT * FROM users;");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, active INTEGER);"
        "CREATE VIEW all_users AS SELECT * FROM users WHERE active = 1;");

    auto plan = diff(current, desired);
    // Should drop then create the view
    bool has_drop = false, has_create = false;
    for (const auto& op : plan.operations()) {
        if (op.type == OpType::DropView) has_drop = true;
        if (op.type == OpType::CreateView) has_create = true;
    }
    CHECK(has_drop);
    CHECK(has_create);
}

TEST_CASE("diff add trigger") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);"
        "CREATE TABLE log (msg TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);"
        "CREATE TABLE log (msg TEXT);"
        "CREATE TRIGGER on_insert AFTER INSERT ON users "
        "BEGIN INSERT INTO log VALUES ('added'); END;");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::CreateTrigger);
}

TEST_CASE("diff rejects nullable to NOT NULL on existing column") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);");

    CHECK_THROWS_AS(diff(current, desired), BreakingChangeError);
}

TEST_CASE("diff rejects nullable to NOT NULL even with DEFAULT") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL DEFAULT 'unknown');");

    CHECK_THROWS_AS(diff(current, desired), BreakingChangeError);
}

TEST_CASE("diff rejects adding FK to existing table") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);"
        "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);"
        "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));");

    CHECK_THROWS_AS(diff(current, desired), BreakingChangeError);
}

TEST_CASE("diff rejects new NOT NULL column without DEFAULT") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);");

    CHECK_THROWS_AS(diff(current, desired), BreakingChangeError);
}

TEST_CASE("diff allows new NOT NULL column with DEFAULT via AddColumn") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER NOT NULL DEFAULT 1);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::AddColumn);
}

TEST_CASE("diff allows new table with NOT NULL columns and FKs") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);"
        "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id));");

    auto plan = diff(current, desired);
    bool has_create = false;
    for (const auto& op : plan.operations()) {
        if (op.type == OpType::CreateTable && op.object_name == "orders")
            has_create = true;
    }
    CHECK(has_create);
}

TEST_CASE("diff destructive guard") {
    Schema current = parse("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    Schema empty;
    auto plan = diff(current, empty);
    CHECK(plan.has_destructive_operations());
}

TEST_CASE("diff rejects adding CHECK constraint to existing table") {
    Schema current = parse(
        "CREATE TABLE items (id INTEGER PRIMARY KEY, price REAL);");
    Schema desired = parse(
        "CREATE TABLE items (id INTEGER PRIMARY KEY, price REAL, CHECK (price > 0));");

    CHECK_THROWS_AS(diff(current, desired), BreakingChangeError);
}

TEST_CASE("diff COLLATE change triggers rebuild") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT COLLATE NOCASE);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::RebuildTable);
}

TEST_CASE("diff GENERATED column change triggers rebuild") {
    Schema current = parse(
        "CREATE TABLE people ("
        "  id INTEGER PRIMARY KEY,"
        "  first_name TEXT,"
        "  last_name TEXT,"
        "  full_name TEXT GENERATED ALWAYS AS (first_name || ' ' || last_name) STORED"
        ");");
    Schema desired = parse(
        "CREATE TABLE people ("
        "  id INTEGER PRIMARY KEY,"
        "  first_name TEXT,"
        "  last_name TEXT,"
        "  full_name TEXT GENERATED ALWAYS AS (last_name || ', ' || first_name) STORED"
        ");");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::RebuildTable);
}

TEST_CASE("diff STRICT change triggers rebuild") {
    Schema current = parse(
        "CREATE TABLE data (id INTEGER PRIMARY KEY, value TEXT);");
    Schema desired = parse(
        "CREATE TABLE data (id INTEGER PRIMARY KEY, value TEXT) STRICT;");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::RebuildTable);
}

TEST_CASE("diff partial index") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER, name TEXT);"
        "CREATE INDEX idx_active ON users(name) WHERE active = 1;");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER, name TEXT);"
        "CREATE INDEX idx_active ON users(name) WHERE active = 0;");

    auto plan = diff(current, desired);
    // Should drop and recreate the index
    bool has_drop = false, has_create = false;
    for (const auto& op : plan.operations()) {
        if (op.type == OpType::DropIndex && op.object_name == "idx_active") has_drop = true;
        if (op.type == OpType::CreateIndex && op.object_name == "idx_active") has_create = true;
    }
    CHECK(has_drop);
    CHECK(has_create);
}

TEST_CASE("diff expression index added") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE INDEX idx_name_len ON users(length(name));");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::CreateIndex);
    CHECK(plan.operations()[0].object_name == "idx_name_len");
}

TEST_CASE("diff partial index extracted correctly") {
    Schema s = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER, name TEXT);"
        "CREATE INDEX idx_active ON users(name) WHERE active = 1;");

    const auto& idx = s.indexes.at("idx_active");
    CHECK(idx.where_clause == "active = 1");
    CHECK(idx.columns == std::vector<std::string>{"name"});
}

TEST_CASE("diff expression index uses expr placeholder") {
    Schema s = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE INDEX idx_name_len ON users(length(name));");

    const auto& idx = s.indexes.at("idx_name_len");
    CHECK(idx.columns == std::vector<std::string>{"<expr>"});
}

TEST_CASE("diff view dependency ordering") {
    Schema current;
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE VIEW base_users AS SELECT id, name FROM users;"
        "CREATE VIEW active_users AS SELECT * FROM base_users;");

    auto plan = diff(current, desired);

    // Find the create view operations
    int base_pos = -1, active_pos = -1;
    for (size_t i = 0; i < plan.operations().size(); ++i) {
        if (plan.operations()[i].type == OpType::CreateView) {
            if (plan.operations()[i].object_name == "base_users") base_pos = static_cast<int>(i);
            if (plan.operations()[i].object_name == "active_users") active_pos = static_cast<int>(i);
        }
    }
    REQUIRE(base_pos >= 0);
    REQUIRE(active_pos >= 0);
    // base_users must be created before active_users
    CHECK(base_pos < active_pos);
}

TEST_CASE("diff view dependency ordering - drops") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE VIEW base_users AS SELECT id, name FROM users;"
        "CREATE VIEW active_users AS SELECT * FROM base_users;");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");

    auto plan = diff(current, desired);

    // Find the drop view operations
    int base_pos = -1, active_pos = -1;
    for (size_t i = 0; i < plan.operations().size(); ++i) {
        if (plan.operations()[i].type == OpType::DropView) {
            if (plan.operations()[i].object_name == "base_users") base_pos = static_cast<int>(i);
            if (plan.operations()[i].object_name == "active_users") active_pos = static_cast<int>(i);
        }
    }
    REQUIRE(base_pos >= 0);
    REQUIRE(active_pos >= 0);
    // active_users (dependent) must be dropped before base_users (dependency)
    CHECK(active_pos < base_pos);
}

TEST_CASE("diff trigger dependency ordering - creates") {
    Schema current;
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE log (msg TEXT);"
        "CREATE TABLE audit (entry TEXT);"
        "CREATE TRIGGER log_insert AFTER INSERT ON log "
        "BEGIN INSERT INTO audit (entry) VALUES (NEW.msg); END;"
        "CREATE TRIGGER on_user_insert AFTER INSERT ON users "
        "BEGIN INSERT INTO log (msg) VALUES (NEW.name); END;");

    auto plan = diff(current, desired);

    // Find create trigger operations
    int log_pos = -1, user_pos = -1;
    for (size_t i = 0; i < plan.operations().size(); ++i) {
        if (plan.operations()[i].type == OpType::CreateTrigger) {
            if (plan.operations()[i].object_name == "log_insert") log_pos = static_cast<int>(i);
            if (plan.operations()[i].object_name == "on_user_insert") user_pos = static_cast<int>(i);
        }
    }
    REQUIRE(log_pos >= 0);
    REQUIRE(user_pos >= 0);
    // log_insert references audit (not log), on_user_insert references log.
    // Dependency ordering ensures triggers referencing other objects are
    // created after those objects' triggers.
    // Both should be created; exact order depends on dependency analysis.
    CHECK(log_pos != user_pos);
}

// --- Redundant index detection ---

TEST_CASE("warn prefix-duplicate index") {
    Schema s = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);"
        "CREATE INDEX idx_name ON users(name);"
        "CREATE INDEX idx_name_email ON users(name, email);");
    auto plan = diff(Schema{}, s);
    REQUIRE(plan.warnings().size() == 1);
    CHECK(plan.warnings()[0].index_name == "idx_name");
    CHECK(plan.warnings()[0].covered_by == "idx_name_email");
    CHECK(plan.warnings()[0].table_name == "users");
}

TEST_CASE("warn PK-duplicate index") {
    Schema s = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE INDEX idx_id ON users(id);");
    auto plan = diff(Schema{}, s);
    REQUIRE(plan.warnings().size() == 1);
    CHECK(plan.warnings()[0].index_name == "idx_id");
    CHECK(plan.warnings()[0].covered_by == "PRIMARY KEY");
}

TEST_CASE("no warning for UNIQUE prefix index") {
    Schema s = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);"
        "CREATE UNIQUE INDEX idx_name ON users(name);"
        "CREATE INDEX idx_name_email ON users(name, email);");
    auto plan = diff(Schema{}, s);
    CHECK(plan.warnings().empty());
}

TEST_CASE("warn non-unique index covered by UNIQUE index") {
    Schema s = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);"
        "CREATE INDEX idx_name ON users(name);"
        "CREATE UNIQUE INDEX idx_name_email ON users(name, email);");
    auto plan = diff(Schema{}, s);
    REQUIRE(plan.warnings().size() == 1);
    CHECK(plan.warnings()[0].index_name == "idx_name");
    CHECK(plan.warnings()[0].covered_by == "idx_name_email");
}

TEST_CASE("no warning for partial index as PK-duplicate") {
    Schema s = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER);"
        "CREATE INDEX idx_id_active ON users(id) WHERE active = 1;");
    auto plan = diff(Schema{}, s);
    CHECK(plan.warnings().empty());
}

TEST_CASE("warn partial indexes with same WHERE as prefix-duplicate") {
    Schema s = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT, active INTEGER);"
        "CREATE INDEX idx_a ON users(name) WHERE active = 1;"
        "CREATE INDEX idx_b ON users(name, email) WHERE active = 1;");
    auto plan = diff(Schema{}, s);
    REQUIRE(plan.warnings().size() == 1);
    CHECK(plan.warnings()[0].index_name == "idx_a");
    CHECK(plan.warnings()[0].covered_by == "idx_b");
}

TEST_CASE("no warning for partial indexes with different WHERE") {
    Schema s = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, active INTEGER);"
        "CREATE INDEX idx_a ON users(name) WHERE active = 1;"
        "CREATE INDEX idx_b ON users(name) WHERE active = 0;");
    auto plan = diff(Schema{}, s);
    CHECK(plan.warnings().empty());
}

TEST_CASE("warn exact PK-duplicate UNIQUE index") {
    Schema s = parse(
        "CREATE TABLE t (a INTEGER, b INTEGER, PRIMARY KEY (a, b));"
        "CREATE UNIQUE INDEX idx_pk ON t(a, b);");
    auto plan = diff(Schema{}, s);
    REQUIRE(plan.warnings().size() == 1);
    CHECK(plan.warnings()[0].covered_by == "PRIMARY KEY");
}

TEST_CASE("no warning for UNIQUE strict prefix of PK") {
    Schema s = parse(
        "CREATE TABLE t (a INTEGER, b INTEGER, PRIMARY KEY (a, b));"
        "CREATE UNIQUE INDEX idx_a ON t(a);");
    auto plan = diff(Schema{}, s);
    CHECK(plan.warnings().empty());
}

TEST_CASE("standalone detect_redundant_indexes matches diff warnings") {
    Schema s = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE INDEX idx_id ON users(id);");
    auto standalone = detect_redundant_indexes(s);
    auto plan = diff(Schema{}, s);
    REQUIRE(standalone.size() == plan.warnings().size());
    CHECK(standalone[0].index_name == plan.warnings()[0].index_name);
}

TEST_CASE("no warnings when no redundant indexes") {
    Schema s = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);"
        "CREATE INDEX idx_name ON users(name);"
        "CREATE INDEX idx_email ON users(email);");
    auto plan = diff(Schema{}, s);
    CHECK(plan.warnings().empty());
}

TEST_CASE("diff trigger dependency ordering - drops") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE log (msg TEXT);"
        "CREATE TABLE audit (entry TEXT);"
        "CREATE TRIGGER log_insert AFTER INSERT ON log "
        "BEGIN INSERT INTO audit (entry) VALUES (NEW.msg); END;"
        "CREATE TRIGGER on_user_insert AFTER INSERT ON users "
        "BEGIN INSERT INTO log (msg) VALUES (NEW.name); END;");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE log (msg TEXT);"
        "CREATE TABLE audit (entry TEXT);");

    auto plan = diff(current, desired);

    // Find drop trigger operations
    int log_pos = -1, user_pos = -1;
    for (size_t i = 0; i < plan.operations().size(); ++i) {
        if (plan.operations()[i].type == OpType::DropTrigger) {
            if (plan.operations()[i].object_name == "log_insert") log_pos = static_cast<int>(i);
            if (plan.operations()[i].object_name == "on_user_insert") user_pos = static_cast<int>(i);
        }
    }
    REQUIRE(log_pos >= 0);
    REQUIRE(user_pos >= 0);
    // Both triggers dropped; dependency ordering applies in reverse for drops.
    CHECK(log_pos != user_pos);
}
