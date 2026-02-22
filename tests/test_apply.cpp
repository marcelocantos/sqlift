#include <doctest/doctest.h>
#include "sqlift.h"

using namespace sqlift;

TEST_CASE("apply create table") {
    Database db(":memory:");
    Schema desired = parse("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    Schema current = extract(db);
    auto plan = diff(current, desired);

    apply(db, plan);

    Schema after = extract(db);
    // Compare structurally (ignore raw_sql differences)
    REQUIRE(after.tables.size() == 1);
    CHECK(after.tables.count("users"));
    CHECK(after.tables.at("users").columns.size() == 2);
}

TEST_CASE("apply add column") {
    Database db(":memory:");
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    db.exec("INSERT INTO users VALUES (1, 'Alice');");

    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);");
    Schema current = extract(db);
    auto plan = diff(current, desired);

    apply(db, plan);

    // Verify data preserved
    Statement stmt(db, "SELECT name FROM users WHERE id = 1");
    REQUIRE(stmt.step());
    CHECK(stmt.column_text(0) == "Alice");

    // Verify new column exists
    Schema after = extract(db);
    CHECK(after.tables.at("users").columns.size() == 3);
}

TEST_CASE("apply rebuild table - change column type") {
    Database db(":memory:");
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, age TEXT);");
    db.exec("INSERT INTO users VALUES (1, '30');");

    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, age INTEGER);");
    Schema current = extract(db);
    auto plan = diff(current, desired);

    apply(db, plan);

    // Data should be preserved (SQLite will coerce TEXT '30' to INTEGER 30)
    Statement stmt(db, "SELECT age FROM users WHERE id = 1");
    REQUIRE(stmt.step());
    CHECK(stmt.column_text(0) == "30");
}

TEST_CASE("apply refuses destructive without flag") {
    Database db(":memory:");
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY);");

    Schema current = extract(db);
    Schema desired; // empty = drop everything
    auto plan = diff(current, desired);

    CHECK_THROWS_AS(apply(db, plan), DestructiveError);
}

TEST_CASE("apply destructive with flag") {
    Database db(":memory:");
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY);");

    Schema current = extract(db);
    Schema desired;
    auto plan = diff(current, desired);

    apply(db, plan, {.allow_destructive = true});

    Schema after = extract(db);
    CHECK(after.tables.empty());
}

TEST_CASE("apply updates state hash") {
    Database db(":memory:");
    Schema desired = parse("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    auto plan = diff(Schema{}, desired);

    apply(db, plan);

    // Verify _sqlift_state exists and has a hash
    Statement stmt(db, "SELECT value FROM _sqlift_state WHERE key = 'schema_hash'");
    REQUIRE(stmt.step());
    CHECK(!stmt.column_text(0).empty());
}

TEST_CASE("apply detects drift") {
    Database db(":memory:");

    // First migration
    Schema v1 = parse("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    apply(db, diff(Schema{}, v1));

    // Modify schema outside sqlift
    db.exec("ALTER TABLE users ADD COLUMN sneaky TEXT;");

    // Try to apply another migration
    Schema v2 = parse("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    Schema current = extract(db);
    auto plan = diff(current, v2);

    CHECK_THROWS_AS(apply(db, plan, {.allow_destructive = true}), DriftError);
}
