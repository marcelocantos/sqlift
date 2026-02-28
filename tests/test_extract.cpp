#include <doctest/doctest.h>
#include "sqlift.h"

using namespace sqlift;

TEST_CASE("extract empty database") {
    Database db(":memory:");
    Schema s = extract(db);
    CHECK(s.tables.empty());
    CHECK(s.indexes.empty());
    CHECK(s.views.empty());
    CHECK(s.triggers.empty());
}

TEST_CASE("extract table") {
    Database db(":memory:");
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);");

    Schema s = extract(db);
    REQUIRE(s.tables.size() == 1);
    REQUIRE(s.tables.count("users"));

    const auto& t = s.tables.at("users");
    REQUIRE(t.columns.size() == 2);
    CHECK(t.columns[0].name == "id");
    CHECK(t.columns[0].type == "INTEGER");
    CHECK(t.columns[0].pk == 1);
    CHECK(t.columns[1].name == "name");
    CHECK(t.columns[1].type == "TEXT");
    CHECK(t.columns[1].notnull == true);
}

TEST_CASE("extract excludes _sqlift_state") {
    Database db(":memory:");
    db.exec("CREATE TABLE _sqlift_state (key TEXT PRIMARY KEY, value TEXT NOT NULL);");
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY);");

    Schema s = extract(db);
    CHECK(s.tables.size() == 1);
    CHECK(s.tables.count("users"));
    CHECK(!s.tables.count("_sqlift_state"));
}

TEST_CASE("extract excludes sqlite_autoindex") {
    Database db(":memory:");
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT UNIQUE);");

    Schema s = extract(db);
    // The UNIQUE constraint creates a sqlite_autoindex, which should be excluded
    CHECK(s.indexes.empty());
}

TEST_CASE("extract index") {
    Database db(":memory:");
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);");
    db.exec("CREATE UNIQUE INDEX idx_email ON users(email);");

    Schema s = extract(db);
    REQUIRE(s.indexes.size() == 1);
    const auto& idx = s.indexes.at("idx_email");
    CHECK(idx.table_name == "users");
    CHECK(idx.unique == true);
    CHECK(idx.columns == std::vector<std::string>{"email"});
}

TEST_CASE("extract foreign key") {
    Database db(":memory:");
    db.exec(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);"
        "CREATE TABLE posts ("
        "  id INTEGER PRIMARY KEY,"
        "  user_id INTEGER REFERENCES users(id) ON DELETE CASCADE"
        ");");

    Schema s = extract(db);
    const auto& fks = s.tables.at("posts").foreign_keys;
    REQUIRE(fks.size() == 1);
    CHECK(fks[0].to_table == "users");
    CHECK(fks[0].on_delete == "CASCADE");
}

TEST_CASE("extract STRICT table") {
    Database db(":memory:");
    db.exec("CREATE TABLE data (id INTEGER PRIMARY KEY, value TEXT NOT NULL) STRICT;");

    Schema s = extract(db);
    CHECK(s.tables.at("data").strict == true);
}

TEST_CASE("extract WITHOUT ROWID table") {
    Database db(":memory:");
    db.exec("CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT) WITHOUT ROWID;");

    Schema s = extract(db);
    CHECK(s.tables.at("kv").without_rowid == true);
}

TEST_CASE("extract GENERATED columns") {
    Database db(":memory:");
    db.exec(
        "CREATE TABLE people ("
        "  id INTEGER PRIMARY KEY,"
        "  first TEXT,"
        "  last TEXT,"
        "  full_name TEXT GENERATED ALWAYS AS (first || ' ' || last) STORED"
        ");");

    Schema s = extract(db);
    const auto& cols = s.tables.at("people").columns;
    REQUIRE(cols.size() == 4);
    CHECK(cols[3].name == "full_name");
    CHECK(cols[3].generated == GeneratedType::Stored);
    CHECK(!cols[3].generated_expr.empty());
    CHECK(cols[0].generated == GeneratedType::Normal);
}

TEST_CASE("extract partial index") {
    Database db(":memory:");
    db.exec(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER, email TEXT);"
        "CREATE INDEX idx_active_email ON users(email) WHERE active = 1;");

    Schema s = extract(db);
    const auto& idx = s.indexes.at("idx_active_email");
    CHECK(idx.where_clause == "active = 1");
}

TEST_CASE("extract CHECK constraint") {
    Database db(":memory:");
    db.exec("CREATE TABLE products (id INTEGER PRIMARY KEY, price REAL, CHECK(price > 0));");

    Schema s = extract(db);
    const auto& checks = s.tables.at("products").check_constraints;
    REQUIRE(checks.size() == 1);
    CHECK(checks[0].expression == "price > 0");
}

TEST_CASE("extract COLLATE clause") {
    Database db(":memory:");
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT COLLATE NOCASE);");

    Schema s = extract(db);
    CHECK(s.tables.at("users").columns[1].collation == "NOCASE");
    CHECK(s.tables.at("users").columns[0].collation.empty());
}

TEST_CASE("extract named constraints") {
    Database db(":memory:");
    db.exec(
        "CREATE TABLE parent (id INTEGER PRIMARY KEY);"
        "CREATE TABLE child ("
        "  id INTEGER PRIMARY KEY,"
        "  parent_id INTEGER,"
        "  CONSTRAINT fk_parent FOREIGN KEY (parent_id) REFERENCES parent(id)"
        ");");

    Schema s = extract(db);
    const auto& fks = s.tables.at("child").foreign_keys;
    REQUIRE(fks.size() == 1);
    CHECK(fks[0].constraint_name == "fk_parent");
}

TEST_CASE("extract view") {
    Database db(":memory:");
    db.exec(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE VIEW user_names AS SELECT name FROM users;");

    Schema s = extract(db);
    REQUIRE(s.views.size() == 1);
    CHECK(s.views.count("user_names"));
    CHECK(!s.views.at("user_names").sql.empty());
}

TEST_CASE("extract trigger") {
    Database db(":memory:");
    db.exec(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE log (msg TEXT);"
        "CREATE TRIGGER on_user_insert AFTER INSERT ON users "
        "BEGIN INSERT INTO log (msg) VALUES ('inserted'); END;");

    Schema s = extract(db);
    REQUIRE(s.triggers.size() == 1);
    CHECK(s.triggers.count("on_user_insert"));
    CHECK(s.triggers.at("on_user_insert").table_name == "users");
    CHECK(!s.triggers.at("on_user_insert").sql.empty());
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
    Schema parsed = parse(ddl);

    // Extract from a live DB with the same DDL
    Database db(":memory:");
    db.exec(ddl);
    Schema extracted = extract(db);

    // FK ordering should match
    const auto& p_fks = parsed.tables.at("child").foreign_keys;
    const auto& e_fks = extracted.tables.at("child").foreign_keys;
    REQUIRE(p_fks.size() == e_fks.size());
    for (size_t i = 0; i < p_fks.size(); ++i) {
        CHECK(p_fks[i].to_table == e_fks[i].to_table);
        CHECK(p_fks[i].from_columns == e_fks[i].from_columns);
    }
}

TEST_CASE("extract non-unique index") {
    Database db(":memory:");
    db.exec(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);"
        "CREATE INDEX idx_email ON users(email);");

    Schema s = extract(db);
    const auto& idx = s.indexes.at("idx_email");
    CHECK(idx.unique == false);
}
