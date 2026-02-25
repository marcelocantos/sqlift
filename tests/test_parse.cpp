#include <doctest/doctest.h>
#include "sqlift.h"

using namespace sqlift;

TEST_CASE("parse empty string") {
    Schema s = parse("");
    CHECK(s.tables.empty());
    CHECK(s.indexes.empty());
    CHECK(s.views.empty());
    CHECK(s.triggers.empty());
}

TEST_CASE("parse single table") {
    Schema s = parse(
        "CREATE TABLE users ("
        "  id INTEGER PRIMARY KEY,"
        "  name TEXT NOT NULL,"
        "  email TEXT"
        ");");

    REQUIRE(s.tables.size() == 1);
    REQUIRE(s.tables.count("users"));

    const auto& t = s.tables.at("users");
    CHECK(t.name == "users");
    REQUIRE(t.columns.size() == 3);

    CHECK(t.columns[0].name == "id");
    CHECK(t.columns[0].type == "INTEGER");
    CHECK(t.columns[0].pk == 1);

    CHECK(t.columns[1].name == "name");
    CHECK(t.columns[1].type == "TEXT");
    CHECK(t.columns[1].notnull == true);
    CHECK(t.columns[1].pk == 0);

    CHECK(t.columns[2].name == "email");
    CHECK(t.columns[2].type == "TEXT");
    CHECK(t.columns[2].notnull == false);
}

TEST_CASE("parse table with default") {
    Schema s = parse(
        "CREATE TABLE items ("
        "  id INTEGER PRIMARY KEY,"
        "  active INTEGER NOT NULL DEFAULT 1"
        ");");

    const auto& col = s.tables.at("items").columns[1];
    CHECK(col.name == "active");
    CHECK(col.notnull == true);
    CHECK(col.default_value == "1");
}

TEST_CASE("parse table with foreign key") {
    Schema s = parse(
        "CREATE TABLE posts ("
        "  id INTEGER PRIMARY KEY,"
        "  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE"
        ");"
        "CREATE TABLE users ("
        "  id INTEGER PRIMARY KEY"
        ");");

    REQUIRE(s.tables.count("posts"));
    const auto& fks = s.tables.at("posts").foreign_keys;
    REQUIRE(fks.size() == 1);
    CHECK(fks[0].to_table == "users");
    CHECK(fks[0].to_columns == std::vector<std::string>{"id"});
    CHECK(fks[0].on_delete == "CASCADE");
}

TEST_CASE("parse index") {
    Schema s = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);"
        "CREATE UNIQUE INDEX idx_email ON users(email);");

    REQUIRE(s.indexes.size() == 1);
    REQUIRE(s.indexes.count("idx_email"));
    const auto& idx = s.indexes.at("idx_email");
    CHECK(idx.table_name == "users");
    CHECK(idx.unique == true);
    CHECK(idx.columns == std::vector<std::string>{"email"});
}

TEST_CASE("parse view") {
    Schema s = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE VIEW active_users AS SELECT * FROM users;");

    REQUIRE(s.views.size() == 1);
    REQUIRE(s.views.count("active_users"));
}

TEST_CASE("parse trigger") {
    Schema s = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE log (msg TEXT);"
        "CREATE TRIGGER on_user_insert AFTER INSERT ON users "
        "BEGIN INSERT INTO log VALUES ('user added'); END;");

    REQUIRE(s.triggers.size() == 1);
    REQUIRE(s.triggers.count("on_user_insert"));
    CHECK(s.triggers.at("on_user_insert").table_name == "users");
}

TEST_CASE("parse invalid SQL throws ParseError") {
    CHECK_THROWS_AS(parse("NOT VALID SQL"), ParseError);
}

TEST_CASE("parse composite primary key") {
    Schema s = parse(
        "CREATE TABLE user_roles ("
        "  user_id INTEGER,"
        "  role_id INTEGER,"
        "  PRIMARY KEY (user_id, role_id)"
        ");");

    const auto& t = s.tables.at("user_roles");
    CHECK(t.columns[0].pk == 1);
    CHECK(t.columns[1].pk == 2);
}

TEST_CASE("parse column with COLLATE NOCASE") {
    Schema s = parse(
        "CREATE TABLE users ("
        "  id INTEGER PRIMARY KEY,"
        "  name TEXT COLLATE NOCASE"
        ");");

    const auto& t = s.tables.at("users");
    REQUIRE(t.columns.size() == 2);
    CHECK(t.columns[1].name == "name");
    CHECK(t.columns[1].collation == "NOCASE");
    // Default collation should be empty
    CHECK(t.columns[0].collation.empty());
}

TEST_CASE("parse table with CHECK constraint") {
    Schema s = parse(
        "CREATE TABLE items ("
        "  id INTEGER PRIMARY KEY,"
        "  price REAL NOT NULL,"
        "  CHECK (price > 0)"
        ");");

    const auto& t = s.tables.at("items");
    REQUIRE(t.check_constraints.size() == 1);
    CHECK(t.check_constraints[0].name.empty());
    CHECK(t.check_constraints[0].expression == "price > 0");
}

TEST_CASE("parse table with named CHECK constraint") {
    Schema s = parse(
        "CREATE TABLE items ("
        "  id INTEGER PRIMARY KEY,"
        "  price REAL NOT NULL,"
        "  CONSTRAINT positive_price CHECK (price > 0)"
        ");");

    const auto& t = s.tables.at("items");
    REQUIRE(t.check_constraints.size() == 1);
    CHECK(t.check_constraints[0].name == "positive_price");
    CHECK(t.check_constraints[0].expression == "price > 0");
}

TEST_CASE("parse stored generated column") {
    Schema s = parse(
        "CREATE TABLE people ("
        "  id INTEGER PRIMARY KEY,"
        "  first_name TEXT,"
        "  last_name TEXT,"
        "  full_name TEXT GENERATED ALWAYS AS (first_name || ' ' || last_name) STORED"
        ");");

    const auto& t = s.tables.at("people");
    REQUIRE(t.columns.size() == 4);
    CHECK(t.columns[3].name == "full_name");
    CHECK(t.columns[3].generated == sqlift::GeneratedType::Stored);
    CHECK(t.columns[3].generated_expr == "first_name || ' ' || last_name");
}

TEST_CASE("parse virtual generated column") {
    Schema s = parse(
        "CREATE TABLE products ("
        "  id INTEGER PRIMARY KEY,"
        "  price REAL,"
        "  tax REAL GENERATED ALWAYS AS (price * 0.1) VIRTUAL"
        ");");

    const auto& t = s.tables.at("products");
    REQUIRE(t.columns.size() == 3);
    CHECK(t.columns[2].name == "tax");
    CHECK(t.columns[2].generated == sqlift::GeneratedType::Virtual);
    CHECK(t.columns[2].generated_expr == "price * 0.1");
}

TEST_CASE("parse STRICT table") {
    Schema s = parse(
        "CREATE TABLE data ("
        "  id INTEGER PRIMARY KEY,"
        "  value TEXT NOT NULL"
        ") STRICT;");

    const auto& t = s.tables.at("data");
    CHECK(t.strict == true);
    CHECK(t.without_rowid == false);
}

TEST_CASE("parse STRICT WITHOUT ROWID table") {
    Schema s = parse(
        "CREATE TABLE data ("
        "  id INTEGER PRIMARY KEY,"
        "  value TEXT NOT NULL"
        ") STRICT, WITHOUT ROWID;");

    const auto& t = s.tables.at("data");
    CHECK(t.strict == true);
    CHECK(t.without_rowid == true);
}

TEST_CASE("parse WITHOUT ROWID STRICT table") {
    Schema s = parse(
        "CREATE TABLE data ("
        "  id INTEGER PRIMARY KEY,"
        "  value TEXT NOT NULL"
        ") WITHOUT ROWID, STRICT;");

    const auto& t = s.tables.at("data");
    CHECK(t.strict == true);
    CHECK(t.without_rowid == true);
}
