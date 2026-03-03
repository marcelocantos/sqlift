#include <doctest/doctest.h>
#include "test_helpers.h"

TEST_CASE("parse empty string") {
    auto j = json::parse(parse_schema(""));
    CHECK(j["tables"].empty());
    CHECK(j["indexes"].empty());
    CHECK(j["views"].empty());
    CHECK(j["triggers"].empty());
}

TEST_CASE("parse single table") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE users ("
        "  id INTEGER PRIMARY KEY,"
        "  name TEXT NOT NULL,"
        "  email TEXT"
        ");"));

    REQUIRE(j["tables"].size() == 1);
    REQUIRE(j["tables"].contains("users"));

    const auto& t = j["tables"]["users"];
    CHECK(t["name"] == "users");
    REQUIRE(t["columns"].size() == 3);

    CHECK(t["columns"][0]["name"] == "id");
    CHECK(t["columns"][0]["type"] == "INTEGER");
    CHECK(t["columns"][0]["pk"] == 1);

    CHECK(t["columns"][1]["name"] == "name");
    CHECK(t["columns"][1]["type"] == "TEXT");
    CHECK(t["columns"][1]["notnull"] == true);
    CHECK(t["columns"][1]["pk"] == 0);

    CHECK(t["columns"][2]["name"] == "email");
    CHECK(t["columns"][2]["type"] == "TEXT");
    CHECK(t["columns"][2]["notnull"] == false);
}

TEST_CASE("parse table with default") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE items ("
        "  id INTEGER PRIMARY KEY,"
        "  active INTEGER NOT NULL DEFAULT 1"
        ");"));

    const auto& col = j["tables"]["items"]["columns"][1];
    CHECK(col["name"] == "active");
    CHECK(col["notnull"] == true);
    CHECK(col["default_value"] == "1");
}

TEST_CASE("parse table with foreign key") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE posts ("
        "  id INTEGER PRIMARY KEY,"
        "  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE"
        ");"
        "CREATE TABLE users ("
        "  id INTEGER PRIMARY KEY"
        ");"));

    REQUIRE(j["tables"].contains("posts"));
    const auto& fks = j["tables"]["posts"]["foreign_keys"];
    REQUIRE(fks.size() == 1);
    CHECK(fks[0]["to_table"] == "users");
    CHECK(fks[0]["to_columns"] == json::array({"id"}));
    CHECK(fks[0]["on_delete"] == "CASCADE");
}

TEST_CASE("parse index") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);"
        "CREATE UNIQUE INDEX idx_email ON users(email);"));

    REQUIRE(j["indexes"].size() == 1);
    REQUIRE(j["indexes"].contains("idx_email"));
    const auto& idx = j["indexes"]["idx_email"];
    CHECK(idx["table_name"] == "users");
    CHECK(idx["unique"] == true);
    CHECK(idx["columns"] == json::array({"email"}));
}

TEST_CASE("parse view") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE VIEW active_users AS SELECT * FROM users;"));

    REQUIRE(j["views"].size() == 1);
    REQUIRE(j["views"].contains("active_users"));
}

TEST_CASE("parse trigger") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE log (msg TEXT);"
        "CREATE TRIGGER on_user_insert AFTER INSERT ON users "
        "BEGIN INSERT INTO log VALUES ('user added'); END;"));

    REQUIRE(j["triggers"].size() == 1);
    REQUIRE(j["triggers"].contains("on_user_insert"));
    CHECK(j["triggers"]["on_user_insert"]["table_name"] == "users");
}

TEST_CASE("parse invalid SQL throws ParseError") {
    CHECK(parse_err("NOT VALID SQL") == SQLIFT_PARSE_ERROR);
}

TEST_CASE("parse composite primary key") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE user_roles ("
        "  user_id INTEGER,"
        "  role_id INTEGER,"
        "  PRIMARY KEY (user_id, role_id)"
        ");"));

    const auto& t = j["tables"]["user_roles"];
    CHECK(t["columns"][0]["pk"] == 1);
    CHECK(t["columns"][1]["pk"] == 2);
}

TEST_CASE("parse column with COLLATE NOCASE") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE users ("
        "  id INTEGER PRIMARY KEY,"
        "  name TEXT COLLATE NOCASE"
        ");"));

    const auto& t = j["tables"]["users"];
    REQUIRE(t["columns"].size() == 2);
    CHECK(t["columns"][1]["name"] == "name");
    CHECK(t["columns"][1]["collation"] == "NOCASE");
    // Default collation should be empty
    CHECK(t["columns"][0]["collation"] == "");
}

TEST_CASE("parse table with CHECK constraint") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE items ("
        "  id INTEGER PRIMARY KEY,"
        "  price REAL NOT NULL,"
        "  CHECK (price > 0)"
        ");"));

    const auto& t = j["tables"]["items"];
    REQUIRE(t["check_constraints"].size() == 1);
    CHECK(t["check_constraints"][0]["name"] == "");
    CHECK(t["check_constraints"][0]["expression"] == "price > 0");
}

TEST_CASE("parse table with named CHECK constraint") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE items ("
        "  id INTEGER PRIMARY KEY,"
        "  price REAL NOT NULL,"
        "  CONSTRAINT positive_price CHECK (price > 0)"
        ");"));

    const auto& t = j["tables"]["items"];
    REQUIRE(t["check_constraints"].size() == 1);
    CHECK(t["check_constraints"][0]["name"] == "positive_price");
    CHECK(t["check_constraints"][0]["expression"] == "price > 0");
}

TEST_CASE("parse stored generated column") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE people ("
        "  id INTEGER PRIMARY KEY,"
        "  first_name TEXT,"
        "  last_name TEXT,"
        "  full_name TEXT GENERATED ALWAYS AS (first_name || ' ' || last_name) STORED"
        ");"));

    const auto& t = j["tables"]["people"];
    REQUIRE(t["columns"].size() == 4);
    CHECK(t["columns"][3]["name"] == "full_name");
    CHECK(t["columns"][3]["generated"] == 3);
    CHECK(t["columns"][3]["generated_expr"] == "first_name || ' ' || last_name");
}

TEST_CASE("parse virtual generated column") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE products ("
        "  id INTEGER PRIMARY KEY,"
        "  price REAL,"
        "  tax REAL GENERATED ALWAYS AS (price * 0.1) VIRTUAL"
        ");"));

    const auto& t = j["tables"]["products"];
    REQUIRE(t["columns"].size() == 3);
    CHECK(t["columns"][2]["name"] == "tax");
    CHECK(t["columns"][2]["generated"] == 2);
    CHECK(t["columns"][2]["generated_expr"] == "price * 0.1");
}

TEST_CASE("parse STRICT table") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE data ("
        "  id INTEGER PRIMARY KEY,"
        "  value TEXT NOT NULL"
        ") STRICT;"));

    const auto& t = j["tables"]["data"];
    CHECK(t["strict"] == true);
    CHECK(t["without_rowid"] == false);
}

TEST_CASE("parse STRICT WITHOUT ROWID table") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE data ("
        "  id INTEGER PRIMARY KEY,"
        "  value TEXT NOT NULL"
        ") STRICT, WITHOUT ROWID;"));

    const auto& t = j["tables"]["data"];
    CHECK(t["strict"] == true);
    CHECK(t["without_rowid"] == true);
}

TEST_CASE("parse WITHOUT ROWID STRICT table") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE data ("
        "  id INTEGER PRIMARY KEY,"
        "  value TEXT NOT NULL"
        ") WITHOUT ROWID, STRICT;"));

    const auto& t = j["tables"]["data"];
    CHECK(t["strict"] == true);
    CHECK(t["without_rowid"] == true);
}

TEST_CASE("parse named PRIMARY KEY constraint") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE user_roles ("
        "  user_id INTEGER,"
        "  role_id INTEGER,"
        "  CONSTRAINT pk_user_roles PRIMARY KEY (user_id, role_id)"
        ");"));

    const auto& t = j["tables"]["user_roles"];
    CHECK(t["pk_constraint_name"] == "pk_user_roles");
    CHECK(t["columns"][0]["pk"] == 1);
    CHECK(t["columns"][1]["pk"] == 2);
}

TEST_CASE("parse unnamed PRIMARY KEY constraint") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE user_roles ("
        "  user_id INTEGER,"
        "  role_id INTEGER,"
        "  PRIMARY KEY (user_id, role_id)"
        ");"));

    const auto& t = j["tables"]["user_roles"];
    CHECK(t["pk_constraint_name"] == "");
}

TEST_CASE("parse named FOREIGN KEY constraint") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);"
        "CREATE TABLE posts ("
        "  id INTEGER PRIMARY KEY,"
        "  user_id INTEGER,"
        "  CONSTRAINT fk_posts_user FOREIGN KEY (user_id) REFERENCES users(id)"
        ");"));

    const auto& t = j["tables"]["posts"];
    REQUIRE(t["foreign_keys"].size() == 1);
    CHECK(t["foreign_keys"][0]["constraint_name"] == "fk_posts_user");
    CHECK(t["foreign_keys"][0]["from_columns"] == json::array({"user_id"}));
    CHECK(t["foreign_keys"][0]["to_table"] == "users");
}

TEST_CASE("parse unnamed FOREIGN KEY constraint") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);"
        "CREATE TABLE posts ("
        "  id INTEGER PRIMARY KEY,"
        "  user_id INTEGER,"
        "  FOREIGN KEY (user_id) REFERENCES users(id)"
        ");"));

    const auto& t = j["tables"]["posts"];
    REQUIRE(t["foreign_keys"].size() == 1);
    CHECK(t["foreign_keys"][0]["constraint_name"] == "");
}

TEST_CASE("parse named composite FOREIGN KEY constraint") {
    auto j = json::parse(parse_schema(
        "CREATE TABLE parent (a INTEGER, b INTEGER, PRIMARY KEY (a, b));"
        "CREATE TABLE child ("
        "  id INTEGER PRIMARY KEY,"
        "  pa INTEGER,"
        "  pb INTEGER,"
        "  CONSTRAINT fk_child_parent FOREIGN KEY (pa, pb) REFERENCES parent(a, b)"
        ");"));

    const auto& t = j["tables"]["child"];
    REQUIRE(t["foreign_keys"].size() == 1);
    CHECK(t["foreign_keys"][0]["constraint_name"] == "fk_child_parent");
    CHECK(t["foreign_keys"][0]["from_columns"] == json::array({"pa", "pb"}));
}
