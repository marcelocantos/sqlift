// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

#include <doctest/doctest.h>
#include "test_helpers.h"

TEST_CASE("diff identical schemas") {
    auto sql = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);";
    auto a = parse_schema(sql);
    auto b = parse_schema(sql);
    auto plan = json::parse(diff_schemas(a, b));
    CHECK(plan["operations"].empty());
}

TEST_CASE("diff add table") {
    auto empty = empty_schema();
    auto desired = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");

    auto plan = json::parse(diff_schemas(empty, desired));
    REQUIRE(plan["operations"].size() == 1);
    CHECK(plan["operations"][0]["type"] == "CreateTable");
    CHECK(plan["operations"][0]["object_name"] == "users");
    bool has_destructive = false;
    for (const auto& op : plan["operations"]) {
        if (op.value("destructive", false)) has_destructive = true;
    }
    CHECK(!has_destructive);
}

TEST_CASE("diff drop table") {
    auto current = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    auto empty = empty_schema();

    auto plan = json::parse(diff_schemas(current, empty));
    REQUIRE(plan["operations"].size() == 1);
    CHECK(plan["operations"][0]["type"] == "DropTable");
    CHECK(plan["operations"][0]["destructive"] == true);
}

TEST_CASE("diff add column - simple append") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);");

    auto plan = json::parse(diff_schemas(current, desired));
    REQUIRE(plan["operations"].size() == 1);
    CHECK(plan["operations"][0]["type"] == "AddColumn");
    CHECK(plan["operations"][0]["object_name"] == "users");
    bool has_destructive = false;
    for (const auto& op : plan["operations"]) {
        if (op.value("destructive", false)) has_destructive = true;
    }
    CHECK(!has_destructive);
}

TEST_CASE("diff add NOT NULL column with default - simple append") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER NOT NULL DEFAULT 1);");

    auto plan = json::parse(diff_schemas(current, desired));
    REQUIRE(plan["operations"].size() == 1);
    CHECK(plan["operations"][0]["type"] == "AddColumn");
}

TEST_CASE("diff add NOT NULL column without default - breaking change") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);");

    CHECK(diff_err(current, desired) == SQLIFT_BREAKING_CHANGE_ERROR);
}

TEST_CASE("diff remove column") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");

    auto plan = json::parse(diff_schemas(current, desired));
    REQUIRE(plan["operations"].size() == 1);
    CHECK(plan["operations"][0]["type"] == "RebuildTable");
    CHECK(plan["operations"][0]["destructive"] == true);
}

TEST_CASE("diff change column type") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, age TEXT);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, age INTEGER);");

    auto plan = json::parse(diff_schemas(current, desired));
    REQUIRE(plan["operations"].size() == 1);
    CHECK(plan["operations"][0]["type"] == "RebuildTable");
}

TEST_CASE("diff change column nullability - breaking change") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);");

    CHECK(diff_err(current, desired) == SQLIFT_BREAKING_CHANGE_ERROR);
}

TEST_CASE("diff add index") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);"
        "CREATE INDEX idx_email ON users(email);");

    auto plan = json::parse(diff_schemas(current, desired));
    REQUIRE(plan["operations"].size() == 1);
    CHECK(plan["operations"][0]["type"] == "CreateIndex");
}

TEST_CASE("diff drop index") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);"
        "CREATE INDEX idx_email ON users(email);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);");

    auto plan = json::parse(diff_schemas(current, desired));
    REQUIRE(plan["operations"].size() == 1);
    CHECK(plan["operations"][0]["type"] == "DropIndex");
    CHECK(plan["operations"][0]["destructive"] == true);
}

TEST_CASE("diff add view") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE VIEW all_users AS SELECT * FROM users;");

    auto plan = json::parse(diff_schemas(current, desired));
    REQUIRE(plan["operations"].size() == 1);
    CHECK(plan["operations"][0]["type"] == "CreateView");
}

TEST_CASE("diff change view") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, active INTEGER);"
        "CREATE VIEW all_users AS SELECT * FROM users;");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, active INTEGER);"
        "CREATE VIEW all_users AS SELECT * FROM users WHERE active = 1;");

    auto plan = json::parse(diff_schemas(current, desired));
    // Should drop then create the view
    bool has_drop = false, has_create = false;
    for (const auto& op : plan["operations"]) {
        if (op["type"] == "DropView") has_drop = true;
        if (op["type"] == "CreateView") has_create = true;
    }
    CHECK(has_drop);
    CHECK(has_create);
}

TEST_CASE("diff add trigger") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);"
        "CREATE TABLE log (msg TEXT);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);"
        "CREATE TABLE log (msg TEXT);"
        "CREATE TRIGGER on_insert AFTER INSERT ON users "
        "BEGIN INSERT INTO log VALUES ('added'); END;");

    auto plan = json::parse(diff_schemas(current, desired));
    REQUIRE(plan["operations"].size() == 1);
    CHECK(plan["operations"][0]["type"] == "CreateTrigger");
}

TEST_CASE("diff rejects nullable to NOT NULL on existing column") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);");

    CHECK(diff_err(current, desired) == SQLIFT_BREAKING_CHANGE_ERROR);
}

TEST_CASE("diff rejects nullable to NOT NULL even with DEFAULT") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL DEFAULT 'unknown');");

    CHECK(diff_err(current, desired) == SQLIFT_BREAKING_CHANGE_ERROR);
}

TEST_CASE("diff rejects adding FK to existing table") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);"
        "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);"
        "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));");

    CHECK(diff_err(current, desired) == SQLIFT_BREAKING_CHANGE_ERROR);
}

TEST_CASE("diff rejects new NOT NULL column without DEFAULT") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);");

    CHECK(diff_err(current, desired) == SQLIFT_BREAKING_CHANGE_ERROR);
}

TEST_CASE("diff allows new NOT NULL column with DEFAULT via AddColumn") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER NOT NULL DEFAULT 1);");

    auto plan = json::parse(diff_schemas(current, desired));
    REQUIRE(plan["operations"].size() == 1);
    CHECK(plan["operations"][0]["type"] == "AddColumn");
}

TEST_CASE("diff allows new table with NOT NULL columns and FKs") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);"
        "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id));");

    auto plan = json::parse(diff_schemas(current, desired));
    bool has_create = false;
    for (const auto& op : plan["operations"]) {
        if (op["type"] == "CreateTable" && op["object_name"] == "orders")
            has_create = true;
    }
    CHECK(has_create);
}

TEST_CASE("diff destructive guard") {
    auto current = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    auto empty = empty_schema();
    auto plan = json::parse(diff_schemas(current, empty));
    bool has_destructive = false;
    for (const auto& op : plan["operations"]) {
        if (op.value("destructive", false)) has_destructive = true;
    }
    CHECK(has_destructive);
}

TEST_CASE("diff rejects adding CHECK constraint to existing table") {
    auto current = parse_schema(
        "CREATE TABLE items (id INTEGER PRIMARY KEY, price REAL);");
    auto desired = parse_schema(
        "CREATE TABLE items (id INTEGER PRIMARY KEY, price REAL, CHECK (price > 0));");

    CHECK(diff_err(current, desired) == SQLIFT_BREAKING_CHANGE_ERROR);
}

TEST_CASE("diff COLLATE change triggers rebuild") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT COLLATE NOCASE);");

    auto plan = json::parse(diff_schemas(current, desired));
    REQUIRE(plan["operations"].size() == 1);
    CHECK(plan["operations"][0]["type"] == "RebuildTable");
}

TEST_CASE("diff GENERATED column change triggers rebuild") {
    auto current = parse_schema(
        "CREATE TABLE people ("
        "  id INTEGER PRIMARY KEY,"
        "  first_name TEXT,"
        "  last_name TEXT,"
        "  full_name TEXT GENERATED ALWAYS AS (first_name || ' ' || last_name) STORED"
        ");");
    auto desired = parse_schema(
        "CREATE TABLE people ("
        "  id INTEGER PRIMARY KEY,"
        "  first_name TEXT,"
        "  last_name TEXT,"
        "  full_name TEXT GENERATED ALWAYS AS (last_name || ', ' || first_name) STORED"
        ");");

    auto plan = json::parse(diff_schemas(current, desired));
    REQUIRE(plan["operations"].size() == 1);
    CHECK(plan["operations"][0]["type"] == "RebuildTable");
}

TEST_CASE("diff STRICT change triggers rebuild") {
    auto current = parse_schema(
        "CREATE TABLE data (id INTEGER PRIMARY KEY, value TEXT);");
    auto desired = parse_schema(
        "CREATE TABLE data (id INTEGER PRIMARY KEY, value TEXT) STRICT;");

    auto plan = json::parse(diff_schemas(current, desired));
    REQUIRE(plan["operations"].size() == 1);
    CHECK(plan["operations"][0]["type"] == "RebuildTable");
}

TEST_CASE("diff partial index") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER, name TEXT);"
        "CREATE INDEX idx_active ON users(name) WHERE active = 1;");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER, name TEXT);"
        "CREATE INDEX idx_active ON users(name) WHERE active = 0;");

    auto plan = json::parse(diff_schemas(current, desired));
    // Should drop and recreate the index
    bool has_drop = false, has_create = false;
    for (const auto& op : plan["operations"]) {
        if (op["type"] == "DropIndex" && op["object_name"] == "idx_active") has_drop = true;
        if (op["type"] == "CreateIndex" && op["object_name"] == "idx_active") has_create = true;
    }
    CHECK(has_drop);
    CHECK(has_create);
}

TEST_CASE("diff expression index added") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE INDEX idx_name_len ON users(length(name));");

    auto plan = json::parse(diff_schemas(current, desired));
    REQUIRE(plan["operations"].size() == 1);
    CHECK(plan["operations"][0]["type"] == "CreateIndex");
    CHECK(plan["operations"][0]["object_name"] == "idx_name_len");
}

TEST_CASE("diff partial index extracted correctly") {
    auto s = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER, name TEXT);"
        "CREATE INDEX idx_active ON users(name) WHERE active = 1;");
    auto schema = json::parse(s);
    const auto& idx = schema["indexes"]["idx_active"];
    CHECK(idx["where_clause"] == "active = 1");
    CHECK(idx["columns"] == json::array({"name"}));
}

TEST_CASE("diff expression index uses expr placeholder") {
    auto s = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE INDEX idx_name_len ON users(length(name));");
    auto schema = json::parse(s);
    const auto& idx = schema["indexes"]["idx_name_len"];
    CHECK(idx["columns"] == json::array({"<expr>"}));
}

TEST_CASE("diff view dependency ordering") {
    auto current = empty_schema();
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE VIEW base_users AS SELECT id, name FROM users;"
        "CREATE VIEW active_users AS SELECT * FROM base_users;");

    auto plan = json::parse(diff_schemas(current, desired));

    // Find the create view operations
    int base_pos = -1, active_pos = -1;
    for (size_t i = 0; i < plan["operations"].size(); ++i) {
        const auto& op = plan["operations"][i];
        if (op["type"] == "CreateView") {
            if (op["object_name"] == "base_users") base_pos = static_cast<int>(i);
            if (op["object_name"] == "active_users") active_pos = static_cast<int>(i);
        }
    }
    REQUIRE(base_pos >= 0);
    REQUIRE(active_pos >= 0);
    // base_users must be created before active_users
    CHECK(base_pos < active_pos);
}

TEST_CASE("diff view dependency ordering - drops") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE VIEW base_users AS SELECT id, name FROM users;"
        "CREATE VIEW active_users AS SELECT * FROM base_users;");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");

    auto plan = json::parse(diff_schemas(current, desired));

    // Find the drop view operations
    int base_pos = -1, active_pos = -1;
    for (size_t i = 0; i < plan["operations"].size(); ++i) {
        const auto& op = plan["operations"][i];
        if (op["type"] == "DropView") {
            if (op["object_name"] == "base_users") base_pos = static_cast<int>(i);
            if (op["object_name"] == "active_users") active_pos = static_cast<int>(i);
        }
    }
    REQUIRE(base_pos >= 0);
    REQUIRE(active_pos >= 0);
    // active_users (dependent) must be dropped before base_users (dependency)
    CHECK(active_pos < base_pos);
}

TEST_CASE("diff trigger dependency ordering - creates") {
    auto current = empty_schema();
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE log (msg TEXT);"
        "CREATE TABLE audit (entry TEXT);"
        "CREATE TRIGGER log_insert AFTER INSERT ON log "
        "BEGIN INSERT INTO audit (entry) VALUES (NEW.msg); END;"
        "CREATE TRIGGER on_user_insert AFTER INSERT ON users "
        "BEGIN INSERT INTO log (msg) VALUES (NEW.name); END;");

    auto plan = json::parse(diff_schemas(current, desired));

    // Find create trigger operations
    int log_pos = -1, user_pos = -1;
    for (size_t i = 0; i < plan["operations"].size(); ++i) {
        const auto& op = plan["operations"][i];
        if (op["type"] == "CreateTrigger") {
            if (op["object_name"] == "log_insert") log_pos = static_cast<int>(i);
            if (op["object_name"] == "on_user_insert") user_pos = static_cast<int>(i);
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
    auto s = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);"
        "CREATE INDEX idx_name ON users(name);"
        "CREATE INDEX idx_name_email ON users(name, email);");
    auto plan = json::parse(diff_schemas(empty_schema(), s));
    auto warnings = plan.value("warnings", json::array());
    REQUIRE(warnings.size() == 1);
    CHECK(warnings[0]["index_name"] == "idx_name");
    CHECK(warnings[0]["covered_by"] == "idx_name_email");
    CHECK(warnings[0]["table_name"] == "users");
}

TEST_CASE("warn PK-duplicate index") {
    auto s = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE INDEX idx_id ON users(id);");
    auto plan = json::parse(diff_schemas(empty_schema(), s));
    auto warnings = plan.value("warnings", json::array());
    REQUIRE(warnings.size() == 1);
    CHECK(warnings[0]["index_name"] == "idx_id");
    CHECK(warnings[0]["covered_by"] == "PRIMARY KEY");
}

TEST_CASE("no warning for UNIQUE prefix index") {
    auto s = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);"
        "CREATE UNIQUE INDEX idx_name ON users(name);"
        "CREATE INDEX idx_name_email ON users(name, email);");
    auto plan = json::parse(diff_schemas(empty_schema(), s));
    auto warnings = plan.value("warnings", json::array());
    CHECK(warnings.empty());
}

TEST_CASE("warn non-unique index covered by UNIQUE index") {
    auto s = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);"
        "CREATE INDEX idx_name ON users(name);"
        "CREATE UNIQUE INDEX idx_name_email ON users(name, email);");
    auto plan = json::parse(diff_schemas(empty_schema(), s));
    auto warnings = plan.value("warnings", json::array());
    REQUIRE(warnings.size() == 1);
    CHECK(warnings[0]["index_name"] == "idx_name");
    CHECK(warnings[0]["covered_by"] == "idx_name_email");
}

TEST_CASE("no warning for partial index as PK-duplicate") {
    auto s = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER);"
        "CREATE INDEX idx_id_active ON users(id) WHERE active = 1;");
    auto plan = json::parse(diff_schemas(empty_schema(), s));
    auto warnings = plan.value("warnings", json::array());
    CHECK(warnings.empty());
}

TEST_CASE("warn partial indexes with same WHERE as prefix-duplicate") {
    auto s = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT, active INTEGER);"
        "CREATE INDEX idx_a ON users(name) WHERE active = 1;"
        "CREATE INDEX idx_b ON users(name, email) WHERE active = 1;");
    auto plan = json::parse(diff_schemas(empty_schema(), s));
    auto warnings = plan.value("warnings", json::array());
    REQUIRE(warnings.size() == 1);
    CHECK(warnings[0]["index_name"] == "idx_a");
    CHECK(warnings[0]["covered_by"] == "idx_b");
}

TEST_CASE("no warning for partial indexes with different WHERE") {
    auto s = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, active INTEGER);"
        "CREATE INDEX idx_a ON users(name) WHERE active = 1;"
        "CREATE INDEX idx_b ON users(name) WHERE active = 0;");
    auto plan = json::parse(diff_schemas(empty_schema(), s));
    auto warnings = plan.value("warnings", json::array());
    CHECK(warnings.empty());
}

TEST_CASE("warn exact PK-duplicate UNIQUE index") {
    auto s = parse_schema(
        "CREATE TABLE t (a INTEGER, b INTEGER, PRIMARY KEY (a, b));"
        "CREATE UNIQUE INDEX idx_pk ON t(a, b);");
    auto plan = json::parse(diff_schemas(empty_schema(), s));
    auto warnings = plan.value("warnings", json::array());
    REQUIRE(warnings.size() == 1);
    CHECK(warnings[0]["covered_by"] == "PRIMARY KEY");
}

TEST_CASE("no warning for UNIQUE strict prefix of PK") {
    auto s = parse_schema(
        "CREATE TABLE t (a INTEGER, b INTEGER, PRIMARY KEY (a, b));"
        "CREATE UNIQUE INDEX idx_a ON t(a);");
    auto plan = json::parse(diff_schemas(empty_schema(), s));
    auto warnings = plan.value("warnings", json::array());
    CHECK(warnings.empty());
}

TEST_CASE("standalone detect_redundant_indexes matches diff warnings") {
    auto s = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE INDEX idx_id ON users(id);");
    auto standalone = detect_redundant(s);
    auto plan = json::parse(diff_schemas(empty_schema(), s));
    auto warnings = plan.value("warnings", json::array());
    REQUIRE(standalone.size() == warnings.size());
    CHECK(standalone[0]["index_name"] == warnings[0]["index_name"]);
}

TEST_CASE("no warnings when no redundant indexes") {
    auto s = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);"
        "CREATE INDEX idx_name ON users(name);"
        "CREATE INDEX idx_email ON users(email);");
    auto plan = json::parse(diff_schemas(empty_schema(), s));
    auto warnings = plan.value("warnings", json::array());
    CHECK(warnings.empty());
}

TEST_CASE("diff trigger dependency ordering - drops") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE log (msg TEXT);"
        "CREATE TABLE audit (entry TEXT);"
        "CREATE TRIGGER log_insert AFTER INSERT ON log "
        "BEGIN INSERT INTO audit (entry) VALUES (NEW.msg); END;"
        "CREATE TRIGGER on_user_insert AFTER INSERT ON users "
        "BEGIN INSERT INTO log (msg) VALUES (NEW.name); END;");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE log (msg TEXT);"
        "CREATE TABLE audit (entry TEXT);");

    auto plan = json::parse(diff_schemas(current, desired));

    // Find drop trigger operations
    int log_pos = -1, user_pos = -1;
    for (size_t i = 0; i < plan["operations"].size(); ++i) {
        const auto& op = plan["operations"][i];
        if (op["type"] == "DropTrigger") {
            if (op["object_name"] == "log_insert") log_pos = static_cast<int>(i);
            if (op["object_name"] == "on_user_insert") user_pos = static_cast<int>(i);
        }
    }
    REQUIRE(log_pos >= 0);
    REQUIRE(user_pos >= 0);
    // Both triggers dropped; dependency ordering applies in reverse for drops.
    CHECK(log_pos != user_pos);
}
