// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

#include <doctest/doctest.h>
#include "test_helpers.h"

TEST_CASE("extract empty database") {
    TestDB db;
    auto j = json::parse(extract_schema(db.db));
    CHECK(j["tables"].empty());
    CHECK(j["indexes"].empty());
    CHECK(j["views"].empty());
    CHECK(j["triggers"].empty());
}

TEST_CASE("extract table") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);");

    auto j = json::parse(extract_schema(db.db));
    REQUIRE(j["tables"].size() == 1);
    REQUIRE(j["tables"].contains("users"));

    const auto& cols = j["tables"]["users"]["columns"];
    REQUIRE(cols.size() == 2);
    CHECK(cols[0]["name"] == "id");
    CHECK(cols[0]["type"] == "INTEGER");
    CHECK(cols[0]["pk"] == 1);
    CHECK(cols[1]["name"] == "name");
    CHECK(cols[1]["type"] == "TEXT");
    CHECK(cols[1]["notnull"] == true);
}

TEST_CASE("extract excludes _sqlift_state") {
    TestDB db;
    db.exec("CREATE TABLE _sqlift_state (key TEXT PRIMARY KEY, value TEXT NOT NULL);");
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY);");

    auto j = json::parse(extract_schema(db.db));
    CHECK(j["tables"].size() == 1);
    CHECK(j["tables"].contains("users"));
    CHECK(!j["tables"].contains("_sqlift_state"));
}

TEST_CASE("extract excludes sqlite_autoindex") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT UNIQUE);");

    auto j = json::parse(extract_schema(db.db));
    // The UNIQUE constraint creates a sqlite_autoindex, which should be excluded
    CHECK(j["indexes"].empty());
}

TEST_CASE("extract index") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);");
    db.exec("CREATE UNIQUE INDEX idx_email ON users(email);");

    auto j = json::parse(extract_schema(db.db));
    REQUIRE(j["indexes"].size() == 1);
    const auto& idx = j["indexes"]["idx_email"];
    CHECK(idx["table_name"] == "users");
    CHECK(idx["unique"] == true);
    CHECK(idx["columns"] == json::array({"email"}));
}

TEST_CASE("extract foreign key") {
    TestDB db;
    db.exec(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);"
        "CREATE TABLE posts ("
        "  id INTEGER PRIMARY KEY,"
        "  user_id INTEGER REFERENCES users(id) ON DELETE CASCADE"
        ");");

    auto j = json::parse(extract_schema(db.db));
    const auto& fks = j["tables"]["posts"]["foreign_keys"];
    REQUIRE(fks.size() == 1);
    CHECK(fks[0]["to_table"] == "users");
    CHECK(fks[0]["on_delete"] == "CASCADE");
}

TEST_CASE("extract STRICT table") {
    TestDB db;
    db.exec("CREATE TABLE data (id INTEGER PRIMARY KEY, value TEXT NOT NULL) STRICT;");

    auto j = json::parse(extract_schema(db.db));
    CHECK(j["tables"]["data"]["strict"] == true);
}

TEST_CASE("extract WITHOUT ROWID table") {
    TestDB db;
    db.exec("CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT) WITHOUT ROWID;");

    auto j = json::parse(extract_schema(db.db));
    CHECK(j["tables"]["kv"]["without_rowid"] == true);
}

TEST_CASE("extract GENERATED columns") {
    TestDB db;
    db.exec(
        "CREATE TABLE people ("
        "  id INTEGER PRIMARY KEY,"
        "  first TEXT,"
        "  last TEXT,"
        "  full_name TEXT GENERATED ALWAYS AS (first || ' ' || last) STORED"
        ");");

    auto j = json::parse(extract_schema(db.db));
    const auto& cols = j["tables"]["people"]["columns"];
    REQUIRE(cols.size() == 4);
    CHECK(cols[3]["name"] == "full_name");
    CHECK(cols[3]["generated"] == 3);  // GeneratedType::Stored = 3
    CHECK(!cols[3]["generated_expr"].get<std::string>().empty());
    CHECK(cols[0]["generated"] == 0);  // GeneratedType::Normal = 0
}

TEST_CASE("extract partial index") {
    TestDB db;
    db.exec(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER, email TEXT);"
        "CREATE INDEX idx_active_email ON users(email) WHERE active = 1;");

    auto j = json::parse(extract_schema(db.db));
    const auto& idx = j["indexes"]["idx_active_email"];
    CHECK(idx["where_clause"] == "active = 1");
}

TEST_CASE("extract CHECK constraint") {
    TestDB db;
    db.exec("CREATE TABLE products (id INTEGER PRIMARY KEY, price REAL, CHECK(price > 0));");

    auto j = json::parse(extract_schema(db.db));
    const auto& checks = j["tables"]["products"]["check_constraints"];
    REQUIRE(checks.size() == 1);
    CHECK(checks[0]["expression"] == "price > 0");
}

TEST_CASE("extract COLLATE clause") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT COLLATE NOCASE);");

    auto j = json::parse(extract_schema(db.db));
    const auto& cols = j["tables"]["users"]["columns"];
    CHECK(cols[1]["collation"] == "NOCASE");
    CHECK(cols[0]["collation"] == "");
}

TEST_CASE("extract named constraints") {
    TestDB db;
    db.exec(
        "CREATE TABLE parent (id INTEGER PRIMARY KEY);"
        "CREATE TABLE child ("
        "  id INTEGER PRIMARY KEY,"
        "  parent_id INTEGER,"
        "  CONSTRAINT fk_parent FOREIGN KEY (parent_id) REFERENCES parent(id)"
        ");");

    auto j = json::parse(extract_schema(db.db));
    const auto& fks = j["tables"]["child"]["foreign_keys"];
    REQUIRE(fks.size() == 1);
    CHECK(fks[0]["constraint_name"] == "fk_parent");
}

TEST_CASE("extract view") {
    TestDB db;
    db.exec(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE VIEW user_names AS SELECT name FROM users;");

    auto j = json::parse(extract_schema(db.db));
    REQUIRE(j["views"].size() == 1);
    CHECK(j["views"].contains("user_names"));
    CHECK(!j["views"]["user_names"]["sql"].get<std::string>().empty());
}

TEST_CASE("extract trigger") {
    TestDB db;
    db.exec(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE log (msg TEXT);"
        "CREATE TRIGGER on_user_insert AFTER INSERT ON users "
        "BEGIN INSERT INTO log (msg) VALUES ('inserted'); END;");

    auto j = json::parse(extract_schema(db.db));
    REQUIRE(j["triggers"].size() == 1);
    CHECK(j["triggers"].contains("on_user_insert"));
    CHECK(j["triggers"]["on_user_insert"]["table_name"] == "users");
    CHECK(!j["triggers"]["on_user_insert"]["sql"].get<std::string>().empty());
}

TEST_CASE("extract FK ordering matches parse") {
    const char* ddl =
        "CREATE TABLE parent (id INTEGER PRIMARY KEY);"
        "CREATE TABLE other (id INTEGER PRIMARY KEY);"
        "CREATE TABLE child ("
        "  id INTEGER PRIMARY KEY,"
        "  parent_id INTEGER REFERENCES parent(id),"
        "  other_id INTEGER REFERENCES other(id)"
        ");";

    // Parse from DDL
    auto parsed = json::parse(parse_schema(ddl));

    // Extract from a live DB with the same DDL
    TestDB db;
    db.exec(ddl);
    auto extracted = json::parse(extract_schema(db.db));

    // FK ordering should match
    const auto& p_fks = parsed["tables"]["child"]["foreign_keys"];
    const auto& e_fks = extracted["tables"]["child"]["foreign_keys"];
    REQUIRE(p_fks.size() == e_fks.size());
    for (size_t i = 0; i < p_fks.size(); ++i) {
        CHECK(p_fks[i]["to_table"] == e_fks[i]["to_table"]);
        CHECK(p_fks[i]["from_columns"] == e_fks[i]["from_columns"]);
    }
}

TEST_CASE("extract non-unique index") {
    TestDB db;
    db.exec(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);"
        "CREATE INDEX idx_email ON users(email);");

    auto j = json::parse(extract_schema(db.db));
    const auto& idx = j["indexes"]["idx_email"];
    CHECK(idx["unique"] == false);
}
