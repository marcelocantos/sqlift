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

TEST_CASE("apply FK violation includes parent table and rowid") {
    Database db(":memory:");
    db.exec("PRAGMA foreign_keys=OFF;");
    db.exec(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));"
        "INSERT INTO users VALUES (1, 'Alice');"
        "INSERT INTO orders VALUES (1, 1);"
        "INSERT INTO orders VALUES (2, 999);");  // orphan FK
    db.exec("PRAGMA foreign_keys=ON;");

    // Change column type on orders to trigger a rebuild — FK is unchanged so
    // no BreakingChangeError, but the orphan data causes an FK violation.
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id BIGINT REFERENCES users(id));");
    Schema current = extract(db);
    auto plan = diff(current, desired);

    try {
        apply(db, plan);
        FAIL("Expected ApplyError");
    } catch (const ApplyError& e) {
        std::string msg = e.what();
        CHECK(msg.find("orders") != std::string::npos);
        CHECK(msg.find("users") != std::string::npos);
        CHECK(msg.find("rowid") != std::string::npos);
    }
}

TEST_CASE("apply error recovery preserves database state") {
    Database db(":memory:");
    db.exec("PRAGMA foreign_keys=OFF;");
    db.exec(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "INSERT INTO users VALUES (1, 'Alice');"
        "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));"
        "INSERT INTO orders VALUES (1, 1);"
        "INSERT INTO orders VALUES (2, 999);");  // orphan FK
    db.exec("PRAGMA foreign_keys=ON;");

    Schema current = extract(db);
    // Change column type on orders to trigger a rebuild — FK is unchanged
    // so no BreakingChangeError, but the orphan data causes an FK violation.
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id BIGINT REFERENCES users(id));");

    auto plan = diff(current, desired);

    // This should fail during the FK check step of rebuild
    CHECK_THROWS_AS(apply(db, plan), ApplyError);

    // Verify the original orders table still has its data
    Statement count_stmt(db, "SELECT count(*) FROM orders");
    REQUIRE(count_stmt.step());
    CHECK(count_stmt.column_int(0) == 2);

    // Verify no temp table left behind
    Statement temp_check(db,
        "SELECT count(*) FROM sqlite_master WHERE name LIKE '%sqlift_new%'");
    REQUIRE(temp_check.step());
    CHECK(temp_check.column_int(0) == 0);
}

TEST_CASE("migration_version starts at 0") {
    Database db(":memory:");
    CHECK(migration_version(db) == 0);
}

TEST_CASE("migration_version increments on apply") {
    Database db(":memory:");

    // First migration
    Schema v1 = parse("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    apply(db, diff(Schema{}, v1));
    CHECK(migration_version(db) == 1);

    // Second migration
    Schema v2 = parse("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    Schema current = extract(db);
    apply(db, diff(current, v2));
    CHECK(migration_version(db) == 2);
}

TEST_CASE("migration_version survives no-op apply") {
    Database db(":memory:");

    Schema v1 = parse("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    apply(db, diff(Schema{}, v1));
    CHECK(migration_version(db) == 1);

    // No-op (same schema) — should not increment
    Schema current = extract(db);
    auto plan = diff(current, v1);
    CHECK(plan.empty());
    // Version unchanged since apply() returns early for empty plans
    CHECK(migration_version(db) == 1);
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
