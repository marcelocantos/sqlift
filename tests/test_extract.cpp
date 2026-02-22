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
