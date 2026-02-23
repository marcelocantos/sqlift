#include <doctest/doctest.h>
#include "sqlift.h"

using namespace sqlift;

TEST_CASE("roundtrip: empty to schema") {
    auto sql =
        "CREATE TABLE users ("
        "  id INTEGER PRIMARY KEY,"
        "  name TEXT NOT NULL,"
        "  email TEXT"
        ");"
        "CREATE INDEX idx_email ON users(email);";

    Schema desired = parse(sql);
    Database db(":memory:");
    Schema empty = extract(db);

    auto plan = diff(empty, desired);
    apply(db, plan);

    Schema after = extract(db);
    CHECK(after.tables.size() == desired.tables.size());
    CHECK(after.indexes.size() == desired.indexes.size());

    // Verify column structure matches
    const auto& dt = desired.tables.at("users");
    const auto& at = after.tables.at("users");
    REQUIRE(dt.columns.size() == at.columns.size());
    for (size_t i = 0; i < dt.columns.size(); ++i) {
        CHECK(dt.columns[i].name == at.columns[i].name);
        CHECK(dt.columns[i].type == at.columns[i].type);
        CHECK(dt.columns[i].notnull == at.columns[i].notnull);
        CHECK(dt.columns[i].pk == at.columns[i].pk);
    }
}

TEST_CASE("roundtrip: idempotent apply") {
    auto sql = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);";
    Schema desired = parse(sql);

    Database db(":memory:");
    apply(db, diff(extract(db), desired));

    // Second diff should be empty
    auto plan = diff(extract(db), desired);
    CHECK(plan.empty());
}

TEST_CASE("roundtrip: v1 to v2 migration") {
    auto v1_sql = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);";
    auto v2_sql =
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);"
        "CREATE TABLE posts ("
        "  id INTEGER PRIMARY KEY,"
        "  user_id INTEGER REFERENCES users(id),"
        "  title TEXT NOT NULL"
        ");";

    Database db(":memory:");

    // Apply v1
    Schema v1 = parse(v1_sql);
    apply(db, diff(extract(db), v1));

    // Insert data
    db.exec("INSERT INTO users VALUES (1, 'Alice');");

    // Apply v2
    Schema v2 = parse(v2_sql);
    auto plan = diff(extract(db), v2);
    apply(db, plan);

    // Verify data preserved
    Statement stmt(db, "SELECT name FROM users WHERE id = 1");
    REQUIRE(stmt.step());
    CHECK(stmt.column_text(0) == "Alice");

    // Verify new table exists
    Schema after = extract(db);
    CHECK(after.tables.count("posts"));
    CHECK(after.tables.at("users").columns.size() == 3);

    // Idempotent check
    auto plan2 = diff(extract(db), v2);
    CHECK(plan2.empty());
}

TEST_CASE("roundtrip: v1 to v2 to v3 breaking change rejected") {
    auto v1 = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);";
    auto v2 = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);";
    auto v3 = "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL);";

    Database db(":memory:");

    // v1
    apply(db, diff(extract(db), parse(v1)));
    db.exec("INSERT INTO users VALUES (1, 'Alice');");

    // v2
    apply(db, diff(extract(db), parse(v2)));

    // v3 makes email NOT NULL — this is a breaking change and must be rejected
    CHECK_THROWS_AS(diff(extract(db), parse(v3)), BreakingChangeError);
}
